package service_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
	"github.com/turahe/blog-api/internal/core/notification/ports"
	notificationservice "github.com/turahe/blog-api/internal/core/notification/service"
	"github.com/turahe/blog-api/internal/core/notification/template"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
)

type captureMailer struct {
	messages []ports.Message
	err      error
}

func (c *captureMailer) Send(_ context.Context, msg ports.Message) error {
	c.messages = append(c.messages, msg)
	return c.err
}

type templateStore struct {
	rows map[string]template.Template
}

func (s templateStore) Get(_ context.Context, typ string, channel template.Channel) (template.Template, error) {
	tpl, ok := s.rows[typ+"/"+string(channel)]
	if !ok {
		return template.Template{}, template.ErrNotFound
	}

	return tpl, nil
}

func (s templateStore) List(context.Context) ([]template.Template, error)       { return nil, nil }
func (s templateStore) Save(context.Context, template.Template) error           { return nil }
func (s templateStore) SeedDefaults(context.Context, []template.Template) error { return nil }

var ada = userdomain.User{Email: "ada@example.com", FullName: "Ada"}

func newService(mailer *captureMailer) (*notificationservice.Service, *bytes.Buffer) {
	var logs bytes.Buffer
	svc := notificationservice.New(mailer, slog.New(slog.NewTextHandler(&logs, nil)), " http://127.0.0.1:8080/ ")

	return svc, &logs
}

func TestWithTemplates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		stored      template.Template
		wantSubject string
		wantLog     string
	}{
		{
			name: "stored copy is used",
			stored: template.Template{
				Type: template.TypePasswordChanged, Channel: template.ChannelEmail, Subject: "Custom notice", Body: "Hi {{.Name}}",
			},
			wantSubject: "Custom notice",
		},
		{
			name: "a broken stored template sends nothing",
			stored: template.Template{
				Type: template.TypePasswordChanged, Channel: template.ChannelEmail, Subject: "s", Body: "{{.Password}}",
			},
			wantLog: "notify: template render failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mailer := &captureMailer{}
			svc, logs := newService(mailer)
			svc = svc.WithTemplates(templateStore{rows: map[string]template.Template{
				tt.stored.Type + "/" + string(tt.stored.Channel): tt.stored,
			}})

			svc.PasswordChanged(t.Context(), ada)

			if tt.wantLog != "" {
				require.Empty(t, mailer.messages)
				require.Contains(t, logs.String(), tt.wantLog)

				return
			}

			require.Len(t, mailer.messages, 1)
			require.Equal(t, tt.wantSubject, mailer.messages[0].Subject)
			require.Equal(t, "Hi Ada", mailer.messages[0].Text)
		})
	}
}

func TestEmailChangeRequested(t *testing.T) {
	t.Parallel()

	mailer := &captureMailer{}
	svc, _ := newService(mailer)

	svc.EmailChangeRequested(t.Context(), ada, "new@example.com", "change-token", time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC))

	require.Len(t, mailer.messages, 2)

	confirm, notice := mailer.messages[0], mailer.messages[1]
	assert.Equal(t, "new@example.com", confirm.To)
	assert.Equal(t, "Confirm your new email address", confirm.Subject)
	assert.Contains(t, confirm.Text, "Confirm new@example.com as the address for your account before 2026-09-28T00:00:00Z.")
	assert.Contains(t, confirm.Text, "change-token")
	assert.Contains(t, confirm.Text, "POST http://127.0.0.1:8080/api/v1/me/email/confirm-change")

	assert.Equal(t, "ada@example.com", notice.To)
	assert.Equal(t, "Email change requested", notice.Subject)
	assert.NotContains(t, notice.Text+notice.HTML, "change-token", "the current address never sees the token")
}

func TestEmailChanged(t *testing.T) {
	t.Parallel()

	mailer := &captureMailer{}
	svc, _ := newService(mailer)

	svc.EmailChanged(t.Context(), ada, "old@example.com", "new@example.com")

	require.Len(t, mailer.messages, 2)

	for i, to := range []string{"old@example.com", "new@example.com"} {
		assert.Equal(t, to, mailer.messages[i].To)
		assert.Equal(t, "Your email address was changed", mailer.messages[i].Subject)
		assert.Contains(t, mailer.messages[i].Text, "The email on your account is now new@example.com.")
	}
}

