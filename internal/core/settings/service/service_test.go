package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/core/event"
	"github.com/turahe/blog-api/internal/core/event/eventtest"
	"github.com/turahe/blog-api/internal/core/readcache"
	"github.com/turahe/blog-api/internal/core/readcache/readcachetest"
	"github.com/turahe/blog-api/internal/core/settings/domain"
	"github.com/turahe/blog-api/internal/core/settings/service"
)

var testNow = time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)

type fixedClock struct{}

func (fixedClock) Now() time.Time { return testNow }

type seqIDs struct{}

func (seqIDs) New() uuid.UUID { return uuid.New() }

type memoryRepo struct {
	mu        sync.Mutex
	rows      map[string]domain.Stored
	writes    []domain.Write
	listCalls int
}

func newRepo() *memoryRepo {
	return &memoryRepo{rows: map[string]domain.Stored{}}
}

func (r *memoryRepo) List(context.Context) ([]domain.Stored, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.listCalls++

	out := make([]domain.Stored, 0, len(r.rows))
	for _, row := range r.rows {
		out = append(out, row)
	}

	return out, nil
}

func (r *memoryRepo) Lock(_ context.Context, keys []string) ([]domain.Stored, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var out []domain.Stored

	for _, key := range keys {
		if row, ok := r.rows[key]; ok {
			out = append(out, row)
		}
	}

	return out, nil
}

func (r *memoryRepo) Save(_ context.Context, w domain.Write) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.rows[w.Key].Version != w.ExpectedVersion {
		return fmt.Errorf("%w: %s", domain.ErrVersionConflict, w.Key)
	}

	r.rows[w.Key] = domain.Stored{Key: w.Key, Value: w.Value, Version: w.ExpectedVersion + 1, UpdatedAt: w.At, UpdatedBy: w.ChangedBy}
	r.writes = append(r.writes, w)

	return nil
}

func (r *memoryRepo) History(_ context.Context, f domain.HistoryFilter) (domain.HistoryPage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	page := domain.HistoryPage{Page: f.Page, PerPage: f.PerPage}
	for _, w := range slices.Backward(r.writes) {
		if f.Key == "" || f.Key == w.Key {
			page.Items = append(page.Items, domain.HistoryEntry{
				UUID: w.HistoryID, Key: w.Key, Previous: w.Previous, New: w.Value,
				Version: w.ExpectedVersion + 1, ChangedBy: w.ChangedBy, RequestID: w.RequestID, CreatedAt: w.At,
			})
		}
	}

	page.Total = int64(len(page.Items))

	return page, nil
}

func (r *memoryRepo) put(key, raw string, version int64) {
	r.rows[key] = domain.Stored{Key: key, Value: json.RawMessage(raw), Version: version, UpdatedAt: testNow}
}

func testCatalogue() domain.Catalogue {
	return domain.NewCatalogue(
		domain.Definition{Key: "site.name", Category: domain.CategorySite, Type: domain.TypeString, Sensitivity: domain.PublicSafe, Default: "Blog", MaxLength: 20},
		domain.Definition{Key: "media.quality", Category: domain.CategoryMedia, Type: domain.TypeInteger, Sensitivity: domain.PublicSafe, Default: int64(80), Min: 1, Max: 100},
		domain.Definition{Key: "analytics.days", Category: domain.CategoryAnalytics, Type: domain.TypeInteger, Sensitivity: domain.AdminOnly, Default: int64(90), Min: 1, Max: 365},
		domain.Definition{Key: "security.internal", Category: domain.CategorySecurity, Type: domain.TypeString, Sensitivity: domain.ServerOnly, Default: "hidden"},
		domain.Definition{Key: "notifications.roles", Category: domain.CategoryNotifications, Type: domain.TypeStringList, Sensitivity: domain.PublicSafe, Default: []string{"admin"}, Enum: []string{"admin", "editor"}},
	)
}

