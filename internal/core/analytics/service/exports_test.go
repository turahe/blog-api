package service

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/core/analytics/domain"
	"github.com/turahe/blog-api/internal/core/analytics/ports"
)

type fakeExportRepo struct {
	exports   map[uuid.UUID]domain.Export
	createErr error
	failed    []bool
}

func newFakeExportRepo() *fakeExportRepo {
	return &fakeExportRepo{exports: map[uuid.UUID]domain.Export{}}
}

func (f *fakeExportRepo) Create(_ context.Context, export domain.Export) error {
	if f.createErr != nil {
		return f.createErr
	}

	f.exports[export.UUID] = export

	return nil
}

func (f *fakeExportRepo) Get(_ context.Context, id, userID uuid.UUID) (domain.Export, error) {
	export, ok := f.exports[id]
	if !ok || export.RequestedBy != userID {
		return domain.Export{}, domain.ErrExportNotFound
	}

	return export, nil
}

func (f *fakeExportRepo) Open(_ context.Context, userID uuid.UUID) (domain.Export, error) {
	for _, export := range f.exports {
		if export.RequestedBy == userID && export.Open() {
			return export, nil
		}
	}

	return domain.Export{}, domain.ErrExportNotFound
}

func (f *fakeExportRepo) List(_ context.Context, userID uuid.UUID, _ int) ([]domain.Export, error) {
	var out []domain.Export

	for _, export := range f.exports {
		if export.RequestedBy == userID {
			out = append(out, export)
		}
	}

	return out, nil
}

func (f *fakeExportRepo) ClaimNext(_ context.Context, now, _ time.Time) (domain.Export, bool, error) {
	for id, export := range f.exports {
		if export.Status == domain.ExportPending {
			export.Status, export.StartedAt = domain.ExportRunning, &now
			export.Attempts++
			f.exports[id] = export

			return export, true, nil
		}
	}

	return domain.Export{}, false, nil
}

func (f *fakeExportRepo) Complete(_ context.Context, id uuid.UUID, key string, size int64, expiresAt, at time.Time) error {
	export := f.exports[id]
	export.Status, export.StorageKey, export.SizeBytes = domain.ExportCompleted, &key, &size
	export.ExpiresAt, export.CompletedAt = &expiresAt, &at
	f.exports[id] = export

	return nil
}

func (f *fakeExportRepo) Fail(_ context.Context, id uuid.UUID, message string, final bool, _ time.Time) error {
	export := f.exports[id]
	export.Status, export.LastError = domain.ExportFailed, &message

	if !final {
		// Keep it off the queue for the rest of the run, as the stale window would.
		export.Status = domain.ExportRunning
	}

	f.exports[id] = export
	f.failed = append(f.failed, final)

	return nil
}

func (f *fakeExportRepo) Archives(_ context.Context, before time.Time, limit int) ([]domain.Export, error) {
	var out []domain.Export

	for _, export := range f.exports {
		if export.StorageKey != nil && export.ExpiresAt.Before(before) && len(out) < limit {
			out = append(out, export)
		}
	}

	return out, nil
}

func (f *fakeExportRepo) ClearArchive(_ context.Context, id uuid.UUID) error {
	export := f.exports[id]
	export.StorageKey = nil
	f.exports[id] = export

	return nil
}

type fakeArchives struct {
	objects map[string][]byte
	ttls    []time.Duration
}

func (f *fakeArchives) PutObject(_ context.Context, key, contentType string, body []byte) error {
	if contentType != exportMediaType {
		return errors.New("wrong content type " + contentType)
	}

	f.objects[key] = body

	return nil
}

func (f *fakeArchives) PresignGet(_ context.Context, key string, ttl time.Duration) (string, error) {
	f.ttls = append(f.ttls, ttl)

	return "https://storage.example/" + key, nil
}

func (f *fakeArchives) DeleteObject(_ context.Context, key string) error {
	delete(f.objects, key)

	return nil
}

