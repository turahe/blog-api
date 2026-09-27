package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/core/consent/domain"
	"github.com/turahe/blog-api/internal/core/event"
	"github.com/turahe/blog-api/internal/core/event/eventtest"
)

type randomIDs struct{}

func (randomIDs) New() uuid.UUID { return uuid.New() }

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

type memRepo struct {
	subjects map[uuid.UUID]domain.Subject
	byHash   map[string]uuid.UUID
	consents map[uuid.UUID]domain.Consent
	erased   []uuid.UUID

	subjectErr error
	createErr  error
	touchErr   error
	listErr    error
	getErr     error
	saveErr    error
	eraseErr   error
}

func (m *memRepo) DeleteSubjectEvents(_ context.Context, subject uuid.UUID) error {
	if m.eraseErr != nil {
		return m.eraseErr
	}

	m.erased = append(m.erased, subject)

	return nil
}

func newMemRepo() *memRepo {
	return &memRepo{subjects: map[uuid.UUID]domain.Subject{}, byHash: map[string]uuid.UUID{}, consents: map[uuid.UUID]domain.Consent{}}
}

func (m *memRepo) CreateSubject(_ context.Context, s domain.Subject, hash string) error {
	if m.createErr != nil {
		return m.createErr
	}

	m.subjects[s.UUID] = s
	m.byHash[hash] = s.UUID

	return nil
}

func (m *memRepo) SubjectByToken(_ context.Context, hash string) (domain.Subject, error) {
	if m.subjectErr != nil {
		return domain.Subject{}, m.subjectErr
	}

	id, ok := m.byHash[hash]
	if !ok {
		return domain.Subject{}, domain.ErrNotFound
	}

	return m.subjects[id], nil
}

func (m *memRepo) SetUser(_ context.Context, id uuid.UUID, user *uuid.UUID) error {
	s := m.subjects[id]
	s.UserUUID = user
	m.subjects[id] = s

	return nil
}

func (m *memRepo) Touch(_ context.Context, id uuid.UUID, at time.Time) error {
	if m.touchErr != nil {
		return m.touchErr
	}

	s := m.subjects[id]
	s.LastSeenAt = at
	m.subjects[id] = s

	return nil
}

func (m *memRepo) List(_ context.Context, id uuid.UUID) ([]domain.Consent, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}

	var out []domain.Consent

	for _, purpose := range domain.Purposes {
		for _, c := range m.consents {
			if c.SubjectUUID == id && c.Purpose == purpose {
				out = append(out, c)
			}
		}
	}

	return out, nil
}

func (m *memRepo) Get(_ context.Context, id uuid.UUID) (domain.Consent, domain.Subject, error) {
	if m.getErr != nil {
		return domain.Consent{}, domain.Subject{}, m.getErr
	}

	c, ok := m.consents[id]
	if !ok {
		return domain.Consent{}, domain.Subject{}, domain.ErrNotFound
	}

	return c, m.subjects[c.SubjectUUID], nil
}

func (m *memRepo) Save(_ context.Context, c domain.Consent) error {
	if m.saveErr != nil {
		return m.saveErr
	}

	for id, existing := range m.consents {
		if existing.SubjectUUID == c.SubjectUUID && existing.Purpose == c.Purpose {
			delete(m.consents, id)
		}
	}

	m.consents[c.UUID] = c

	return nil
}

func (m *memRepo) DeleteForUser(_ context.Context, user uuid.UUID) (int64, error) {
	var n int64

	for id, s := range m.subjects {
		if s.UserUUID != nil && *s.UserUUID == user {
			delete(m.subjects, id)

			n++
		}
	}

	return n, nil
}

var now = time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)

func newService() (*Service, *memRepo, *eventtest.Recorder) {
	repo := newMemRepo()
	events := &eventtest.Recorder{}

	return New(repo, randomIDs{}, fixedClock{now: now}).WithEvents(events.Unit()), repo, events
}