func newService(repo *memoryRepo) (*service.Service, *eventtest.Recorder, *readcachetest.Memory) {
	recorder := &eventtest.Recorder{}
	cache := readcachetest.New()
	svc := service.New(repo, testCatalogue(), seqIDs{}, fixedClock{}).WithEvents(recorder.Unit()).WithCache(cache)

	return svc, recorder, cache
}

func keys(settings []domain.Setting) []string {
	out := make([]string, 0, len(settings))
	for _, s := range settings {
		out = append(out, s.Key)
	}

	return out
}

func TestListFiltersBySensitivityAndCategory(t *testing.T) {
	t.Parallel()

	svc, _, _ := newService(newRepo())

	got, err := svc.List(t.Context(), service.ListFilter{})
	require.NoError(t, err)
	assert.Equal(t, []string{"media.quality", "notifications.roles", "site.name"}, keys(got), "admin_only hidden, server_only never")

	got, err = svc.List(t.Context(), service.ListFilter{IncludeAdminOnly: true})
	require.NoError(t, err)
	assert.Equal(t, []string{"analytics.days", "media.quality", "notifications.roles", "site.name"}, keys(got))

	got, err = svc.List(t.Context(), service.ListFilter{Category: domain.CategorySecurity, IncludeAdminOnly: true})
	require.NoError(t, err)
	assert.Empty(t, got, "server_only keys are not listed even when their category is requested")

	got, err = svc.List(t.Context(), service.ListFilter{Category: domain.CategoryMedia})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, int64(80), got[0].Value)
	assert.True(t, got[0].Defaulted)
	assert.Zero(t, got[0].Version)

	_, err = svc.List(t.Context(), service.ListFilter{Category: "storage"})
	require.ErrorIs(t, err, service.ErrValidation)
}

func TestListFallsBackToDefaultForInvalidStoredValue(t *testing.T) {
	t.Parallel()

	repo := newRepo()
	repo.put("media.quality", `500`, 3)
	svc, _, _ := newService(repo)

	got, err := svc.List(t.Context(), service.ListFilter{Category: domain.CategoryMedia})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, int64(80), got[0].Value)
	assert.True(t, got[0].Defaulted)
	assert.EqualValues(t, 3, got[0].Version, "version kept so the next write is not a conflict")

	result, err := svc.Update(t.Context(), service.Actor{}, []service.Update{{Key: "media.quality", Value: json.RawMessage(`80`)}})
	require.NoError(t, err)
	assert.Len(t, result.Applied, 1, "an invalid stored value is replaced even with the default")
	assert.EqualValues(t, 4, repo.rows["media.quality"].Version)
}

func TestUpdateAppliesChangedKeysAndRecordsEverything(t *testing.T) {
	t.Parallel()

	repo := newRepo()
	svc, recorder, cache := newService(repo)
	actorID := uuid.New()
	actor := service.Actor{UserID: &actorID, RequestID: "req-1"}

	_, err := svc.List(t.Context(), service.ListFilter{})
	require.NoError(t, err)
	require.Equal(t, 1, repo.listCalls)

	result, err := svc.Update(t.Context(), actor, []service.Update{
		{Key: "site.name", Value: json.RawMessage(`"New Blog"`)},
		{Key: " media.quality ", Value: json.RawMessage(`80`)},
		{Key: "notifications.roles", Value: json.RawMessage(`["admin","editor"]`)},
	})
	require.NoError(t, err)

	require.Len(t, result.Applied, 2)
	assert.Equal(t, domain.Change{Key: "site.name", Previous: "Blog", New: "New Blog", Version: 1, Sensitivity: domain.PublicSafe}, result.Applied[0])
	assert.Equal(t, []string{"media.quality"}, result.Unchanged, "equal to the default: no row written")
	assert.NotContains(t, repo.rows, "media.quality")

	require.Len(t, repo.writes, 2)
	assert.JSONEq(t, `"Blog"`, string(repo.writes[0].Previous), "history keeps the default that was in effect")
	assert.Equal(t, &actorID, repo.writes[0].ChangedBy)
	assert.Equal(t, "req-1", repo.writes[0].RequestID)

	events := recorder.Events()
	require.Len(t, events, 1)
	assert.Equal(t, event.SettingsUpdated, events[0].Type)
	assert.Equal(t, service.AggregateID, events[0].AggregateID)
	payload, err := json.Marshal(events[0].Payload)
	require.NoError(t, err)
	assert.JSONEq(t, fmt.Sprintf(`{"changed_keys":[
		{"key":"site.name","previous_value":"Blog","new_value":"New Blog","sensitivity":"public_safe"},
		{"key":"notifications.roles","previous_value":["admin"],"new_value":["admin","editor"],"sensitivity":"public_safe"}],
		"actor_id":%q,"request_id":"req-1"}`, actorID), string(payload))

	assert.Equal(t, 1, cache.Invalidations(readcache.Settings))

	values, err := svc.Values(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 2, repo.listCalls, "the write invalidated the cached rows")
	assert.Equal(t, "New Blog", values.String("site.name"))
	assert.Equal(t, []string{"admin", "editor"}, values.Strings("notifications.roles"))
	assert.Equal(t, "hidden", values.String("security.internal"), "server_only keys are readable internally")

	_, err = svc.Values(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 2, repo.listCalls, "served from cache")
}

