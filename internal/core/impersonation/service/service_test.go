package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
	"github.com/turahe/blog-api/internal/core/event"
	"github.com/turahe/blog-api/internal/core/impersonation/domain"
	"github.com/turahe/blog-api/internal/core/impersonation/service"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
)

var epoch = time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)

type fixedClock struct{ now time.Time }

func (c *fixedClock) Now() time.Time { return c.now }

type seqIDs struct{}

func (seqIDs) New() uuid.UUID { return uuid.New() }

type fakeRepo struct {
	sessions map[uuid.UUID]domain.Session
}

func (r *fakeRepo) Create(_ context.Context, s domain.Session) error {
	for _, existing := range r.sessions {
		if existing.ActorUUID == s.ActorUUID && existing.State == domain.StateActive {
			return domain.ErrAlreadyActive
		}
	}

	s.ParticipantsActive = true
	r.sessions[s.UUID] = s

	return nil
}

func (r *fakeRepo) Get(_ context.Context, id uuid.UUID) (domain.Session, error) {
	s, ok := r.sessions[id]
	if !ok {
		return domain.Session{}, domain.ErrNotFound
	}

	return s, nil
}

func (r *fakeRepo) ActiveForActor(_ context.Context, actorID uuid.UUID) (domain.Session, bool, error) {
	for _, s := range r.sessions {
		if s.ActorUUID == actorID && s.State == domain.StateActive {
			return s, true, nil
		}
	}

	return domain.Session{}, false, nil
}

func (r *fakeRepo) End(_ context.Context, id uuid.UUID, state domain.State, reason domain.EndReason, at time.Time) (bool, error) {
	s, ok := r.sessions[id]
	if !ok || s.State != domain.StateActive {
		return false, nil
	}

	s.State, s.EndReason, s.EndedAt = state, &reason, &at
	r.sessions[id] = s

	return true, nil
}

func (r *fakeRepo) Expired(_ context.Context, now time.Time, limit int) ([]domain.Session, error) {
	var out []domain.Session

	for _, s := range r.sessions {
		if s.State == domain.StateActive && !now.Before(s.ExpiresAt) && len(out) < limit {
			out = append(out, s)
		}
	}

	return out, nil
}

type fakeUsers map[uuid.UUID]userdomain.User

func (u fakeUsers) FindByID(_ context.Context, id uuid.UUID) (userdomain.User, error) {
	user, ok := u[id]
	if !ok {
		return userdomain.User{}, userdomain.ErrNotFound
	}

	return user, nil
}

type fakePerms map[uuid.UUID]domain.Grants

func (p fakePerms) Enforce(_ context.Context, userID uuid.UUID, permission string) (bool, error) {
	return p[userID].Has(permission), nil
}

func (p fakePerms) Grants(_ context.Context, userID uuid.UUID) (domain.Grants, error) {
	return p[userID], nil
}

type fakeStepUp struct{ fail bool }

func (f fakeStepUp) VerifyStepUp(context.Context, uuid.UUID, string, string) (bool, error) {
	return !f.fail, nil
}

type fakeTokens struct{ issued []authdomain.AccessClaims }

func (t *fakeTokens) IssueAccess(c authdomain.AccessClaims) (string, error) {
	t.issued = append(t.issued, c)
	return "token-" + c.SessionID, nil
}

type recorder struct{ types []string }

func (r *recorder) Record(_ context.Context, events ...event.Event) error {
	for _, e := range events {
		r.types = append(r.types, e.Type)
	}

	return nil
}

type fixture struct {
	svc     *service.Service
	repo    *fakeRepo
	users   fakeUsers
	perms   fakePerms
	tokens  *fakeTokens
	events  *recorder
	clock   *fixedClock
	stepUp  *fakeStepUp
	actor   uuid.UUID
	target  uuid.UUID
	started func(t *testing.T) service.Started
}