func statuses(consents []domain.Consent) map[domain.Purpose]domain.Status {
	out := map[domain.Purpose]domain.Status{}
	for _, c := range consents {
		out[c.Purpose] = c.Status
	}

	return out
}

func TestStoreCreatesSubjectAndReturnsTokenOnce(t *testing.T) {
	t.Parallel()

	svc, repo, events := newService()

	state, err := svc.Store(t.Context(), "", nil, map[domain.Purpose]bool{domain.PurposeAnalytics: true}, "2026-09")
	require.NoError(t, err)
	require.NotEmpty(t, state.Token)
	assert.Equal(t, map[domain.Purpose]domain.Status{domain.PurposeAnalytics: domain.StatusGranted}, statuses(state.Consents))
	assert.Equal(t, []string{event.AnalyticsConsentGranted}, events.Types())

	_, stored := repo.byHash[domain.HashToken(state.Token)]
	assert.True(t, stored, "only the token hash is stored")

	again, err := svc.Store(t.Context(), state.Token, nil, map[domain.Purpose]bool{domain.PurposeAnalytics: true}, "2026-09")
	require.NoError(t, err)
	assert.Empty(t, again.Token, "an existing subject gets no new token")
	assert.Equal(t, state.Subject.UUID, again.Subject.UUID)
	assert.Len(t, events.Types(), 1, "an unchanged decision records nothing")

	fresh, err := svc.Store(t.Context(), "unknown-token", nil, map[domain.Purpose]bool{domain.PurposeAnalytics: false}, "2026-09")
	require.NoError(t, err)
	assert.NotEmpty(t, fresh.Token, "an unknown token starts a new subject")
	assert.Equal(t, domain.StatusRejected, fresh.Consents[0].Status)
}

func TestRefusingGrantedConsentWithdrawsIt(t *testing.T) {
	t.Parallel()

	svc, repo, events := newService()

	state, err := svc.Store(t.Context(), "", nil, map[domain.Purpose]bool{domain.PurposeAnalytics: true}, "v1")
	require.NoError(t, err)
	assert.Empty(t, repo.erased)

	state, err = svc.Store(t.Context(), state.Token, nil, map[domain.Purpose]bool{domain.PurposeAnalytics: false}, "v1")
	require.NoError(t, err)
	assert.Equal(t, []uuid.UUID{state.Subject.UUID}, repo.erased, "withdrawing analytics deletes the events already stored")

	require.Len(t, state.Consents, 1)
	assert.Equal(t, domain.StatusWithdrawn, state.Consents[0].Status)
	assert.Equal(t, &now, state.Consents[0].WithdrawnAt)
	assert.Equal(t, []string{event.AnalyticsConsentGranted, event.AnalyticsConsentWithdrawn}, events.Types())
}

func TestStoreValidation(t *testing.T) {
	t.Parallel()

	svc, _, _ := newService()
	user := uuid.New()

	cases := map[string]struct {
		decisions map[domain.Purpose]bool
		version   string
		user      *uuid.UUID
	}{
		"no decisions":        {decisions: map[domain.Purpose]bool{}, version: "v1"},
		"unknown purpose":     {decisions: map[domain.Purpose]bool{"marketing": true}, version: "v1"},
		"bad version":         {decisions: map[domain.Purpose]bool{domain.PurposeAnalytics: true}, version: "v 1"},
		"anonymous linking":   {decisions: map[domain.Purpose]bool{domain.PurposeAnalytics: true, domain.PurposeAuthenticatedAnalytics: true}, version: "v1"},
		"linking needs basis": {decisions: map[domain.Purpose]bool{domain.PurposeAuthenticatedAnalytics: true}, version: "v1", user: &user},
	}

	for name, tc := range cases {
		_, err := svc.Store(t.Context(), "", tc.user, tc.decisions, tc.version)
		require.ErrorIs(t, err, domain.ErrValidation, name)
	}
}