func TestUpdateWithOnlyUnchangedKeysRecordsNothing(t *testing.T) {
	t.Parallel()

	repo := newRepo()
	svc, recorder, cache := newService(repo)

	result, err := svc.Update(t.Context(), service.Actor{}, []service.Update{{Key: "site.name", Value: json.RawMessage(`"Blog"`)}})
	require.NoError(t, err)
	assert.Empty(t, result.Applied)
	assert.Equal(t, []string{"site.name"}, result.Unchanged)
	assert.Empty(t, recorder.Events())
	assert.Zero(t, cache.Invalidations(readcache.Settings))
}

func TestUpdateRejectsWholeRequestWithAllViolations(t *testing.T) {
	t.Parallel()

	repo := newRepo()
	svc, recorder, _ := newService(repo)

	_, err := svc.Update(t.Context(), service.Actor{}, []service.Update{
		{Key: "site.name", Value: json.RawMessage(`"ok"`)},
		{Key: "site.unknown", Value: json.RawMessage(`1`)},
		{Key: "media.quality", Value: json.RawMessage(`"90"`)},
		{Key: "analytics.days", Value: json.RawMessage(`0`)},
		{Key: "security.internal", Value: json.RawMessage(`"x"`)},
		{Key: "site.name", Value: json.RawMessage(`"again"`)},
	})

	var invalid *domain.ValidationError
	require.ErrorAs(t, err, &invalid)

	reasons := map[string]string{}
	for _, v := range invalid.Violations {
		reasons[v.Key] = v.Reason
	}

	assert.Equal(t, map[string]string{
		"site.unknown":      domain.ReasonUnknownKey,
		"media.quality":     domain.ReasonTypeMismatch,
		"analytics.days":    domain.ReasonOutOfRange,
		"security.internal": domain.ReasonReadOnly,
		"site.name":         domain.ReasonDuplicate,
	}, reasons)
	assert.Empty(t, repo.writes, "valid keys in a rejected request are not applied")
	assert.Empty(t, recorder.Events())
}

func TestUpdateRejectsEmptyAndOversizedBatches(t *testing.T) {
	t.Parallel()

	svc, _, _ := newService(newRepo())

	_, err := svc.Update(t.Context(), service.Actor{}, nil)
	require.ErrorIs(t, err, service.ErrValidation)

	_, err = svc.Update(t.Context(), service.Actor{}, make([]service.Update, service.MaxUpdates+1))
	require.ErrorIs(t, err, service.ErrValidation)
}