type fakeExporter struct {
	sel         domain.Selection
	first, last string
	err         error
}

func (f *fakeExporter) ExportRollups(_ context.Context, sel domain.Selection, first, last string, w ports.TableWriter) error {
	f.sel, f.first, f.last = sel, first, last
	if f.err != nil {
		return f.err
	}

	if err := w.Table("pages", []string{"period_start", "path", "views"}); err != nil {
		return err
	}

	if err := w.Row([]string{"2026-08-01", "/a", "3"}); err != nil {
		return err
	}

	if err := w.Table("searches", []string{"period_start", "query", "searches"}); err != nil {
		return err
	}

	return w.Row([]string{"2026-08-01", "=HYPERLINK(\"x\")", "1"})
}

type fakeStepUp struct {
	ok    bool
	calls int
}

func (f *fakeStepUp) VerifyStepUp(context.Context, uuid.UUID, string, string) (bool, error) {
	f.calls++

	return f.ok, nil
}

type exportFixture struct {
	svc      *Exports
	repo     *fakeExportRepo
	archives *fakeArchives
	exporter *fakeExporter
	stepUp   *fakeStepUp
	clock    *fixedClock
	actor    uuid.UUID
}

func newExportFixture(t *testing.T) exportFixture {
	t.Helper()

	f := exportFixture{
		repo: newFakeExportRepo(), archives: &fakeArchives{objects: map[string][]byte{}}, exporter: &fakeExporter{},
		stepUp: &fakeStepUp{ok: true}, clock: &fixedClock{now: time.Date(2026, 9, 25, 3, 0, 0, 0, time.UTC)}, actor: uuid.New(),
	}
	f.svc = NewExports(f.repo, f.exporter, fixedTimezone("Asia/Jakarta"), seqIDs{id: uuid.New()}, f.clock,
		ExportConfig{Retention: 72 * time.Hour, DownloadTTL: 15 * time.Minute}).WithArchives(f.archives).WithStepUp(f.stepUp)

	return f
}

func exportQuery(from, to string, grain domain.Grain) ExportRequest {
	first, _ := time.Parse(time.DateOnly, from)
	last, _ := time.Parse(time.DateOnly, to)

	return ExportRequest{Query: domain.ReportQuery{From: first, To: last, Grain: grain}, Password: "pw"}
}

func TestExportRequestWidensToWholePeriods(t *testing.T) {
	t.Parallel()

	f := newExportFixture(t)

	view, err := f.svc.Request(t.Context(), f.actor, exportQuery("2026-08-10", "2026-09-05", domain.GrainMonth))
	require.NoError(t, err)
	assert.Equal(t, domain.ExportPending, view.Status)
	assert.Equal(t, "2026-08-01", view.FirstDay)
	assert.Equal(t, "2026-09-30", view.LastDay)
	assert.Equal(t, "Asia/Jakarta", view.Timezone)
	assert.Equal(t, f.actor, view.RequestedBy)
	assert.Contains(t, f.repo.exports, view.UUID)
}

func TestExportRequestAllowsTwoYearsOfDays(t *testing.T) {
	t.Parallel()

	f := newExportFixture(t)

	view, err := f.svc.Request(t.Context(), f.actor, exportQuery("2024-09-01", "2026-08-31", domain.GrainDay))
	require.NoError(t, err, "the dashboard's daily cap does not apply to exports")
	assert.Equal(t, "2024-09-01", view.FirstDay)
	assert.Equal(t, "2026-08-31", view.LastDay)
}