func TestStoreFailures(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")
	grant := map[domain.Purpose]bool{domain.PurposeAnalytics: true}
	refuse := map[domain.Purpose]bool{domain.PurposeAnalytics: false}

	tests := []struct {
		name      string
		existing  bool
		decisions map[domain.Purpose]bool
		breakIt   func(*memRepo, *eventtest.Recorder)
	}{
		{name: "token lookup fails", existing: true, decisions: grant, breakIt: func(r *memRepo, _ *eventtest.Recorder) { r.subjectErr = boom }},
		{name: "touch fails", existing: true, decisions: grant, breakIt: func(r *memRepo, _ *eventtest.Recorder) { r.touchErr = boom }},
		{name: "subject cannot be created", decisions: grant, breakIt: func(r *memRepo, _ *eventtest.Recorder) { r.createErr = boom }},
		{name: "consents cannot be listed", decisions: grant, breakIt: func(r *memRepo, _ *eventtest.Recorder) { r.listErr = boom }},
		{name: "consent cannot be saved", decisions: grant, breakIt: func(r *memRepo, _ *eventtest.Recorder) { r.saveErr = boom }},
		{name: "event cannot be recorded", decisions: grant, breakIt: func(_ *memRepo, e *eventtest.Recorder) { e.Err = boom }},
		{name: "stored events cannot be erased", existing: true, decisions: refuse, breakIt: func(r *memRepo, _ *eventtest.Recorder) { r.eraseErr = boom }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc, repo, events := newService()

			token := ""
			if tt.existing {
				state, err := svc.Store(t.Context(), "", nil, grant, "v1")
				require.NoError(t, err)

				token = state.Token
			}

			tt.breakIt(repo, events)

			state, err := svc.Store(t.Context(), token, nil, tt.decisions, "v1")
			require.ErrorIs(t, err, boom)
			assert.Equal(t, State{}, state)
		})
	}
}

func TestAuthenticatedAnalyticsLinksAndWithdrawUnlinks(t *testing.T) {
	t.Parallel()

	svc, repo, _ := newService()
	user := uuid.New()

	state, err := svc.Store(t.Context(), "", &user, map[domain.Purpose]bool{
		domain.PurposeAnalytics: true, domain.PurposeAuthenticatedAnalytics: true,
	}, "v1")
	require.NoError(t, err)
	assert.Equal(t, &user, repo.subjects[state.Subject.UUID].UserUUID)

	var analyticsID uuid.UUID

	for _, c := range state.Consents {
		if c.Purpose == domain.PurposeAnalytics {
			analyticsID = c.UUID
		}
	}

	_, err = svc.Withdraw(t.Context(), "", nil, analyticsID)
	require.ErrorIs(t, err, domain.ErrNotFound, "without the token or the linked user the consent is hidden")

	stranger := uuid.New()
	_, err = svc.Withdraw(t.Context(), "", &stranger, analyticsID)
	require.ErrorIs(t, err, domain.ErrNotFound)

	withdrawn, err := svc.Withdraw(t.Context(), "", &user, analyticsID)
	require.NoError(t, err)
	assert.Equal(t, domain.StatusWithdrawn, withdrawn.Status)

	current, err := svc.Current(t.Context(), state.Token)
	require.NoError(t, err)
	assert.Equal(t, map[domain.Purpose]domain.Status{
		domain.PurposeAnalytics: domain.StatusWithdrawn, domain.PurposeAuthenticatedAnalytics: domain.StatusWithdrawn,
	}, statuses(current.Consents))
	assert.Nil(t, repo.subjects[state.Subject.UUID].UserUUID)
	assert.Equal(t, []uuid.UUID{state.Subject.UUID}, repo.erased, "once, for the analytics purpose")
}

