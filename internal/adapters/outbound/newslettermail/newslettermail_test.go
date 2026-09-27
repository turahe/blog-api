package newslettermail_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/adapters/outbound/newslettermail"
	"github.com/turahe/blog-api/internal/core/newsletter/ports"
	notificationports "github.com/turahe/blog-api/internal/core/notification/ports"
	notificationtemplate "github.com/turahe/blog-api/internal/core/notification/template"
	settingsdomain "github.com/turahe/blog-api/internal/core/settings/domain"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
)

var errBoom = errors.New("boom")

type captureMailer struct {
	messages []notificationports.Message
	err      error
}

func (m *captureMailer) Send(_ context.Context, msg notificationports.Message) error {
	m.messages = append(m.messages, msg)
	return m.err
}

type failingRenderer struct{}

func (failingRenderer) Render(
	context.Context, notificationtemplate.Channel, string, notificationtemplate.Data,
) (notificationtemplate.Message, error) {
	return notificationtemplate.Message{}, errBoom
}

type fakeSettings struct {
	values map[string]any
	err    error
}

func (s fakeSettings) Values(context.Context) (settingsdomain.Values, error) {
	return settingsdomain.NewValues(s.values), s.err
}

func siteLinks() *newslettermail.Links {
	return newslettermail.NewLinks(fakeSettings{values: map[string]any{
		"site.public_url": "https://blog.example.test/", "site.name": "Example Blog",
	}}, "https://api.example.test")
}

func TestMailerSendConfirm(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		renderer newslettermail.Renderer
		mailErr  error
		wantErr  error
	}{
		{name: "renders and sends the confirmation", renderer: notificationtemplate.NewRenderer(nil, nil)},
		{name: "render failure is wrapped", renderer: failingRenderer{}, wantErr: errBoom},
		{name: "mail failure is returned", renderer: notificationtemplate.NewRenderer(nil, nil), mailErr: errBoom, wantErr: errBoom},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mailer := &captureMailer{err: tt.mailErr}
			m := newslettermail.NewMailer(tt.renderer, mailer, siteLinks())

			err := m.SendConfirm(t.Context(), ports.ConfirmEmail{
				To: "reader@example.test", Name: "Reader", Token: "confirm-token",
				ExpiresAt: time.Date(2026, 9, 29, 12, 0, 0, 0, time.FixedZone("WIB", 7*60*60)),
				Lists:     []string{"Weekly digest", "Product news"},
			})
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				if tt.mailErr == nil {
					require.ErrorContains(t, err, "render "+notificationtemplate.TypeNewsletterConfirm)
					require.Empty(t, mailer.messages)
				}

				return
			}

			require.NoError(t, err)
			require.Len(t, mailer.messages, 1)

			msg := mailer.messages[0]
			assert.Equal(t, "reader@example.test", msg.To)
			assert.Equal(t, "Confirm your subscription to Example Blog", msg.Subject)
			assert.Contains(t, msg.Text, "Hi Reader,")
			assert.Contains(t, msg.Text, "Weekly digest, Product news")
			assert.Contains(t, msg.Text, "Tue, 29 Sep 2026 05:00:00 UTC", "the expiry is shown in UTC")
			assert.Contains(t, msg.Text, "https://blog.example.test/newsletter/confirm?token=confirm-token")
			assert.Contains(t, msg.HTML, `href="https://blog.example.test/newsletter/confirm?token=confirm-token"`)
		})
	}
}

func TestMailerSendWelcome(t *testing.T) {
	t.Parallel()

	mailer := &captureMailer{}
	m := newslettermail.NewMailer(notificationtemplate.NewRenderer(nil, nil), mailer, siteLinks())

	require.NoError(t, m.SendWelcome(t.Context(), ports.WelcomeEmail{
		To: "reader@example.test", PreferencesToken: "prefs-token", Lists: []string{"Weekly digest"},
	}))

	require.Len(t, mailer.messages, 1)

	msg := mailer.messages[0]
	assert.Equal(t, "You're subscribed to Example Blog", msg.Subject)
	assert.Contains(t, msg.Text, "Hi,\n", "no name, no name in the greeting")
	assert.Contains(t, msg.Text, "subscribed to Weekly digest.")
	assert.Contains(t, msg.Text, "https://blog.example.test/newsletter/preferences?token=prefs-token")
}

func TestLinks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		settings newslettermail.SettingsReader
		wantSite string
		wantName string
	}{
		{
			name: "site settings",
			settings: fakeSettings{values: map[string]any{
				"site.public_url": "https://blog.example.test//", "site.name": "Example Blog",
			}},
			wantSite: "https://blog.example.test",
			wantName: "Example Blog",
		},
		{name: "no settings reader falls back to the API origin", wantSite: "https://api.example.test"},
		{name: "unset public URL falls back to the API origin", settings: fakeSettings{}, wantSite: "https://api.example.test"},
		{
			name:     "unreadable settings fall back to the API origin",
			settings: fakeSettings{values: map[string]any{"site.name": "Example Blog"}, err: errBoom},
			wantSite: "https://api.example.test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			links := newslettermail.NewLinks(tt.settings, "https://api.example.test/")
			assert.Equal(t, tt.wantSite, links.SiteURL(t.Context()))
			assert.Equal(t, tt.wantName, links.SiteName(t.Context()))
			assert.Equal(t, "https://api.example.test", links.APIURL())
		})
	}
}

type fakeUsers struct {
	user userdomain.User
	err  error
}

func (u fakeUsers) FindByID(context.Context, uuid.UUID) (userdomain.User, error) {
	return u.user, u.err
}

func TestAccountsAccount(t *testing.T) {
	t.Parallel()

	verified := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		users   fakeUsers
		want    ports.Account
		wantErr error
	}{
		{
			name:  "verified user with a full name",
			users: fakeUsers{user: userdomain.User{Email: "ada@example.test", FullName: "Ada Lovelace", Username: "ada", EmailVerifiedAt: &verified}},
			want:  ports.Account{Email: "ada@example.test", Name: "Ada Lovelace", EmailVerified: true},
		},
		{
			name:  "unverified user falls back to the username",
			users: fakeUsers{user: userdomain.User{Email: "ada@example.test", Username: "ada"}},
			want:  ports.Account{Email: "ada@example.test", Name: "ada"},
		},
		{name: "lookup fails", users: fakeUsers{err: errBoom}, wantErr: errBoom},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := newslettermail.NewAccounts(tt.users).Account(t.Context(), uuid.New())
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}