func TestExportRequestRefusals(t *testing.T) {
	t.Parallel()

	t.Run("no storage", func(t *testing.T) {
		t.Parallel()

		f := newExportFixture(t)
		f.svc.archives = nil

		_, err := f.svc.Request(t.Context(), f.actor, exportQuery("2026-08-01", "2026-08-31", ""))
		require.ErrorIs(t, err, domain.ErrExportUnavailable)
	})

	t.Run("invalid window skips step-up", func(t *testing.T) {
		t.Parallel()

		f := newExportFixture(t)

		_, err := f.svc.Request(t.Context(), f.actor, exportQuery("2026-08-31", "2026-08-01", ""))
		require.ErrorIs(t, err, domain.ErrValidation)
		assert.Zero(t, f.stepUp.calls)
	})

	t.Run("step-up fails", func(t *testing.T) {
		t.Parallel()

		f := newExportFixture(t)
		f.stepUp.ok = false

		_, err := f.svc.Request(t.Context(), f.actor, exportQuery("2026-08-01", "2026-08-31", ""))
		require.ErrorIs(t, err, domain.ErrStepUpRequired)
		assert.Empty(t, f.repo.exports)
	})

	t.Run("no step-up verifier", func(t *testing.T) {
		t.Parallel()

		f := newExportFixture(t)
		f.svc.stepUp = nil

		_, err := f.svc.Request(t.Context(), f.actor, exportQuery("2026-08-01", "2026-08-31", ""))
		require.ErrorIs(t, err, domain.ErrStepUpRequired)
	})

	t.Run("already open returns the open export", func(t *testing.T) {
		t.Parallel()

		f := newExportFixture(t)
		open := domain.Export{UUID: uuid.New(), RequestedBy: f.actor, Status: domain.ExportRunning}
		f.repo.exports[open.UUID] = open
		f.repo.createErr = domain.ErrExportOpen

		view, err := f.svc.Request(t.Context(), f.actor, exportQuery("2026-08-01", "2026-08-31", ""))
		require.ErrorIs(t, err, domain.ErrExportOpen)
		assert.Equal(t, open.UUID, view.UUID)
	})
}

func TestExportProcessBuildsZipOfCSVs(t *testing.T) {
	t.Parallel()

	f := newExportFixture(t)

	view, err := f.svc.Request(t.Context(), f.actor, exportQuery("2026-08-01", "2026-08-31", ""))
	require.NoError(t, err)

	done, err := f.svc.ProcessPending(t.Context(), 5)
	require.NoError(t, err)
	assert.Equal(t, 1, done)

	assert.Equal(t, domain.Selection{Grain: domain.GrainDay, First: "2026-08-01", Last: "2026-08-31"}, f.exporter.sel)
	assert.Equal(t, [2]string{"2026-08-01", "2026-08-31"}, [2]string{f.exporter.first, f.exporter.last})

	export := f.repo.exports[view.UUID]
	require.Equal(t, domain.ExportCompleted, export.Status)
	require.NotNil(t, export.StorageKey)
	assert.Equal(t, "analytics-exports/"+view.UUID.String()+".zip", *export.StorageKey)
	assert.Equal(t, f.clock.now.Add(72*time.Hour), *export.ExpiresAt)

	body := f.archives.objects[*export.StorageKey]
	assert.Equal(t, int64(len(body)), *export.SizeBytes)

	files := unzip(t, body)
	assert.ElementsMatch(t, []string{"pages.csv", "searches.csv", "manifest.json"}, keys(files))

	searches, err := csv.NewReader(bytes.NewReader(files["searches.csv"])).ReadAll()
	require.NoError(t, err)
	assert.Equal(t, [][]string{{"period_start", "query", "searches"}, {"2026-08-01", `'=HYPERLINK("x")`, "1"}}, searches)

	var manifest map[string]any
	require.NoError(t, json.Unmarshal(files["manifest.json"], &manifest))
	assert.Equal(t, "Asia/Jakarta", manifest["timezone"])
	assert.Equal(t, []any{"pages", "searches"}, manifest["tables"])
}