func TestWithdrawingAuthenticatedAnalyticsKeepsAnalytics(t *testing.T) {
	t.Parallel()

	svc, repo, events := newService()
	user := uuid.New()

	state, err := svc.Store(t.Context(), "", &user, map[domain.Purpose]bool{
		domain.PurposeAnalytics: true, domain.PurposeAuthenticatedAnalytics: true,
	}, "v1")
	require.NoError(t, err)

	var linkedID uuid.UUID

	for _, c := range state.Consents {
		if c.Purpose == domain.PurposeAuthenticatedAnalytics {
			linkedID = c.UUID
		}
	}

	withdrawn, err := svc.Withdraw(t.Context(), state.Token, nil, linkedID)
	require.NoError(t, err)
	assert.Equal(t, domain.StatusWithdrawn, withdrawn.Status)

	current, err := svc.Current(t.Context(), state.Token)
	require.NoError(t, err)
	assert.Equal(t, map[domain.Purpose]domain.Status{
		domain.PurposeAnalytics: domain.StatusGranted, domain.PurposeAuthenticatedAnalytics: domain.StatusWithdrawn,
	}, statuses(current.Consents))
	assert.Nil(t, repo.subjects[state.Subject.UUID].UserUUID)
	assert.Empty(t, repo.erased, "analytics stays granted")
	assert.Equal(t, event.AnalyticsConsentWithdrawn, events.Types()[len(events.Types())-1])
}

func TestWithdrawFailures(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")

	tests := []struct {
		name    string
		breakIt func(*memRepo, *eventtest.Recorder)
	}{
		{name: "consent lookup fails", breakIt: func(r *memRepo, _ *eventtest.Recorder) { r.getErr = boom }},
		{name: "consents cannot be listed", breakIt: func(r *memRepo, _ *eventtest.Recorder) { r.listErr = boom }},
		{name: "withdrawal cannot be saved", breakIt: func(r *memRepo, _ *eventtest.Recorder) { r.saveErr = boom }},
		{name: "event cannot be recorded", breakIt: func(_ *memRepo, e *eventtest.Recorder) { e.Err = boom }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc, repo, events := newService()

			state, err := svc.Store(t.Context(), "", nil, map[domain.Purpose]bool{domain.PurposeAnalytics: true}, "v1")
			require.NoError(t, err)

			tt.breakIt(repo, events)

			got, err := svc.Withdraw(t.Context(), state.Token, nil, state.Consents[0].UUID)
			require.ErrorIs(t, err, boom)
			assert.Equal(t, domain.Consent{}, got)
		})
	}
}

