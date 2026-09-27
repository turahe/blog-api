package responses

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
)

func TestTokenPair(t *testing.T) {
	t.Parallel()

	pair := authdomain.TokenPair{AccessToken: "access", RefreshToken: "refresh", TokenType: "Bearer", ExpiresIn: 900}

	assertJSON(t, `{"accessToken": "access", "refreshToken": "refresh", "tokenType": "Bearer", "expiresIn": 900}`,
		TokenPair(pair))
}

func TestTwoFactorChallenge(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
	wib := time.FixedZone("WIB", 7*60*60)

	tests := []struct {
		name      string
		expiresAt time.Time
		want      string
	}{
		{
			name:      "seconds left are truncated",
			expiresAt: now.Add(299*time.Second + 600*time.Millisecond).In(wib),
			want:      `{"twoFactorRequired": true, "challengeToken": "tok", "expiresAt": "2026-08-01T10:04:59Z", "expiresIn": 299}`,
		},
		{
			name:      "already expired clamps to zero",
			expiresAt: now.Add(-time.Minute),
			want:      `{"twoFactorRequired": true, "challengeToken": "tok", "expiresAt": "2026-08-01T09:59:00Z", "expiresIn": 0}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			challenge := authdomain.TwoFactorChallenge{Token: "tok", ExpiresAt: tt.expiresAt}
			assertJSON(t, tt.want, TwoFactorChallenge(challenge, now))
		})
	}
}

func TestTwoFactorStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status authdomain.TwoFactorStatus
		want   string
	}{
		{
			name:   "not enrolled",
			status: authdomain.TwoFactorStatus{},
			want:   `{"enabled": false, "pending": false, "confirmedAt": null, "backupCodesRemaining": 0}`,
		},
		{
			name: "enabled",
			status: authdomain.TwoFactorStatus{
				Enabled:              true,
				ConfirmedAt:          new(time.Date(2026, 8, 1, 17, 0, 0, 0, time.FixedZone("WIB", 7*60*60))),
				BackupCodesRemaining: 8,
			},
			want: `{"enabled": true, "pending": false, "confirmedAt": "2026-08-01T10:00:00Z", "backupCodesRemaining": 8}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertJSON(t, tt.want, TwoFactorStatus(tt.status))
		})
	}
}

func TestRFC3339(t *testing.T) {
	t.Parallel()

	assert.Nil(t, RFC3339(nil))
	assert.Equal(t, "2026-08-01T10:00:00Z", RFC3339(new(time.Date(2026, 8, 1, 12, 0, 0, 0, time.FixedZone("CEST", 2*60*60)))))
}
