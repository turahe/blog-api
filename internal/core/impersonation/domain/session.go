// Package domain holds impersonation sessions and the rules for who may impersonate whom.
package domain

import (
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"
)

// PermissionStart lets a staff member impersonate users. The admin role holds it through "*".
const PermissionStart = "impersonation.start"

// Reason length bounds, in characters.
const (
	MinReasonLength = 10
	MaxReasonLength = 255
)

// Impersonation errors.
var (
	ErrValidation     = errors.New("impersonation validation failed")
	ErrForbidden      = errors.New("impersonation not permitted")
	ErrStepUpRequired = errors.New("impersonation requires current password and 2FA code")
	ErrIneligible     = errors.New("user cannot be impersonated")
	ErrAlreadyActive  = errors.New("an impersonation session is already active")
	ErrNotFound       = errors.New("impersonation session not found")
	// ErrSignInRequired means the caller's token does not name the sign-in (refresh-session
	// family) the impersonation would be bound to; refreshing the token supplies it.
	ErrSignInRequired = errors.New("impersonation requires a token from a current sign-in")
	// ErrEnded means an impersonation token's session is over (stopped, expired, or revoked).
	ErrEnded = errors.New("impersonation session ended")
)

// State is an impersonation session state.
type State string

// Session states. Only active sessions back a usable token.
const (
	StateActive  State = "active"
	StateExited  State = "exited"
	StateExpired State = "expired"
	StateRevoked State = "revoked"
)

// EndReason says why a session ended; the values match the AsyncAPI reason_code.
type EndReason string

// End reasons.
const (
	EndManual  EndReason = "manual_exit"
	EndExpired EndReason = "expired"
	EndPolicy  EndReason = "policy"
	// EndBaseSession: the impersonator's own sign-in ended (logout, revocation, or expiry).
	EndBaseSession EndReason = "parent_session_expired"
)

// Session is one impersonation: Actor acts as Target until ExpiresAt or until it ends.
type Session struct {
	UUID       uuid.UUID
	ActorUUID  uuid.UUID
	TargetUUID uuid.UUID
	State      State
	Reason     string
	IP         string
	UserAgent  string
	StartedAt  time.Time
	ExpiresAt  time.Time
	EndedAt    *time.Time
	EndReason  *EndReason
	// BaseFamilyID is the refresh-session family of the impersonator's own sign-in; the
	// session lasts only as long as that sign-in does.
	BaseFamilyID uuid.UUID
	// ParticipantsActive is filled on reads: both the actor and the target accounts are active.
	ParticipantsActive bool
	// BaseSessionUntil is filled on reads: the latest expiry among the base family's unrevoked
	// refresh sessions, nil when there are none.
	BaseSessionUntil *time.Time
}

// ActiveAt reports whether the session still backs its token at now.
func (s Session) ActiveAt(now time.Time) bool {
	return s.State == StateActive && now.Before(s.ExpiresAt)
}

// BaseSessionActiveAt reports whether the impersonator is still signed in at now.
func (s Session) BaseSessionActiveAt(now time.Time) bool {
	return s.BaseSessionUntil != nil && now.Before(*s.BaseSessionUntil)
}

// Grants are a user's roles and the permissions those roles give.
type Grants struct {
	Roles       []string
	Permissions []string
}

// Has reports whether the grants allow permission, directly or through "*".
func (g Grants) Has(permission string) bool {
	return slices.Contains(g.Permissions, permission) || slices.Contains(g.Permissions, "*")
}

// CanImpersonate reports whether an actor with these grants may impersonate a target with
// target's grants: the target must not be an administrator or able to impersonate, and every
// permission the target has must be one the actor has, so impersonation never adds rights.
func (g Grants) CanImpersonate(target Grants, adminRole string) bool {
	if slices.Contains(target.Roles, adminRole) || target.Has(PermissionStart) {
		return false
	}

	for _, permission := range target.Permissions {
		if !g.Has(permission) {
			return false
		}
	}

	return true
}