func TestCurrentFailures(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")

	tests := []struct {
		name    string
		token   func(State) string
		breakIt func(*memRepo)
		wantErr error
	}{
		{name: "no token", token: func(State) string { return "" }, breakIt: func(*memRepo) {}, wantErr: domain.ErrNotFound},
		{name: "unknown token", token: func(State) string { return "unknown" }, breakIt: func(*memRepo) {}, wantErr: domain.ErrNotFound},
		{name: "touch fails", token: func(s State) string { return s.Token }, breakIt: func(r *memRepo) { r.touchErr = boom }, wantErr: boom},
		{name: "list fails", token: func(s State) string { return s.Token }, breakIt: func(r *memRepo) { r.listErr = boom }, wantErr: boom},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc, repo, _ := newService()

			state, err := svc.Store(t.Context(), "", nil, map[domain.Purpose]bool{domain.PurposeAnalytics: true}, "v1")
			require.NoError(t, err)

			tt.breakIt(repo)

			_, err = svc.Current(t.Context(), tt.token(state))
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestDecision(t *testing.T) {
	t.Parallel()

	svc, repo, _ := newService()

	for _, token := range []string{"", "unknown"} {
		d, err := svc.Decision(t.Context(), token, domain.PurposeAnalytics)
		require.NoError(t, err)
		assert.Equal(t, Decision{}, d, token)
		assert.False(t, d.Granted() || d.Refused())
	}

	state, err := svc.Store(t.Context(), "", nil, map[domain.Purpose]bool{domain.PurposeAnalytics: true}, "v1")
	require.NoError(t, err)

	earlier := now.Add(-time.Hour)
	subject := repo.subjects[state.Subject.UUID]
	subject.LastSeenAt = earlier
	repo.subjects[state.Subject.UUID] = subject

	d, err := svc.Decision(t.Context(), state.Token, domain.PurposeAnalytics)
	require.NoError(t, err)
	assert.Equal(t, Decision{Subject: state.Subject.UUID, Status: domain.StatusGranted, Found: true}, d)
	assert.True(t, d.Granted())
	assert.Equal(t, earlier, repo.subjects[state.Subject.UUID].LastSeenAt, "a decision lookup writes nothing")

	d, err = svc.Decision(t.Context(), state.Token, domain.PurposeAuthenticatedAnalytics)
	require.NoError(t, err)
	assert.True(t, d.Found)
	assert.Empty(t, d.Status, "no decision yet for this purpose")

	_, err = svc.Store(t.Context(), state.Token, nil, map[domain.Purpose]bool{domain.PurposeAnalytics: false}, "v1")
	require.NoError(t, err)

	d, err = svc.Decision(t.Context(), state.Token, domain.PurposeAnalytics)
	require.NoError(t, err)
	assert.True(t, d.Refused())
	assert.False(t, d.Granted())
}

func TestAllowed(t *testing.T) {
	t.Parallel()

	svc, _, _ := newService()

	allowed, err := svc.Allowed(t.Context(), "", domain.PurposeAnalytics)
	require.NoError(t, err)
	assert.False(t, allowed)

	state, err := svc.Store(t.Context(), "", nil, map[domain.Purpose]bool{domain.PurposeAnalytics: true}, "v1")
	require.NoError(t, err)

	allowed, err = svc.Allowed(t.Context(), state.Token, domain.PurposeAnalytics)
	require.NoError(t, err)
	assert.True(t, allowed)

	allowed, err = svc.Allowed(t.Context(), state.Token, domain.PurposeAuthenticatedAnalytics)
	require.NoError(t, err)
	assert.False(t, allowed)

	_, err = svc.Withdraw(t.Context(), state.Token, nil, state.Consents[0].UUID)
	require.NoError(t, err)

	allowed, err = svc.Allowed(t.Context(), state.Token, domain.PurposeAnalytics)
	require.NoError(t, err)
	assert.False(t, allowed)
}

func TestDecisionAndAllowedPassThroughFailures(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")

	tests := []struct {
		name    string
		breakIt func(*memRepo)
	}{
		{name: "token lookup fails", breakIt: func(r *memRepo) { r.subjectErr = boom }},
		{name: "list fails", breakIt: func(r *memRepo) { r.listErr = boom }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc, repo, _ := newService()

			state, err := svc.Store(t.Context(), "", nil, map[domain.Purpose]bool{domain.PurposeAnalytics: true}, "v1")
			require.NoError(t, err)

			tt.breakIt(repo)

			d, err := svc.Decision(t.Context(), state.Token, domain.PurposeAnalytics)
			require.ErrorIs(t, err, boom)
			assert.Equal(t, Decision{}, d)

			allowed, err := svc.Allowed(t.Context(), state.Token, domain.PurposeAnalytics)
			require.ErrorIs(t, err, boom)
			assert.False(t, allowed)
		})
	}
}

func TestDeleteForUserRemovesLinkedSubjects(t *testing.T) {
	t.Parallel()

	svc, repo, _ := newService()
	user := uuid.New()

	linked, err := svc.Store(t.Context(), "", &user, map[domain.Purpose]bool{
		domain.PurposeAnalytics: true, domain.PurposeAuthenticatedAnalytics: true,
	}, "v1")
	require.NoError(t, err)

	anonymous, err := svc.Store(t.Context(), "", nil, map[domain.Purpose]bool{domain.PurposeAnalytics: true}, "v1")
	require.NoError(t, err)

	n, err := svc.DeleteForUser(t.Context(), user)
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)
	assert.NotContains(t, repo.subjects, linked.Subject.UUID)
	assert.Contains(t, repo.subjects, anonymous.Subject.UUID)
}