func TestPasswordResetIncludesTokenOnlyInBody(t *testing.T) {
	t.Parallel()

	mailer := &captureMailer{}
	svc := notificationservice.New(mailer, nil, "http://127.0.0.1:8080")
	user := userdomain.User{Email: "ada@example.com", FullName: "Ada"}

	svc.PasswordReset(context.Background(), user, "secret-token", time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC))

	require.Len(t, mailer.messages, 1)
	require.Equal(t, "ada@example.com", mailer.messages[0].To)
	require.Equal(t, "Reset your password", mailer.messages[0].Subject)
	require.Contains(t, mailer.messages[0].Text, "secret-token")
	require.NotContains(t, mailer.messages[0].Subject, "secret-token")
}

func TestAccountVerify(t *testing.T) {
	t.Parallel()

	mailer := &captureMailer{}
	svc, _ := newService(mailer)

	svc.AccountVerify(t.Context(), authdomain.Registration{
		Email: "grace@example.com", Username: "grace", FullName: "Grace Hopper",
		ExpiresAt: time.Date(2026, 9, 28, 7, 0, 0, 0, time.FixedZone("WIB", 7*60*60)),
	}, "verify-token")

	require.Len(t, mailer.messages, 1)

	msg := mailer.messages[0]
	assert.Equal(t, "grace@example.com", msg.To)
	assert.Equal(t, "Verify your email address", msg.Subject)
	assert.Contains(t, msg.Text, "Hi Grace Hopper,")
	assert.Contains(t, msg.Text, "Confirm grace@example.com before 2026-09-28T00:00:00Z.", "expiry is shown in UTC")
	assert.Contains(t, msg.Text, "Token: verify-token")
}

func TestAccountExists(t *testing.T) {
	t.Parallel()

	mailer := &captureMailer{}
	svc, _ := newService(mailer)

	svc.AccountExists(t.Context(), ada)

	require.Len(t, mailer.messages, 1)
	assert.Equal(t, "ada@example.com", mailer.messages[0].To)
	assert.Equal(t, "You already have an account", mailer.messages[0].Subject)
	assert.Contains(t, mailer.messages[0].Text, "POST http://127.0.0.1:8080/api/v1/auth/password/forgot")
}

func TestPasswordChanged(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		user         userdomain.User
		mailErr      error
		wantGreeting string
		wantLog      string
		avoidLog     string
	}{
		{
			name:         "greets by full name and logs the masked address",
			user:         userdomain.User{Email: "ada@example.com", FullName: "  Ada  ", Username: "ada"},
			wantGreeting: "Hi Ada,",
			wantLog:      "notify: email sent",
			avoidLog:     "ada@example.com",
		},
		{
			name:         "falls back to the username",
			user:         userdomain.User{Email: "ada@example.com", FullName: " ", Username: " ada "},
			wantGreeting: "Hi ada,",
			wantLog:      "a***@example.com",
		},
		{
			name:         "falls back to a generic greeting",
			user:         userdomain.User{Email: "ada@example.com"},
			wantGreeting: "Hi there,",
		},
		{
			name:         "delivery failure is logged with a masked address",
			user:         userdomain.User{Email: "ada@example.com", FullName: "Ada"},
			mailErr:      errors.New("smtp down"),
			wantGreeting: "Hi Ada,",
			wantLog:      "notify: email delivery failed",
			avoidLog:     "ada@example.com",
		},
		{
			name:         "an address without a local part is fully masked",
			user:         userdomain.User{Email: "not-an-address", FullName: "Ada"},
			mailErr:      errors.New("smtp down"),
			wantGreeting: "Hi Ada,",
			wantLog:      "to=***",
			avoidLog:     "not-an-address",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mailer := &captureMailer{err: tt.mailErr}
			svc, logs := newService(mailer)

			svc.PasswordChanged(t.Context(), tt.user)

			require.Len(t, mailer.messages, 1)
			assert.Equal(t, "Your password was changed", mailer.messages[0].Subject)
			assert.Contains(t, mailer.messages[0].Text, tt.wantGreeting)
			assert.Contains(t, logs.String(), tt.wantLog)

			if tt.avoidLog != "" {
				assert.NotContains(t, logs.String(), tt.avoidLog)
			}
		})
	}
}