func newFixture(t *testing.T) *fixture {
	t.Helper()

	f := &fixture{
		repo:   &fakeRepo{sessions: map[uuid.UUID]domain.Session{}},
		users:  fakeUsers{},
		perms:  fakePerms{},
		tokens: &fakeTokens{},
		events: &recorder{},
		clock:  &fixedClock{now: epoch},
		stepUp: &fakeStepUp{},
		actor:  uuid.New(),
		target: uuid.New(),
	}
	f.users[f.actor] = userdomain.User{UUID: f.actor, Status: userdomain.StatusActive}
	f.users[f.target] = userdomain.User{UUID: f.target, Email: "t@example.com", Username: "target", Status: userdomain.StatusActive}
	f.perms[f.actor] = domain.Grants{Roles: []string{"support"}, Permissions: []string{domain.PermissionStart, "posts.read", "comments.moderate"}}
	f.perms[f.target] = domain.Grants{Roles: []string{"author"}, Permissions: []string{"posts.read"}}

	f.svc = service.New(f.repo, f.users, f.perms, f.stepUp, f.tokens, seqIDs{}, f.clock,
		service.Config{TTL: time.Hour, AdminRole: "admin"}).
		WithEvents(event.Unit{Recorder: f.events})
	f.started = func(t *testing.T) service.Started {
		t.Helper()

		out, err := f.svc.Start(t.Context(), f.input())
		require.NoError(t, err)

		return out
	}

	return f
}

func (f *fixture) input() service.StartInput {
	return service.StartInput{ActorID: f.actor, TargetID: f.target, Reason: "  ticket #4521 login issue  ", Password: "pw"}
}

func TestStartIssuesSessionBoundToken(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	out := f.started(t)

	assert.Equal(t, domain.StateActive, out.Session.State)
	assert.Equal(t, "ticket #4521 login issue", out.Session.Reason)
	assert.Equal(t, epoch.Add(time.Hour), out.Session.ExpiresAt)
	assert.Equal(t, "token-"+out.Session.UUID.String(), out.AccessToken)
	require.Len(t, f.tokens.issued, 1)

	claims := f.tokens.issued[0]
	assert.Equal(t, f.target, claims.Subject)
	require.NotNil(t, claims.Actor)
	assert.Equal(t, f.actor, *claims.Actor)
	assert.Equal(t, out.Session.UUID.String(), claims.SessionID)
	assert.Equal(t, out.Session.ExpiresAt, claims.ExpiresAt)
	assert.Equal(t, []string{event.ImpersonationStarted}, f.events.types)
}

func TestStartRejections(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		mutate func(f *fixture, in *service.StartInput)
		want   error
	}{
		"short reason": {func(_ *fixture, in *service.StartInput) { in.Reason = "   too short " }, domain.ErrValidation},
		"no permission": {func(f *fixture, _ *service.StartInput) {
			f.perms[f.actor] = domain.Grants{Permissions: []string{"posts.read"}}
		}, domain.ErrForbidden},
		"failed step-up": {func(f *fixture, _ *service.StartInput) { f.stepUp.fail = true }, domain.ErrStepUpRequired},
		"self":           {func(f *fixture, in *service.StartInput) { in.TargetID = f.actor }, domain.ErrIneligible},
		"missing target": {func(_ *fixture, in *service.StartInput) { in.TargetID = uuid.New() }, domain.ErrIneligible},
		"suspended target": {func(f *fixture, _ *service.StartInput) {
			u := f.users[f.target]
			u.Status = userdomain.StatusSuspended
			f.users[f.target] = u
		}, domain.ErrIneligible},
		"admin target": {func(f *fixture, _ *service.StartInput) {
			f.perms[f.target] = domain.Grants{Roles: []string{"admin"}}
		}, domain.ErrIneligible},
		"peer impersonator": {func(f *fixture, _ *service.StartInput) {
			f.perms[f.target] = domain.Grants{Permissions: []string{domain.PermissionStart}}
		}, domain.ErrIneligible},
		"target has more rights": {func(f *fixture, _ *service.StartInput) {
			f.perms[f.target] = domain.Grants{Permissions: []string{"posts.read", "settings.update"}}
		}, domain.ErrIneligible},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFixture(t)
			in := f.input()
			tc.mutate(f, &in)

			_, err := f.svc.Start(t.Context(), in)
			require.ErrorIs(t, err, tc.want)
			assert.Empty(t, f.repo.sessions)
			assert.Empty(t, f.tokens.issued)
		})
	}
}

func TestStartRejectsSecondActiveSession(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	f.started(t)

	_, err := f.svc.Start(t.Context(), f.input())
	require.ErrorIs(t, err, domain.ErrAlreadyActive)
}

