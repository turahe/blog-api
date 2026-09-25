package bootstrap

import (
	"log/slog"
	"net/url"

	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	"github.com/turahe/blog-api/internal/adapters/outbound/captcha"
	"github.com/turahe/blog-api/internal/adapters/outbound/mailqueue"
	"github.com/turahe/blog-api/internal/adapters/outbound/markdown"
	"github.com/turahe/blog-api/internal/adapters/outbound/newslettermail"
	"github.com/turahe/blog-api/internal/adapters/outbound/newsletterprovider"
	"github.com/turahe/blog-api/internal/adapters/outbound/persistence"
	"github.com/turahe/blog-api/internal/core/event"
	newsletterservice "github.com/turahe/blog-api/internal/core/newsletter/service"
	notificationtemplate "github.com/turahe/blog-api/internal/core/notification/template"
	settingsdomain "github.com/turahe/blog-api/internal/core/settings/domain"
	settingsservice "github.com/turahe/blog-api/internal/core/settings/service"
	"github.com/turahe/blog-api/internal/platform/config"
	"github.com/turahe/blog-api/internal/platform/database"
	"github.com/turahe/blog-api/internal/platform/system"
)

// NewNewsletterService wires newsletter subscriptions, issues, and dispatch. settings may be
// nil (app worker and scheduler); links then read the stored settings directly. Missing SMTP
// disables confirmation emails and, with the smtp provider, dispatch.
func NewNewsletterService(
	cfg config.Config, db *database.Database, events event.Unit, settings newslettermail.SettingsReader, logger *slog.Logger,
) *newsletterservice.Service {
	if settings == nil {
		settings = settingsservice.New(persistence.NewSettingsRepository(db.GORM), settingsdomain.DefaultCatalogue(),
			system.UUIDGenerator{}, system.Clock{})
	}

	links := newslettermail.NewLinks(settings, cfg.AppPublicURL)
	deps := newsletterservice.Deps{
		Repo: persistence.NewNewsletterRepository(db.GORM), IDs: system.UUIDGenerator{}, Clock: system.Clock{},
		Links: links, Markdown: markdown.NewNewsletter(), Events: events, Logger: logger,
		Accounts: newslettermail.NewAccounts(persistence.NewUserRepository(db.GORM)),
	}

	if mailer := NewTransactionalMailer(cfg, db, newsletterBox(cfg, logger), logger); mailer != nil {
		renderer := notificationtemplate.NewRenderer(persistence.NewNotificationTemplateRepository(db.GORM), logger)
		deps.Mailer = newslettermail.NewMailer(renderer, mailer, links)
	}

	switch cfg.NewsletterProvider {
	case config.NewsletterProviderCustomHTTP:
		gateway := newsletterprovider.NewHTTP(cfg.NewsletterHTTPEndpoint, cfg.NewsletterHTTPSecret, nil)
		deps.Sender, deps.Contacts = gateway, gateway
	default:
		if smtp, err := NewMailer(cfg); err == nil && smtp != nil {
			deps.Sender = newsletterprovider.NewSMTP(smtp)
		}
	}

	if cfg.NewsletterWebhookEnabled() {
		deps.Webhooks = newsletterprovider.NewVerifier(cfg.NewsletterHTTPSecret)
	}

	if cfg.TurnstileSecretKey != "" {
		deps.Captcha = captcha.NewTurnstile(cfg.TurnstileSecretKey, "", nil)
	}

	return newsletterservice.New(deps, newsletterservice.Config{BatchSize: cfg.NewsletterSendBatch})
}

// NewsletterProvider describes the configured provider for the admin provider config. The
// endpoint is shown without credentials or query, and the secret only as set or not.
func NewsletterProvider(cfg config.Config) responses.NewsletterProvider {
	info := responses.NewsletterProvider{
		Name: cfg.NewsletterProvider, SecretConfigured: cfg.NewsletterHTTPSecret != "",
		WebhookEnabled: cfg.NewsletterWebhookEnabled(),
	}

	if u, err := url.Parse(cfg.NewsletterHTTPEndpoint); err == nil && u.Host != "" {
		info.Endpoint = u.Scheme + "://" + u.Host + u.Path
	}

	return info
}

// newsletterBox returns the APP_ENCRYPTION_KEY box for queued emails, or a nil interface.
func newsletterBox(cfg config.Config, logger *slog.Logger) mailqueue.Box {
	box, err := NewSecretBox(cfg)
	if err != nil {
		logger.Error("newsletter: encryption key invalid; confirmation emails are sent inline", "error", err)
		return nil
	}

	if box == nil {
		return nil
	}

	return box
}
