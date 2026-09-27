package notify_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/adapters/outbound/notify"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
)

const secret = "secret-token"

func TestLog(t *testing.T) {
	t.Parallel()

	user := userdomain.User{UUID: uuid.MustParse("5f0c7a1e-3b2d-4c9e-8a7f-6e5d4c3b2a19"), Email: "ada@example.test"}
	expires := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		call func(ctx context.Context, l *notify.Log)
		want map[string]any
	}{
		{
			name: "email change requested",
			call: func(ctx context.Context, l *notify.Log) {
				l.EmailChangeRequested(ctx, user, "new@example.test", secret, expires)
			},
			want: map[string]any{
				"msg": "notify: email change requested", "user_id": user.UUID.String(),
				"to": "n***@example.test", "notice_to": "a***@example.test", "expires_at": "2026-09-28T00:00:00Z",
			},
		},
		{
			name: "email changed",
			call: func(ctx context.Context, l *notify.Log) {
				l.EmailChanged(ctx, user, "old@example.test", "new@example.test")
			},
			want: map[string]any{
				"msg": "notify: email changed", "user_id": user.UUID.String(),
				"old": "o***@example.test", "new": "n***@example.test",
			},
		},
		{
			name: "password reset",
			call: func(ctx context.Context, l *notify.Log) { l.PasswordReset(ctx, user, secret, expires) },
			want: map[string]any{
				"msg": "notify: password reset", "user_id": user.UUID.String(),
				"to": "a***@example.test", "expires_at": "2026-09-28T00:00:00Z",
			},
		},
		{
			name: "account verification",
			call: func(ctx context.Context, l *notify.Log) {
				l.AccountVerify(ctx, authdomain.Registration{Email: "grace@example.test", ExpiresAt: expires}, secret)
			},
			want: map[string]any{
				"msg": "notify: account verification", "to": "g***@example.test", "expires_at": "2026-09-28T00:00:00Z",
			},
		},
		{
			name: "account exists",
			call: func(ctx context.Context, l *notify.Log) { l.AccountExists(ctx, user) },
			want: map[string]any{"msg": "notify: account already exists", "user_id": user.UUID.String(), "to": "a***@example.test"},
		},
		{
			name: "password changed",
			call: func(ctx context.Context, l *notify.Log) { l.PasswordChanged(ctx, user) },
			want: map[string]any{"msg": "notify: password changed", "user_id": user.UUID.String(), "to": "a***@example.test"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			tt.call(t.Context(), notify.NewLog(slog.New(slog.NewJSONHandler(&buf, nil))))

			require.NotContains(t, buf.String(), secret, "tokens are never logged")
			require.NotContains(t, buf.String(), "ada@example.test", "addresses are masked")

			var got map[string]any
			require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
			assert.Equal(t, "INFO", got["level"])

			for key, want := range tt.want {
				assert.Equal(t, want, got[key], key)
			}
		})
	}
}

func TestMaskEmail(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		email string
		want  string
	}{
		{name: "keeps the first character and the domain", email: "ada@example.test", want: "a***@example.test"},
		{name: "multibyte first character", email: "élodie@example.test", want: "é***@example.test"},
		{name: "no at sign", email: "ada", want: "***"},
		{name: "empty local part", email: "@example.test", want: "***"},
		{name: "empty", email: "", want: "***"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, notify.MaskEmail(tt.email))
		})
	}
}