func TestVerify(t *testing.T) {
	t.Parallel()

	t.Run("active session passes", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		out := f.started(t)
		require.NoError(t, f.svc.Verify(t.Context(), out.Session.UUID.String(), f.actor, f.target))
	})

	t.Run("mismatched or unknown session is ended", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		out := f.started(t)
		require.ErrorIs(t, f.svc.Verify(t.Context(), out.Session.UUID.String(), f.target, f.actor), domain.ErrEnded)
		require.ErrorIs(t, f.svc.Verify(t.Context(), uuid.NewString(), f.actor, f.target), domain.ErrEnded)
		require.ErrorIs(t, f.svc.Verify(t.Context(), "not-a-uuid", f.actor, f.target), domain.ErrEnded)
	})

	t.Run("expiry closes the session", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		out := f.started(t)
		f.clock.now = out.Session.ExpiresAt

		require.ErrorIs(t, f.svc.Verify(t.Context(), out.Session.UUID.String(), f.actor, f.target), domain.ErrEnded)
		assert.Equal(t, domain.StateExpired, f.repo.sessions[out.Session.UUID].State)
		assert.Equal(t, []string{event.ImpersonationStarted, event.ImpersonationExpired}, f.events.types)
	})

	t.Run("lost permission revokes", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		out := f.started(t)
		f.perms[f.actor] = domain.Grants{}

		require.ErrorIs(t, f.svc.Verify(t.Context(), out.Session.UUID.String(), f.actor, f.target), domain.ErrEnded)
		assert.Equal(t, domain.StateRevoked, f.repo.sessions[out.Session.UUID].State)
		assert.Equal(t, []string{event.ImpersonationStarted, event.ImpersonationRevoked}, f.events.types)
	})

	t.Run("inactive participant revokes", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		out := f.started(t)
		s := f.repo.sessions[out.Session.UUID]
		s.ParticipantsActive = false
		f.repo.sessions[out.Session.UUID] = s

		require.ErrorIs(t, f.svc.Verify(t.Context(), out.Session.UUID.String(), f.actor, f.target), domain.ErrEnded)
		assert.Equal(t, domain.StateRevoked, f.repo.sessions[out.Session.UUID].State)
	})
}

func TestStopAndCurrent(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	out := f.started(t)

	current, active, err := f.svc.Current(t.Context(), f.actor, nil)
	require.NoError(t, err)
	assert.True(t, active)
	assert.Equal(t, out.Session.UUID, current.UUID)

	_, _, err = f.svc.Current(t.Context(), f.target, &out.Session.UUID)
	require.NoError(t, err)

	stopped, err := f.svc.Stop(t.Context(), f.actor, &out.Session.UUID)
	require.NoError(t, err)
	assert.Equal(t, domain.StateExited, stopped.State)
	require.NotNil(t, stopped.EndReason)
	assert.Equal(t, domain.EndManual, *stopped.EndReason)
	assert.Equal(t, []string{event.ImpersonationStarted, event.ImpersonationExited}, f.events.types)

	_, err = f.svc.Stop(t.Context(), f.actor, nil)
	require.ErrorIs(t, err, domain.ErrNotFound)

	_, active, err = f.svc.Current(t.Context(), f.actor, &out.Session.UUID)
	require.NoError(t, err)
	assert.False(t, active)

	require.ErrorIs(t, f.svc.Verify(t.Context(), out.Session.UUID.String(), f.actor, f.target), domain.ErrEnded)
}

func TestStopRejectsAnotherActorsSession(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	out := f.started(t)

	_, err := f.svc.Stop(t.Context(), uuid.New(), &out.Session.UUID)
	require.ErrorIs(t, err, domain.ErrNotFound)
	assert.Equal(t, domain.StateActive, f.repo.sessions[out.Session.UUID].State)
}

func TestExpireStale(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	f.started(t)

	closed, err := f.svc.ExpireStale(t.Context(), 10)
	require.NoError(t, err)
	assert.Zero(t, closed)

	f.clock.now = epoch.Add(2 * time.Hour)
	closed, err = f.svc.ExpireStale(t.Context(), 10)
	require.NoError(t, err)
	assert.Equal(t, 1, closed)
	assert.Equal(t, []string{event.ImpersonationStarted, event.ImpersonationExpired}, f.events.types)
}
