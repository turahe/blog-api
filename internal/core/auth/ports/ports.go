package ports

import (
	"context"
	"time"

	"github.com/google/uuid"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
)

type UserRepository interface {
	FindByEmail(ctx context.Context, email string) (userdomain.User, error)
	FindByID(ctx context.Context, id uuid.UUID) (userdomain.User, error)
	FindByUsernameOrEmail(ctx context.Context, identity string) (userdomain.User, error)
	RecordLogin(ctx context.Context, id uuid.UUID, at time.Time) error
	Create(ctx context.Context, user userdomain.User) (userdomain.User, error)
	UpdatePassword(ctx context.Context, id uuid.UUID, hash string, changedAt time.Time) error
}

type SessionRepository interface {
	Create(ctx context.Context, session authdomain.RefreshSession) (authdomain.RefreshSession, error)
	FindByTokenHash(ctx context.Context, hash string) (authdomain.RefreshSession, error)
	Revoke(ctx context.Context, id uuid.UUID, at time.Time) error
	RevokeFamily(ctx context.Context, userID, familyID uuid.UUID, at time.Time) error
	RevokeAllForUser(ctx context.Context, userID uuid.UUID, at time.Time) error
	Replace(ctx context.Context, oldID, newID uuid.UUID, at time.Time) error
}

type ResetTokenRepository interface {
	Create(ctx context.Context, token authdomain.PasswordResetToken) error
	FindByHash(ctx context.Context, hash string) (authdomain.PasswordResetToken, error)
	MarkUsed(ctx context.Context, id uuid.UUID, at time.Time) error
}

type PasswordHasher interface {
	Hash(password string) (string, error)
	Compare(hash, password string) bool
}

type TokenService interface {
	IssueAccess(claims authdomain.AccessClaims) (string, error)
	ParseAccess(token string) (authdomain.AccessClaims, error)
	IssueRefresh() (raw string, hash string, err error)
	HashRefresh(raw string) string
	IssueResetToken() (raw string, hash string, jti string, err error)
	HashResetToken(raw string) string
}

type Clock interface {
	Now() time.Time
}

type IDGenerator interface {
	New() uuid.UUID
}

// ResetTokenSink optionally captures newly issued reset tokens (tests / local tooling only).
type ResetTokenSink interface {
	Capture(rawToken string)
}

type Service interface {
	Login(ctx context.Context, email, password, userAgent, ip string, remember bool) (authdomain.TokenPair, error)
	Refresh(ctx context.Context, refreshToken, userAgent, ip string) (authdomain.TokenPair, error)
	Logout(ctx context.Context, userID uuid.UUID, refreshToken string) error
	ParseAccessToken(token string) (authdomain.AccessClaims, error)
	ForgotPassword(ctx context.Context, emailOrUsername string) error
	CheckResetToken(ctx context.Context, rawToken string) (authdomain.ResetTokenValidity, error)
	ResetPassword(ctx context.Context, rawToken, newPassword, confirmPassword string) error
	ChangePassword(ctx context.Context, userID uuid.UUID, current, newPassword, confirm string, revokeAll bool) (changedAt time.Time, sessionsInvalidated bool, err error)
}