func TestExportProcessRetriesThenFails(t *testing.T) {
	t.Parallel()

	f := newExportFixture(t)
	f.exporter.err = errors.New("db down")

	view, err := f.svc.Request(t.Context(), f.actor, exportQuery("2026-08-01", "2026-08-31", ""))
	require.NoError(t, err)

	done, err := f.svc.ProcessPending(t.Context(), 1)
	require.ErrorContains(t, err, "db down")
	assert.Zero(t, done)
	assert.Equal(t, []bool{false}, f.repo.failed)

	export := f.repo.exports[view.UUID]
	export.Status, export.Attempts = domain.ExportPending, exportMaxAttempts-1
	f.repo.exports[view.UUID] = export

	_, err = f.svc.ProcessPending(t.Context(), 1)
	require.Error(t, err)
	assert.Equal(t, []bool{false, true}, f.repo.failed)
	assert.Equal(t, domain.ExportFailed, f.repo.exports[view.UUID].Status)
}

func TestExportGetSignsForTheRequesterOnly(t *testing.T) {
	t.Parallel()

	f := newExportFixture(t)
	key := "analytics-exports/a.zip"
	expires := f.clock.now.Add(5 * time.Minute)
	export := domain.Export{UUID: uuid.New(), RequestedBy: f.actor, Status: domain.ExportCompleted, StorageKey: &key, ExpiresAt: &expires}
	f.repo.exports[export.UUID] = export

	view, err := f.svc.Get(t.Context(), f.actor, export.UUID)
	require.NoError(t, err)
	assert.Equal(t, "https://storage.example/"+key, view.DownloadURL)
	assert.Equal(t, []time.Duration{5 * time.Minute}, f.archives.ttls, "never past the archive expiry")
	assert.Equal(t, expires, *view.DownloadExpiresAt)

	_, err = f.svc.Get(t.Context(), uuid.New(), export.UUID)
	require.ErrorIs(t, err, domain.ErrExportNotFound)

	views, err := f.svc.List(t.Context(), f.actor)
	require.NoError(t, err)
	require.Len(t, views, 1)
	assert.NotEmpty(t, views[0].DownloadURL)

	f.clock.now = expires.Add(time.Second)

	view, err = f.svc.Get(t.Context(), f.actor, export.UUID)
	require.NoError(t, err)
	assert.Empty(t, view.DownloadURL)
}

func TestExportPurgeExpiredDeletesArchives(t *testing.T) {
	t.Parallel()

	f := newExportFixture(t)
	oldKey, newKey := "analytics-exports/old.zip", "analytics-exports/new.zip"
	past, future := f.clock.now.Add(-time.Minute), f.clock.now.Add(time.Hour)
	old := domain.Export{UUID: uuid.New(), Status: domain.ExportCompleted, StorageKey: &oldKey, ExpiresAt: &past}
	fresh := domain.Export{UUID: uuid.New(), Status: domain.ExportCompleted, StorageKey: &newKey, ExpiresAt: &future}
	f.repo.exports[old.UUID], f.repo.exports[fresh.UUID] = old, fresh
	f.archives.objects[oldKey], f.archives.objects[newKey] = []byte("x"), []byte("y")

	purged, err := f.svc.PurgeExpired(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 1, purged)
	assert.NotContains(t, f.archives.objects, oldKey)
	assert.Contains(t, f.archives.objects, newKey)
	assert.Nil(t, f.repo.exports[old.UUID].StorageKey)
}

func TestCSVSafe(t *testing.T) {
	t.Parallel()

	for in, want := range map[string]string{
		"=1+1": "'=1+1", "+a": "'+a", "-a": "'-a", "@a": "'@a", "\tx": "'\tx", "\rx": "'\rx",
		"/path": "/path", "": "", "12": "12", "a=b": "a=b",
	} {
		assert.Equal(t, want, csvSafe(in), in)
	}
}

func unzip(t *testing.T, body []byte) map[string][]byte {
	t.Helper()

	reader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	require.NoError(t, err)

	files := map[string][]byte{}

	for _, file := range reader.File {
		rc, err := file.Open()
		require.NoError(t, err)

		data, err := io.ReadAll(rc)
		require.NoError(t, err)
		require.NoError(t, rc.Close())

		files[file.Name] = data
	}

	return files
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}

	return out
}