func TestUpdateChecksVersions(t *testing.T) {
	t.Parallel()

	repo := newRepo()
	repo.put("site.name", `"Blog v2"`, 2)
	svc, recorder, _ := newService(repo)

	stale, current := int64(1), int64(2)

	_, err := svc.Update(t.Context(), service.Actor{}, []service.Update{
		{Key: "site.name", Value: json.RawMessage(`"Mine"`), Version: &stale},
	})
	require.ErrorIs(t, err, domain.ErrVersionConflict)
	assert.Empty(t, repo.writes)
	assert.Empty(t, recorder.Events())

	result, err := svc.Update(t.Context(), service.Actor{}, []service.Update{
		{Key: "site.name", Value: json.RawMessage(`"Mine"`), Version: &current},
	})
	require.NoError(t, err)
	assert.EqualValues(t, 3, result.Applied[0].Version)

	zero := int64(0)
	_, err = svc.Update(t.Context(), service.Actor{}, []service.Update{
		{Key: "media.quality", Value: json.RawMessage(`70`), Version: &zero},
	})
	require.NoError(t, err, "version 0 means the caller saw the default")
}

type conflictingRepo struct {
	*memoryRepo
}

func (conflictingRepo) Save(context.Context, domain.Write) error {
	return fmt.Errorf("%w: raced", domain.ErrVersionConflict)
}

func TestUpdateSurfacesStoreConflicts(t *testing.T) {
	t.Parallel()

	recorder := &eventtest.Recorder{}
	svc := service.New(conflictingRepo{newRepo()}, testCatalogue(), seqIDs{}, fixedClock{}).WithEvents(recorder.Unit())

	_, err := svc.Update(t.Context(), service.Actor{}, []service.Update{{Key: "site.name", Value: json.RawMessage(`"x"`)}})
	require.ErrorIs(t, err, domain.ErrVersionConflict)
	assert.Empty(t, recorder.Events())
}

func TestUpdateFailsWhenEventCannotBeRecorded(t *testing.T) {
	t.Parallel()

	repo := newRepo()
	recorder := &eventtest.Recorder{Err: errors.New("outbox down")}
	cache := readcachetest.New()
	svc := service.New(repo, testCatalogue(), seqIDs{}, fixedClock{}).WithEvents(recorder.Unit()).WithCache(cache)

	_, err := svc.Update(t.Context(), service.Actor{}, []service.Update{{Key: "site.name", Value: json.RawMessage(`"x"`)}})
	require.Error(t, err)
	assert.Zero(t, cache.Invalidations(readcache.Settings), "a failed transaction does not invalidate")
}

func TestHistoryPaginatesAndRedactsUnknownKeys(t *testing.T) {
	t.Parallel()

	repo := newRepo()
	repo.writes = []domain.Write{
		{Key: "retired.key", Previous: json.RawMessage(`"a"`), Value: json.RawMessage(`"b"`)},
		{Key: "site.name", Previous: json.RawMessage(`"Blog"`), Value: json.RawMessage(`"New"`)},
	}
	svc, _, _ := newService(repo)

	page, err := svc.History(t.Context(), domain.HistoryFilter{PerPage: 1000})
	require.NoError(t, err)
	assert.Equal(t, 1, page.Page)
	assert.Equal(t, 100, page.PerPage, "per_page is capped")
	require.Len(t, page.Items, 2)
	assert.Equal(t, "site.name", page.Items[0].Key)
	assert.False(t, page.Items[0].Redacted)
	assert.JSONEq(t, `"New"`, string(page.Items[0].New))
	assert.True(t, page.Items[1].Redacted)
	assert.Nil(t, page.Items[1].Previous)
	assert.Nil(t, page.Items[1].New)

	_, err = svc.History(t.Context(), domain.HistoryFilter{Key: string(make([]byte, 200))})
	require.ErrorIs(t, err, service.ErrValidation)
}

func TestNoTransactionUnitStillApplies(t *testing.T) {
	t.Parallel()

	repo := newRepo()
	svc := service.New(repo, testCatalogue(), seqIDs{}, fixedClock{}).WithEvents(event.Unit{})

	_, err := svc.Update(t.Context(), service.Actor{}, []service.Update{{Key: "site.name", Value: json.RawMessage(`"x"`)}})
	require.NoError(t, err)
	assert.Len(t, repo.writes, 1)
}
