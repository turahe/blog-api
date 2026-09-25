package service

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/core/event"
	"github.com/turahe/blog-api/internal/core/event/eventtest"
	"github.com/turahe/blog-api/internal/core/privacy/domain"
	"github.com/turahe/blog-api/internal/core/readcache"
	"github.com/turahe/blog-api/internal/core/readcache/readcachetest"
)

type movableClock struct{ now time.Time }

func (c *movableClock) Now() time.Time { return c.now }

type uuids struct{}

func (uuids) New() uuid.UUID { return uuid.New() }

type memRepo struct {
	mu       sync.Mutex
	requests []domain.Request
}

func (r *memRepo) Create(_ context.Context, request domain.Request) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, existing := range r.requests {
		if existing.UserUUID == request.UserUUID && existing.Kind == request.Kind && existing.Open() {
			return domain.ErrAlreadyOpen
		}
	}

	r.requests = append(r.requests, request)

	return nil
}

func (r *memRepo) Latest(_ context.Context, userID uuid.UUID, kind domain.Kind) (domain.Request, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, request := range slices.Backward(r.requests) {
		if request.UserUUID == userID && request.Kind == kind {
			return request, nil
		}
	}

	return domain.Request{}, domain.ErrNotFound
}

func (r *memRepo) ClaimNext(_ context.Context, now, staleBefore time.Time) (domain.Request, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for i, request := range r.requests {
		stale := request.Status == domain.StatusRunning && request.StartedAt.Before(staleBefore)
		if request.Status == domain.StatusPending || stale {
			request.Status, request.StartedAt = domain.StatusRunning, &now
			request.Attempts++
			r.requests[i] = request

			return request, true, nil
		}
	}

	return domain.Request{}, false, nil
}

func (r *memRepo) update(id uuid.UUID, fn func(*domain.Request)) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for i := range r.requests {
		if r.requests[i].UUID == id {
			fn(&r.requests[i])
			return nil
		}
	}

	return domain.ErrNotFound
}

func (r *memRepo) Complete(_ context.Context, id uuid.UUID, key *string, expires *time.Time, at time.Time) error {
	return r.update(id, func(request *domain.Request) {
		request.Status, request.StorageKey, request.ExpiresAt, request.CompletedAt = domain.StatusCompleted, key, expires, &at
	})
}

func (r *memRepo) Fail(_ context.Context, id uuid.UUID, message string, final bool, _ time.Time) error {
	return r.update(id, func(request *domain.Request) {
		request.Status, request.LastError = domain.StatusPending, &message
		if final {
			request.Status = domain.StatusFailed
		}
	})
}

func (r *memRepo) Archives(_ context.Context, userID *uuid.UUID, before time.Time, limit int) ([]domain.Request, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var out []domain.Request

	for _, request := range r.requests {
		if request.StorageKey == nil || (userID != nil && request.UserUUID != *userID) {
			continue
		}

		if !before.IsZero() && !request.ExpiresAt.Before(before) {
			continue
		}

		out = append(out, request)
	}

	return out[:min(len(out), limit)], nil
}

func (r *memRepo) ClearArchive(_ context.Context, id uuid.UUID) error {
	return r.update(id, func(request *domain.Request) { request.StorageKey = nil })
}

type memArchives struct {
	objects map[string][]byte
	putErr  error
}

func (a *memArchives) PutObject(_ context.Context, key, _ string, body []byte) error {
	if a.putErr != nil {
		return a.putErr
	}

	a.objects[key] = body

	return nil
}

func (a *memArchives) PresignGet(_ context.Context, key string, ttl time.Duration) (string, error) {
	return "https://s3.test/" + key + "?ttl=" + ttl.String(), nil
}

func (a *memArchives) DeleteObject(_ context.Context, key string) error {
	delete(a.objects, key)
	return nil
}

type fakeData struct{ err error }

func (d fakeData) ExportUser(_ context.Context, userID uuid.UUID, _ time.Time) ([]byte, error) {
	return []byte(`{"account":{"id":"` + userID.String() + `"}}`), d.err
}

type fakeEraser struct{ erased []uuid.UUID }

func (e *fakeEraser) EraseUser(_ context.Context, userID uuid.UUID, _ time.Time) error {
	e.erased = append(e.erased, userID)
	return nil
}

type fakeVerifier struct{}

func (fakeVerifier) VerifyPassword(_ context.Context, _ uuid.UUID, password string) (bool, error) {
	return password == "correct horse", nil
}

type fixture struct {
	svc      *Service
	repo     *memRepo
	archives *memArchives
	eraser   *fakeEraser
	events   *eventtest.Recorder
	cache    *readcachetest.Memory
	clock    *movableClock
}

func newFixture(data fakeData) fixture {
	f := fixture{
		repo:     &memRepo{},
		archives: &memArchives{objects: map[string][]byte{}},
		eraser:   &fakeEraser{},
		events:   &eventtest.Recorder{},
		cache:    readcachetest.New(),
		clock:    &movableClock{now: time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)},
	}
	f.svc = New(f.repo, data, f.eraser, fakeVerifier{}, uuids{}, f.clock,
		Config{ExportRetention: 72 * time.Hour, DownloadTTL: 15 * time.Minute}).
		WithArchives(f.archives).WithEvents(f.events.Unit()).WithCache(f.cache)

	return f
}

