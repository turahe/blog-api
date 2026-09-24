// Package service sends account emails through an outbound mailer.
package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	authports "github.com/turahe/blog-api/internal/core/auth/ports"
	"github.com/turahe/blog-api/internal/core/notification/ports"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
)

// Service implements authports.EmailChangeNotifier and password notices.
type Service struct {
	mailer    ports.Mailer
	logger    *slog.Logger
	publicURL string
}

var _ authports.EmailChangeNotifier = (*Service)(nil)

// New returns a notification service. publicURL is the API origin used in message text.
func New(mailer ports.Mailer, logger *slog.Logger, publicURL string) *Service {
	if logger == nil {
		logger = slog.Default()
	}

	return &Service{
		mailer:    mailer,
		logger:    logger,
		publicURL: strings.TrimRight(strings.TrimSpace(publicURL), "/"),
	}
}

// EmailChangeRequested sends the confirmation token to the new address and a notice to the current one.
func (s *Service) EmailChangeRequested(ctx context.Context, user userdomain.User, newEmail, rawToken string, expiresAt time.Time) {
	s.send(ctx, newEmail, "Confirm your new email address", fmt.Sprintf(
		"Hi %s,\n\nConfirm %s as the address for your account before %s.\n\nToken: %s\n\nPOST %s/api/v1/me/email/confirm-change with this token while signed in.\n\nIf you did not request this change, ignore this message.\n",
		greeting(user), newEmail, expiresAt.UTC().Format(time.RFC3339), rawToken, s.publicURL,
	))
	s.send(ctx, user.Email, "Email change requested", fmt.Sprintf(
		"Hi %s,\n\nSomeone requested to change the email on your account to a new address. The change is not complete until that address confirms it.\n\nIf you did not request this, change your password.\n",
		greeting(user),
	))
}

// EmailChanged tells both addresses that the change completed.
func (s *Service) EmailChanged(ctx context.Context, user userdomain.User, oldEmail, newEmail string) {
	body := fmt.Sprintf("Hi %s,\n\nThe email on your account is now %s. All sessions were signed out.\n", greeting(user), newEmail)
	s.send(ctx, oldEmail, "Your email address was changed", body)
	s.send(ctx, newEmail, "Your email address was changed", body)
}

// PasswordReset sends the reset token to the account address.
func (s *Service) PasswordReset(ctx context.Context, user userdomain.User, rawToken string, expiresAt time.Time) {
	s.send(ctx, user.Email, "Reset your password", fmt.Sprintf(
		"Hi %s,\n\nUse this token to reset the password for %s before %s.\n\nToken: %s\n\nPOST %s/api/v1/auth/password/reset\n\nIf you did not request this, ignore this message.\n",
		greeting(user), user.Email, expiresAt.UTC().Format(time.RFC3339), rawToken, s.publicURL,
	))
}

// PasswordChanged tells the account that its password was updated.
func (s *Service) PasswordChanged(ctx context.Context, user userdomain.User) {
	s.send(ctx, user.Email, "Your password was changed", fmt.Sprintf(
		"Hi %s,\n\nThe password for %s was just changed. If you did not do this, reset your password.\n",
		greeting(user), user.Email,
	))
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
