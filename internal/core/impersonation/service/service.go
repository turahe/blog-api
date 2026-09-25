// Package service implements staff impersonation: starting a session that yields a
// short-lived token acting as the target, verifying that token on every request, and ending it.
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/core/audit"
	auditdomain "github.com/turahe/blog-api/internal/core/audit/domain"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
	"github.com/turahe/blog-api/internal/core/event"
	"github.com/turahe/blog-api/internal/core/impersonation/domain"
	"github.com/turahe/blog-api/internal/core/impersonation/ports"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
)

// IDGenerator returns new UUIDs.
type IDGenerator interface {
	New() uuid.UUID
}

// Clock returns the current time.
type Clock interface {
	Now() time.Time
}

// Config tunes the service.
type Config struct {
	// TTL is the fixed session and token lifetime; sessions are never renewed.
	TTL time.Duration
	// AdminRole can never be impersonated.
	AdminRole string
}

// Service implements impersonation.
type Service struct {
	repo   ports.Repository
	users  ports.Users
	perms  ports.Permissions
	stepUp ports.StepUp
	tokens ports.Tokens
	ids    IDGenerator
	clock  Clock
	cfg    Config
	events event.Unit
}

// New returns a Service.
func New(
	repo ports.Repository, users ports.Users, perms ports.Permissions, stepUp ports.StepUp,
	tokens ports.Tokens, ids IDGenerator, clock Clock, cfg Config,
) *Service {
	return &Service{repo: repo, users: users, perms: perms, stepUp: stepUp, tokens: tokens, ids: ids, clock: clock, cfg: cfg}
}

// WithEvents records lifecycle events through events, in the same transaction as each change.
func (s *Service) WithEvents(events event.Unit) *Service {
	s.events = events
	return s
}

// StartInput is a request to impersonate TargetID.
type StartInput struct {
	ActorID   uuid.UUID
	TargetID  uuid.UUID
	Reason    string
	Password  string
	Code      string
	IP        string
	UserAgent string
}

// Started is a new session, its target, and the token that acts as the target.
type Started struct {
	Session     domain.Session
	Target      userdomain.User
	AccessToken string
}

// Start checks the actor's permission, step-up proof, and the target's eligibility, then opens
// a session and signs a token for it that expires with the session.
func (s *Service) Start(ctx context.Context, in StartInput) (Started, error) {
	reason := strings.TrimSpace(in.Reason)
	if n := utf8.RuneCountInString(reason); n < domain.MinReasonLength || n > domain.MaxReasonLength {
		return Started{}, fmt.Errorf("%w: reason must be %d-%d characters", domain.ErrValidation,
			domain.MinReasonLength, domain.MaxReasonLength)
	}

	audit.SetResource(ctx, auditdomain.ResourceUser, in.TargetID)
	audit.AddMetadata(ctx, "reason", reason)

	allowed, err := s.perms.Enforce(ctx, in.ActorID, domain.PermissionStart)
	if err != nil {
		return Started{}, err
	}

	if !allowed {
		return Started{}, domain.ErrForbidden
	}

	verified, err := s.stepUp.VerifyStepUp(ctx, in.ActorID, in.Password, in.Code)
	if err != nil {
		return Started{}, err
	}

	if !verified {
		return Started{}, domain.ErrStepUpRequired
	}

	target, err := s.eligibleTarget(ctx, in.ActorID, in.TargetID)
	if err != nil {
		return Started{}, err
	}

	now := s.clock.Now()
	session := domain.Session{
		UUID: s.ids.New(), ActorUUID: in.ActorID, TargetUUID: in.TargetID, State: domain.StateActive,
		Reason: reason, IP: in.IP, UserAgent: in.UserAgent, StartedAt: now, ExpiresAt: now.Add(s.cfg.TTL),
	}

	var token string

	err = s.events.InTx(ctx, func(ctx context.Context) error {
		if err := s.repo.Create(ctx, session); err != nil {
			return err
		}

		token, err = s.tokens.IssueAccess(authdomain.AccessClaims{
			Subject: target.UUID, Email: target.Email, Username: target.Username,
			IssuedAt: now, ExpiresAt: session.ExpiresAt, ID: s.ids.New().String(),
			Actor: &in.ActorID, SessionID: session.UUID.String(),
		})
		if err != nil {
			return err
		}

		return s.events.Record(ctx, lifecycleEvent(event.ImpersonationStarted, session, "", now))
	})
	if err != nil {
		return Started{}, err
	}

	audit.AddMetadata(ctx, "impersonation_session_id", session.UUID.String())

	return Started{Session: session, Target: target, AccessToken: token}, nil
}

// eligibleTarget returns the target when the actor may impersonate it.
func (s *Service) eligibleTarget(ctx context.Context, actorID, targetID uuid.UUID) (userdomain.User, error) {
	if actorID == targetID {
		return userdomain.User{}, domain.ErrIneligible
	}

	target, err := s.users.FindByID(ctx, targetID)
	if errors.Is(err, userdomain.ErrNotFound) {
		return userdomain.User{}, domain.ErrIneligible
	}

	if err != nil {
		return userdomain.User{}, err
	}

	if !target.IsActive() {
		return userdomain.User{}, domain.ErrIneligible
	}

	actorGrants, err := s.perms.Grants(ctx, actorID)
	if err != nil {
		return userdomain.User{}, err
	}

	targetGrants, err := s.perms.Grants(ctx, targetID)
	if err != nil {
		return userdomain.User{}, err
	}

	if !actorGrants.CanImpersonate(targetGrants, s.cfg.AdminRole) {
		return userdomain.User{}, domain.ErrIneligible
	}

	return target, nil
}

