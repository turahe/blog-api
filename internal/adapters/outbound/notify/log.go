// Package notify delivers account notifications. Log is the fallback when SMTP is unset.
package notify

import (
	"context"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
	authports "github.com/turahe/blog-api/internal/core/auth/ports"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
)

// Log records notifications as log events with masked addresses. Tokens are never
// logged, so flows that need them cannot complete until a mail provider is wired.
type Log struct {
	logger *slog.Logger
}

var (
	_ authports.EmailChangeNotifier  = (*Log)(nil)
	_ authports.RegistrationNotifier = (*Log)(nil)
)

// NewLog returns a log notifier.
func NewLog(logger *slog.Logger) *Log {
	return &Log{logger: logger}
}

// EmailChangeRequested logs the confirmation meant for the new address and the notice for the old one.
func (l *Log) EmailChangeRequested(ctx context.Context, user userdomain.User, newEmail, _ string, expiresAt time.Time) {
	l.logger.InfoContext(ctx, "notify: email change requested",
		"user_id", user.UUID,
		"to", MaskEmail(newEmail),
		"notice_to", MaskEmail(user.Email),
		"expires_at", expiresAt,
	)
}

// EmailChanged logs the completion notice sent to both addresses.
func (l *Log) EmailChanged(ctx context.Context, user userdomain.User, oldEmail, newEmail string) {
	l.logger.InfoContext(ctx, "notify: email changed",
		"user_id", user.UUID,
		"old", MaskEmail(oldEmail),
		"new", MaskEmail(newEmail),
	)
}

// PasswordReset logs that a reset message would be sent. The token is never logged.
func (l *Log) PasswordReset(ctx context.Context, user userdomain.User, _ string, expiresAt time.Time) {
	l.logger.InfoContext(ctx, "notify: password reset",
		"user_id", user.UUID,
		"to", MaskEmail(user.Email),
		"expires_at", expiresAt,
	)
}

// AccountVerify logs that a verification message would be sent. The token is never logged.
func (l *Log) AccountVerify(ctx context.Context, registration authdomain.Registration, _ string) {
	l.logger.InfoContext(ctx, "notify: account verification",
		"to", MaskEmail(registration.Email),
		"expires_at", registration.ExpiresAt,
	)
}

// AccountExists logs the notice sent when an address is registered again.
func (l *Log) AccountExists(ctx context.Context, user userdomain.User) {
	l.logger.InfoContext(ctx, "notify: account already exists",
		"user_id", user.UUID,
		"to", MaskEmail(user.Email),
	)
}

// PasswordChanged logs the password-change notice.
func (l *Log) PasswordChanged(ctx context.Context, user userdomain.User) {
	l.logger.InfoContext(ctx, "notify: password changed",
		"user_id", user.UUID,
		"to", MaskEmail(user.Email),
	)
}

// MaskEmail keeps the first character of the local part and the domain: "j***@example.com".
func MaskEmail(email string) string {
	local, domain, ok := strings.Cut(email, "@")
	if !ok || local == "" {
		return "***"
	}

	first, _ := utf8.DecodeRuneInString(local)

	return string(first) + "***@" + domain
}
