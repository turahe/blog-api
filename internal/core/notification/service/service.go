// Package service sends account emails through an outbound mailer.
package service

import (
	"context"
	"log/slog"
	"strings"
	"time"

	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
	authports "github.com/turahe/blog-api/internal/core/auth/ports"
	"github.com/turahe/blog-api/internal/core/notification/ports"
	"github.com/turahe/blog-api/internal/core/notification/template"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
)

// Service implements authports.EmailChangeNotifier and password notices.
type Service struct {
	mailer    ports.Mailer
	renderer  *template.Renderer
	logger    *slog.Logger
	publicURL string
}

var (
	_ authports.EmailChangeNotifier  = (*Service)(nil)
	_ authports.RegistrationNotifier = (*Service)(nil)
)

// New returns a notification service. publicURL is the API origin used in message text.
// Copy comes from the built-in catalogue until WithTemplates sets a store.
func New(mailer ports.Mailer, logger *slog.Logger, publicURL string) *Service {
	if logger == nil {
		logger = slog.Default()
	}

	return &Service{
		mailer:    mailer,
		renderer:  template.NewRenderer(nil, logger),
		logger:    logger,
		publicURL: strings.TrimRight(strings.TrimSpace(publicURL), "/"),
	}
}

// WithTemplates renders copy from store, falling back to the built-in catalogue.
func (s *Service) WithTemplates(store template.Store) *Service {
	s.renderer = template.NewRenderer(store, s.logger)

	return s
}

// EmailChangeRequested sends the confirmation token to the new address and a notice to the current one.
func (s *Service) EmailChangeRequested(ctx context.Context, user userdomain.User, newEmail, rawToken string, expiresAt time.Time) {
	data := s.data(user, expiresAt)
	data.NewEmail = newEmail
	data.Token = rawToken
	s.sendTemplate(ctx, newEmail, template.TypeEmailChangeConfirm, data)
	s.sendTemplate(ctx, user.Email, template.TypeEmailChangeNotice, data)
}

// EmailChanged tells both addresses that the change completed.
func (s *Service) EmailChanged(ctx context.Context, user userdomain.User, oldEmail, newEmail string) {
	data := s.data(user, time.Time{})
	data.NewEmail = newEmail
	s.sendTemplate(ctx, oldEmail, template.TypeEmailChanged, data)
	s.sendTemplate(ctx, newEmail, template.TypeEmailChanged, data)
}

// PasswordReset sends the reset token to the account address.
func (s *Service) PasswordReset(ctx context.Context, user userdomain.User, rawToken string, expiresAt time.Time) {
	data := s.data(user, expiresAt)
	data.Token = rawToken
	s.sendTemplate(ctx, user.Email, template.TypePasswordReset, data)
}

// AccountVerify sends the sign-up verification token to the registered address.
func (s *Service) AccountVerify(ctx context.Context, registration authdomain.Registration, rawToken string) {
	data := s.data(userdomain.User{Email: registration.Email, Username: registration.Username, FullName: registration.FullName},
		registration.ExpiresAt)
	data.Token = rawToken
	s.sendTemplate(ctx, registration.Email, template.TypeAccountVerify, data)
}

// AccountExists tells an account that someone tried to register its address.
func (s *Service) AccountExists(ctx context.Context, user userdomain.User) {
	s.sendTemplate(ctx, user.Email, template.TypeAccountExists, s.data(user, time.Time{}))
}

// PasswordChanged tells the account that its password was updated.
func (s *Service) PasswordChanged(ctx context.Context, user userdomain.User) {
	s.sendTemplate(ctx, user.Email, template.TypePasswordChanged, s.data(user, time.Time{}))
}

func (s *Service) data(user userdomain.User, expiresAt time.Time) template.Data {
	expires := ""
	if !expiresAt.IsZero() {
		expires = expiresAt.UTC().Format(time.RFC3339)
	}

	return template.Data{
		Name:      greeting(user),
		Email:     user.Email,
		ExpiresAt: expires,
		PublicURL: s.publicURL,
	}
}

func (s *Service) sendTemplate(ctx context.Context, to, typ string, data template.Data) {
	msg, err := s.renderer.Render(ctx, template.ChannelEmail, typ, data)
	if err != nil {
		s.logger.ErrorContext(ctx, "notify: template render failed", "type", typ, "error", err)
		return
	}

	s.send(ctx, to, msg.Subject, msg.Body)
}

func (s *Service) send(ctx context.Context, to, subject, text string) {
	err := s.mailer.Send(ctx, ports.Message{To: to, Subject: subject, Text: text})
	if err != nil {
		s.logger.ErrorContext(ctx, "notify: email delivery failed",
			"to", maskEmail(to),
			"subject", subject,
			"error", err,
		)

		return
	}

	s.logger.InfoContext(ctx, "notify: email sent",
		"to", maskEmail(to),
		"subject", subject,
	)
}

func greeting(user userdomain.User) string {
	if name := strings.TrimSpace(user.FullName); name != "" {
		return name
	}

	if name := strings.TrimSpace(user.Username); name != "" {
		return name
	}

	return "there"
}

func maskEmail(email string) string {
	local, domain, ok := strings.Cut(email, "@")
	if !ok || local == "" {
		return "***"
	}

	return local[:1] + "***@" + domain
}