// Verify accepts an impersonation token's session only while it is active, unexpired, bound to
// the same actor and target, both accounts are active, and the actor may still impersonate.
// A session that fails a policy check is revoked; an expired one is closed.
func (s *Service) Verify(ctx context.Context, sessionID string, actorID, targetID uuid.UUID) error {
	id, err := uuid.Parse(sessionID)
	if err != nil {
		return domain.ErrEnded
	}

	session, err := s.repo.Get(ctx, id)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.ErrEnded
	}

	if err != nil {
		return err
	}

	if session.ActorUUID != actorID || session.TargetUUID != targetID {
		return domain.ErrEnded
	}

	if session.State != domain.StateActive {
		return domain.ErrEnded
	}

	now := s.clock.Now()
	if !session.ActiveAt(now) {
		return s.endAndDeny(ctx, session, domain.StateExpired, domain.EndExpired, now)
	}

	allowed := session.ParticipantsActive
	if allowed {
		if allowed, err = s.perms.Enforce(ctx, actorID, domain.PermissionStart); err != nil {
			return err
		}
	}

	if !allowed {
		return s.endAndDeny(ctx, session, domain.StateRevoked, domain.EndPolicy, now)
	}

	return nil
}

func (s *Service) endAndDeny(ctx context.Context, session domain.Session, state domain.State, reason domain.EndReason, now time.Time) error {
	if _, err := s.end(ctx, session, state, reason, now); err != nil {
		return err
	}

	return domain.ErrEnded
}

// Stop ends the actor's session: sessionID when the caller holds an impersonation token,
// otherwise the actor's active session. domain.ErrNotFound when there is none.
func (s *Service) Stop(ctx context.Context, actorID uuid.UUID, sessionID *uuid.UUID) (domain.Session, error) {
	session, ok, err := s.find(ctx, actorID, sessionID)
	if err != nil {
		return domain.Session{}, err
	}

	if !ok || session.State != domain.StateActive {
		return domain.Session{}, domain.ErrNotFound
	}

	audit.SetActor(ctx, actorID)
	audit.SetResource(ctx, auditdomain.ResourceUser, session.TargetUUID)
	audit.AddMetadata(ctx, "impersonation_session_id", session.UUID.String())

	now := s.clock.Now()
	state, reason := domain.StateExited, domain.EndManual

	if !session.ActiveAt(now) {
		state, reason = domain.StateExpired, domain.EndExpired
	}

	return s.end(ctx, session, state, reason, now)
}

// Current returns the actor's session (the token's own when sessionID is set) and whether it
// is active now.
func (s *Service) Current(ctx context.Context, actorID uuid.UUID, sessionID *uuid.UUID) (domain.Session, bool, error) {
	session, ok, err := s.find(ctx, actorID, sessionID)
	if err != nil || !ok {
		return domain.Session{}, false, err
	}

	return session, session.ActiveAt(s.clock.Now()), nil
}

func (s *Service) find(ctx context.Context, actorID uuid.UUID, sessionID *uuid.UUID) (domain.Session, bool, error) {
	if sessionID == nil {
		return s.repo.ActiveForActor(ctx, actorID)
	}

	session, err := s.repo.Get(ctx, *sessionID)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.Session{}, false, nil
	}

	if err != nil {
		return domain.Session{}, false, err
	}

	return session, session.ActorUUID == actorID, nil
}

// ExpireStale closes up to limit active sessions past their expiry and returns how many it
// closed. Tokens stop working at expiry regardless; this records the end.
func (s *Service) ExpireStale(ctx context.Context, limit int) (int, error) {
	now := s.clock.Now()

	sessions, err := s.repo.Expired(ctx, now, limit)
	if err != nil {
		return 0, err
	}

	closed := 0

	for _, session := range sessions {
		ended, err := s.end(ctx, session, domain.StateExpired, domain.EndExpired, now)
		if err != nil {
			return closed, err
		}

		if ended.State == domain.StateExpired {
			closed++
		}
	}

	return closed, nil
}

// end moves an active session to state and records the matching event. A session another
// request already ended is returned unchanged.
func (s *Service) end(ctx context.Context, session domain.Session, state domain.State, reason domain.EndReason, now time.Time) (domain.Session, error) {
	var changed bool

	err := s.events.InTx(ctx, func(ctx context.Context) error {
		var err error
		if changed, err = s.repo.End(ctx, session.UUID, state, reason, now); err != nil || !changed {
			return err
		}

		return s.events.Record(ctx, lifecycleEvent(endEvents[reason], session, reason, now))
	})
	if err != nil || !changed {
		return session, err
	}

	session.State, session.EndedAt, session.EndReason = state, &now, &reason

	return session, nil
}

var endEvents = map[domain.EndReason]string{
	domain.EndManual:  event.ImpersonationExited,
	domain.EndExpired: event.ImpersonationExpired,
	domain.EndPolicy:  event.ImpersonationRevoked,
}

// lifecyclePayload is ImpersonationLifecycle in docs/architecture/asyncapi.yaml.
type lifecyclePayload struct {
	SessionID  uuid.UUID `json:"impersonation_session_id"`
	ActorID    uuid.UUID `json:"impersonator_user_id"`
	TargetID   uuid.UUID `json:"impersonated_user_id"`
	StartedAt  time.Time `json:"started_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	ReasonCode string    `json:"reason_code,omitempty"`
}

func lifecycleEvent(typ string, session domain.Session, reason domain.EndReason, at time.Time) event.Event {
	return event.New(typ, event.AggregateImpersonation, session.UUID, &session.ActorUUID, at, lifecyclePayload{
		SessionID: session.UUID, ActorID: session.ActorUUID, TargetID: session.TargetUUID,
		StartedAt: session.StartedAt, ExpiresAt: session.ExpiresAt, ReasonCode: string(reason),
	})
}
