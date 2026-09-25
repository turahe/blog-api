// Package newslettermail adapts notification templates, site settings, and user accounts to
// the newsletter ports: confirmation and welcome emails, public links, and account lookups.
package newslettermail

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/core/newsletter/ports"
	notificationports "github.com/turahe/blog-api/internal/core/notification/ports"
	notificationtemplate "github.com/turahe/blog-api/internal/core/notification/template"
	settingsdomain "github.com/turahe/blog-api/internal/core/settings/domain"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
)

// Renderer fills a stored notification template.
type Renderer interface {
	Render(ctx context.Context, channel notificationtemplate.Channel, typ string, data notificationtemplate.Data) (notificationtemplate.Message, error)
}

// Mailer renders the newsletter templates and sends them with the transactional mailer, which
// queues them through the outbox when a broker is configured.
type Mailer struct {
	renderer Renderer
	mailer   notificationports.Mailer
	links    ports.Links
}

var _ ports.Mailer = (*Mailer)(nil)

// NewMailer returns a Mailer.
func NewMailer(renderer Renderer, mailer notificationports.Mailer, links ports.Links) *Mailer {
	return &Mailer{renderer: renderer, mailer: mailer, links: links}
}

// SendConfirm emails the double opt-in link.
func (m *Mailer) SendConfirm(ctx context.Context, email ports.ConfirmEmail) error {
	return m.send(ctx, notificationtemplate.TypeNewsletterConfirm, email.To, notificationtemplate.Data{
		Name: email.Name, Email: email.To, Token: email.Token,
		ExpiresAt: email.ExpiresAt.UTC().Format(time.RFC1123), Lists: strings.Join(email.Lists, ", "),
	})
}

// SendWelcome emails the confirmation receipt with a preferences link.
func (m *Mailer) SendWelcome(ctx context.Context, email ports.WelcomeEmail) error {
	return m.send(ctx, notificationtemplate.TypeNewsletterWelcome, email.To, notificationtemplate.Data{
		Name: email.Name, Email: email.To, Token: email.PreferencesToken, Lists: strings.Join(email.Lists, ", "),
	})
}

func (m *Mailer) send(ctx context.Context, typ, to string, data notificationtemplate.Data) error {
	data.SiteURL = strings.TrimRight(m.links.SiteURL(ctx), "/")
	data.SiteName = m.links.SiteName(ctx)

	msg, err := m.renderer.Render(ctx, notificationtemplate.ChannelEmail, typ, data)
	if err != nil {
		return fmt.Errorf("render %s: %w", typ, err)
	}

	return m.mailer.Send(ctx, notificationports.Message{To: to, Subject: msg.Subject, Text: msg.Body})
}

// SettingsReader reads the current site settings.
type SettingsReader interface {
	Values(ctx context.Context) (settingsdomain.Values, error)
}

// Links reads the site origin and name from settings, falling back to the API origin.
type Links struct {
	settings SettingsReader
	apiURL   string
}

var _ ports.Links = (*Links)(nil)

// NewLinks returns Links. settings may be nil.
func NewLinks(settings SettingsReader, apiURL string) *Links {
	return &Links{settings: settings, apiURL: strings.TrimRight(apiURL, "/")}
}

// SiteURL returns site.public_url, or the API origin when it is unset.
func (l *Links) SiteURL(ctx context.Context) string {
	if v := l.value(ctx, "site.public_url"); v != "" {
		return strings.TrimRight(v, "/")
	}

	return l.apiURL
}

// SiteName returns site.name.
func (l *Links) SiteName(ctx context.Context) string { return l.value(ctx, "site.name") }

// APIURL returns the API origin.
func (l *Links) APIURL() string { return l.apiURL }

func (l *Links) value(ctx context.Context, key string) string {
	if l.settings == nil {
		return ""
	}

	values, err := l.settings.Values(ctx)
	if err != nil {
		return ""
	}

	return values.String(key)
}

// UserFinder loads users by id.
type UserFinder interface {
	FindByID(ctx context.Context, id uuid.UUID) (userdomain.User, error)
}

// Accounts exposes a signed-in user's email to the newsletter.
type Accounts struct {
	users UserFinder
}

var _ ports.Accounts = (*Accounts)(nil)

// NewAccounts returns Accounts over users.
func NewAccounts(users UserFinder) *Accounts {
	return &Accounts{users: users}
}

// Account returns the user's email, name, and whether the email is verified.
func (a *Accounts) Account(ctx context.Context, userID uuid.UUID) (ports.Account, error) {
	user, err := a.users.FindByID(ctx, userID)
	if err != nil {
		return ports.Account{}, err
	}

	name := user.FullName
	if name == "" {
		name = user.Username
	}

	return ports.Account{Email: user.Email, Name: name, EmailVerified: user.EmailVerifiedAt != nil}, nil
}
