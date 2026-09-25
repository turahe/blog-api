// Package ports declares what the impersonation service needs from adapters.
package ports

import (
	"context"
	"time"

	"github.com/google/uuid"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
	"github.com/turahe/blog-api/internal/core/impersonation/domain"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
)

// Repository stores impersonation sessions.
type Repository interface {
	// Create inserts an active session; domain.ErrAlreadyActive when the actor has one.
	Create(ctx context.Context, session domain.Session) error
	// Get returns the session, or domain.ErrNotFound.
	Get(ctx context.Context, id uuid.UUID) (domain.Session, error)
	// ActiveForActor returns the actor's active session, which may be past its expiry.
	ActiveForActor(ctx context.Context, actorID uuid.UUID) (domain.Session, bool, error)
	// End moves an active session to state; it reports false when the session was not active.
	End(ctx context.Context, id uuid.UUID, state domain.State, reason domain.EndReason, at time.Time) (bool, error)
	// Expired returns up to limit active sessions whose expiry is not after now.
	Expired(ctx context.Context, now time.Time, limit int) ([]domain.Session, error)
}

// Users loads accounts.
type Users interface {
	FindByID(ctx context.Context, id uuid.UUID) (userdomain.User, error)
}

// Permissions reads authorization state.
type Permissions interface {
	Enforce(ctx context.Context, userID uuid.UUID, permission string) (bool, error)
	Grants(ctx context.Context, userID uuid.UUID) (domain.Grants, error)
}

// StepUp re-verifies the actor: the current password, plus a TOTP or backup code when 2FA is
// enabled. Wrong or missing proof reports false.
type StepUp interface {
	VerifyStepUp(ctx context.Context, userID uuid.UUID, password, code string) (bool, error)
}

// Tokens signs access tokens.
type Tokens interface {
	IssueAccess(claims authdomain.AccessClaims) (string, error)
}