func TestExportLifecycle(t *testing.T) {
	t.Parallel()

	f := newFixture(fakeData{})
	ctx, user := t.Context(), uuid.New()

	export, created, err := f.svc.RequestExport(ctx, user)
	require.NoError(t, err)
	require.True(t, created)
	require.Equal(t, domain.StatusPending, export.Request.Status)
	require.Equal(t, []string{event.UserExportRequested}, f.events.Types())

	again, created, err := f.svc.RequestExport(ctx, user)
	require.NoError(t, err)
	require.False(t, created, "an open request is returned instead of queuing another")
	require.Equal(t, export.Request.UUID, again.Request.UUID)

	done, err := f.svc.ProcessPending(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, 1, done)
	require.Len(t, f.archives.objects, 1)

	ready, created, err := f.svc.RequestExport(ctx, user)
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, domain.StatusCompleted, ready.Request.Status)
	require.True(t, strings.HasPrefix(*ready.Request.StorageKey, "privacy-exports/"+user.String()+"/"))
	require.Contains(t, ready.DownloadURL, "ttl=15m0s")
	require.Equal(t, f.clock.now.Add(15*time.Minute), *ready.DownloadExpiresAt)

	f.clock.now = f.clock.now.Add(72*time.Hour - 5*time.Minute)
	ready, _, err = f.svc.RequestExport(ctx, user)
	require.NoError(t, err)
	require.Contains(t, ready.DownloadURL, "ttl=5m0s", "links never outlive the archive")

	f.clock.now = f.clock.now.Add(10 * time.Minute)
	purged, err := f.svc.PurgeExpired(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, purged)
	require.Empty(t, f.archives.objects)

	next, created, err := f.svc.RequestExport(ctx, user)
	require.NoError(t, err)
	require.True(t, created, "an expired export is replaced by a new request")
	require.NotEqual(t, export.Request.UUID, next.Request.UUID)
}

func TestExportRetriesThenFails(t *testing.T) {
	t.Parallel()

	f := newFixture(fakeData{err: errors.New("database down")})
	ctx := t.Context()

	_, _, err := f.svc.RequestExport(ctx, uuid.New())
	require.NoError(t, err)

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		done, err := f.svc.ProcessPending(ctx, 1)
		require.Error(t, err)
		require.Zero(t, done)
	}

	require.Equal(t, domain.StatusFailed, f.repo.requests[0].Status)
	require.Equal(t, "database down", *f.repo.requests[0].LastError)

	done, err := f.svc.ProcessPending(ctx, 1)
	require.NoError(t, err)
	require.Zero(t, done, "failed requests are not retried again")
}

func TestExportUnavailableWithoutStorage(t *testing.T) {
	t.Parallel()

	f := newFixture(fakeData{})
	f.svc.archives = nil

	_, _, err := f.svc.RequestExport(t.Context(), uuid.New())
	require.ErrorIs(t, err, domain.ErrExportUnavailable)
}

func TestEraseRequiresPasswordAndPurgesArchives(t *testing.T) {
	t.Parallel()

	f := newFixture(fakeData{})
	ctx, user := t.Context(), uuid.New()

	_, _, err := f.svc.RequestErase(ctx, user, "")
	require.ErrorIs(t, err, domain.ErrReauth)

	_, _, err = f.svc.RequestErase(ctx, user, "wrong")
	require.ErrorIs(t, err, domain.ErrReauth)

	_, _, err = f.svc.RequestExport(ctx, user)
	require.NoError(t, err)
	_, err = f.svc.ProcessPending(ctx, 10)
	require.NoError(t, err)
	require.Len(t, f.archives.objects, 1)

	request, created, err := f.svc.RequestErase(ctx, user, "correct horse")
	require.NoError(t, err)
	require.True(t, created)

	again, created, err := f.svc.RequestErase(ctx, user, "correct horse")
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, request.UUID, again.UUID)

	done, err := f.svc.ProcessPending(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, 1, done)
	require.Equal(t, []uuid.UUID{user}, f.eraser.erased)
	require.Empty(t, f.archives.objects, "export archives are deleted before anonymizing")
	require.Equal(t, 1, f.cache.Invalidations(readcache.Users))
	require.Equal(t, 1, f.cache.Invalidations(readcache.Posts))
	require.Contains(t, f.events.Types(), event.UserErasureRequested)

	latest, err := f.repo.Latest(ctx, user, domain.KindErase)
	require.NoError(t, err)
	require.Equal(t, domain.StatusCompleted, latest.Status)
}

func TestStaleRunningRequestIsReclaimed(t *testing.T) {
	t.Parallel()

	f := newFixture(fakeData{})
	ctx := t.Context()

	_, _, err := f.svc.RequestExport(ctx, uuid.New())
	require.NoError(t, err)

	_, ok, err := f.repo.ClaimNext(ctx, f.clock.now, f.clock.now.Add(-staleAfter))
	require.NoError(t, err)
	require.True(t, ok)

	done, err := f.svc.ProcessPending(ctx, 1)
	require.NoError(t, err)
	require.Zero(t, done, "a fresh running request is left alone")

	f.clock.now = f.clock.now.Add(staleAfter + time.Minute)
	done, err = f.svc.ProcessPending(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, 1, done)
}
