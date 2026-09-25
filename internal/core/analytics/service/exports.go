package service

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/core/analytics/domain"
	"github.com/turahe/blog-api/internal/core/analytics/ports"
	"github.com/turahe/blog-api/internal/core/audit"
)

const (
	// exportStaleAfter is how long a running export may go without finishing before another
	// run claims it again.
	exportStaleAfter = 30 * time.Minute
	// exportMaxAttempts is how many times an export is tried before it is marked failed.
	exportMaxAttempts = 3
	// ExportListLimit is how many recent exports List returns.
	ExportListLimit = 20
	// purgeBatch bounds how many archives one purge pass deletes.
	purgeBatch = 100
	// maxErrorLength bounds the stored failure message.
	maxErrorLength  = 500
	exportKeyPrefix = "analytics-exports/"
	// ResourceExport is the audit resource type of an export.
	ResourceExport  = "analytics_export"
	exportMediaType = "application/zip"
)

// ExportConfig tunes export archives.
type ExportConfig struct {
	// Retention is how long an archive is kept after it is built.
	Retention time.Duration
	// DownloadTTL is the longest a presigned download link stays valid.
	DownloadTTL time.Duration
}

// Exports queues rollup exports and builds them into ZIP archives of CSV files.
type Exports struct {
	repo     ports.ExportRepository
	exporter ports.RollupExporter
	archives ports.ArchiveStore
	stepUp   ports.StepUp
	tz       ports.TimezoneSource
	ids      IDGenerator
	clock    Clock
	cfg      ExportConfig
}

// NewExports returns Exports without object storage; requests then fail with
// domain.ErrExportUnavailable.
func NewExports(
	repo ports.ExportRepository, exporter ports.RollupExporter, tz ports.TimezoneSource, ids IDGenerator, clock Clock, cfg ExportConfig,
) *Exports {
	return &Exports{repo: repo, exporter: exporter, tz: tz, ids: ids, clock: clock, cfg: cfg}
}

// WithArchives stores archives in archives.
func (s *Exports) WithArchives(archives ports.ArchiveStore) *Exports {
	s.archives = archives
	return s
}

// WithStepUp verifies requests with stepUp; without it every request is refused.
func (s *Exports) WithStepUp(stepUp ports.StepUp) *Exports {
	s.stepUp = stepUp
	return s
}

// ExportRequest asks for the rollups of the window Query selects. Password and Code re-verify
// the requester.
type ExportRequest struct {
	Query    domain.ReportQuery
	Password string
	Code     string
}

// ExportView is an export with a presigned download link while its archive exists.
type ExportView struct {
	domain.Export

	DownloadURL       string
	DownloadExpiresAt *time.Time
}

// Request queues an export for actor. The window is widened to whole periods like the reports.
// When actor already has an open export, it returns that export and domain.ErrExportOpen.
func (s *Exports) Request(ctx context.Context, actor uuid.UUID, in ExportRequest) (ExportView, error) {
	if s.archives == nil {
		return ExportView{}, domain.ErrExportUnavailable
	}

	header, err := resolveHeader(ctx, s.tz, s.clock, in.Query)
	if err != nil {
		audit.AddMetadata(ctx, "failure_reason", "validation")
		return ExportView{}, err
	}

	audit.AddMetadata(ctx, "grain", string(header.Window.Grain))
	audit.AddMetadata(ctx, "first_day", header.Window.FirstDay())
	audit.AddMetadata(ctx, "last_day", header.Window.LastDay())

	if err := s.verify(ctx, actor, in); err != nil {
		return ExportView{}, err
	}

	export := domain.Export{
		UUID: s.ids.New(), RequestedBy: actor, Grain: header.Window.Grain, Timezone: header.Timezone,
		FirstDay: header.Window.FirstDay(), LastDay: header.Window.LastDay(),
		Status: domain.ExportPending, CreatedAt: s.clock.Now(),
	}

	if err := s.repo.Create(ctx, export); err != nil {
		if !errors.Is(err, domain.ErrExportOpen) {
			return ExportView{}, err
		}

		open, openErr := s.repo.Open(ctx, actor)
		if openErr != nil {
			return ExportView{}, errors.Join(err, openErr)
		}

		audit.AddMetadata(ctx, "failure_reason", "export_in_progress")

		return ExportView{Export: open}, err
	}

	audit.SetResource(ctx, ResourceExport, export.UUID)

	return ExportView{Export: export}, nil
}

func (s *Exports) verify(ctx context.Context, actor uuid.UUID, in ExportRequest) error {
	ok := false

	if s.stepUp != nil {
		var err error
		if ok, err = s.stepUp.VerifyStepUp(ctx, actor, in.Password, in.Code); err != nil {
			return err
		}
	}

	if !ok {
		audit.AddMetadata(ctx, "failure_reason", "step_up_required")
		return domain.ErrStepUpRequired
	}

	return nil
}

// Get returns actor's export with a fresh download link; other users' exports are
// domain.ErrExportNotFound.
func (s *Exports) Get(ctx context.Context, actor, id uuid.UUID) (ExportView, error) {
	export, err := s.repo.Get(ctx, id, actor)
	if err != nil {
		return ExportView{}, err
	}

	return s.view(ctx, export)
}

// List returns actor's ExportListLimit most recent exports, newest first.
func (s *Exports) List(ctx context.Context, actor uuid.UUID) ([]ExportView, error) {
	exports, err := s.repo.List(ctx, actor, ExportListLimit)
	if err != nil {
		return nil, err
	}

	views := make([]ExportView, 0, len(exports))

	for _, export := range exports {
		view, err := s.view(ctx, export)
		if err != nil {
			return nil, err
		}

		views = append(views, view)
	}

	return views, nil
}

// ProcessPending builds up to limit queued exports, returning how many completed. A failed
// export is retried on a later run until exportMaxAttempts.
func (s *Exports) ProcessPending(ctx context.Context, limit int) (int, error) {
	var (
		done int
		errs []error
	)

	for range limit {
		now := s.clock.Now()

		export, ok, err := s.repo.ClaimNext(ctx, now, now.Add(-exportStaleAfter))
		if err != nil || !ok {
			return done, errors.Join(append(errs, err)...)
		}

		if err := s.build(ctx, export); err != nil {
			final := export.Attempts >= exportMaxAttempts
			failErr := s.repo.Fail(ctx, export.UUID, truncateError(err.Error()), final, s.clock.Now())
			errs = append(errs, fmt.Errorf("analytics export %s: %w", export.UUID, err), failErr)

			continue
		}

		done++
	}

	return done, errors.Join(errs...)
}

// PurgeExpired deletes archives past their retention, returning how many were removed.
func (s *Exports) PurgeExpired(ctx context.Context) (int, error) {
	if s.archives == nil {
		return 0, nil
	}

	var purged int

	for {
		exports, err := s.repo.Archives(ctx, s.clock.Now(), purgeBatch)
		if err != nil || len(exports) == 0 {
			return purged, err
		}

		for _, export := range exports {
			if err := s.archives.DeleteObject(ctx, *export.StorageKey); err != nil {
				return purged, err
			}

			if err := s.repo.ClearArchive(ctx, export.UUID); err != nil {
				return purged, err
			}

			purged++
		}

		if len(exports) < purgeBatch {
			return purged, nil
		}
	}
}

func (s *Exports) view(ctx context.Context, export domain.Export) (ExportView, error) {
	view := ExportView{Export: export}
	now := s.clock.Now()

	if s.archives == nil || !export.Downloadable(now) {
		return view, nil
	}

	ttl := min(s.cfg.DownloadTTL, export.ExpiresAt.Sub(now))

	url, err := s.archives.PresignGet(ctx, *export.StorageKey, ttl)
	if err != nil {
		return ExportView{}, err
	}

	expires := now.Add(ttl)
	view.DownloadURL, view.DownloadExpiresAt = url, &expires

	return view, nil
}

func (s *Exports) build(ctx context.Context, export domain.Export) error {
	if s.archives == nil {
		return domain.ErrExportUnavailable
	}

	window, err := export.Window()
	if err != nil {
		return err
	}

	var buf bytes.Buffer

	archive := zip.NewWriter(&buf)
	tables := &zipTables{zip: archive}

	if err := s.exporter.ExportRollups(ctx, window.Selection(), window.FirstDay(), window.LastDay(), tables); err != nil {
		return err
	}

	if err := tables.flush(); err != nil {
		return err
	}

	if err := writeManifest(archive, export, tables.names, s.clock.Now()); err != nil {
		return err
	}

	if err := archive.Close(); err != nil {
		return err
	}

	key := exportKeyPrefix + export.UUID.String() + ".zip"
	if err := s.archives.PutObject(ctx, key, exportMediaType, buf.Bytes()); err != nil {
		return err
	}

	now := s.clock.Now()

	return s.repo.Complete(ctx, export.UUID, key, int64(buf.Len()), now.Add(s.cfg.Retention), now)
}

// exportManifest describes an archive in manifest.json.
type exportManifest struct {
	ExportID    uuid.UUID `json:"export_id"`
	Timezone    string    `json:"timezone"`
	Grain       string    `json:"grain"`
	FirstDay    string    `json:"first_day"`
	LastDay     string    `json:"last_day"`
	GeneratedAt time.Time `json:"generated_at"`
	Tables      []string  `json:"tables"`
	Notes       []string  `json:"notes"`
}

func writeManifest(archive *zip.Writer, export domain.Export, tables []string, now time.Time) error {
	body, err := json.MarshalIndent(exportManifest{
		ExportID: export.UUID, Timezone: export.Timezone, Grain: string(export.Grain),
		FirstDay: export.FirstDay, LastDay: export.LastDay, GeneratedAt: now.UTC(), Tables: tables,
		Notes: []string{
			"Rows are per period (period_start is a local date in timezone); visitors are distinct within one period only.",
			`"` + domain.Other + `" rows fold the values beyond the top ` + strconv.Itoa(domain.RollupTopN) + " of a period.",
			"cohorts.csv lists the daily cohorts of first_day through last_day; empty returns are not final yet.",
			"Text values starting with =, +, -, @, tab, or carriage return are prefixed with ' so spreadsheets do not run them.",
		},
	}, "", "  ")
	if err != nil {
		return err
	}

	w, err := archive.Create("manifest.json")
	if err != nil {
		return err
	}

	_, err = w.Write(body)

	return err
}

// zipTables writes each table as a CSV file of a ZIP archive.
type zipTables struct {
	zip   *zip.Writer
	csv   *csv.Writer
	names []string
}

func (z *zipTables) Table(name string, columns []string) error {
	if err := z.flush(); err != nil {
		return err
	}

	w, err := z.zip.Create(name + ".csv")
	if err != nil {
		return err
	}

	z.csv, z.names = csv.NewWriter(w), append(z.names, name)

	return z.csv.Write(columns)
}

func (z *zipTables) Row(values []string) error {
	if z.csv == nil {
		return errors.New("analytics export: row before table")
	}

	safe := make([]string, len(values))
	for i, v := range values {
		safe[i] = csvSafe(v)
	}

	return z.csv.Write(safe)
}

func (z *zipTables) flush() error {
	if z.csv == nil {
		return nil
	}

	z.csv.Flush()

	return z.csv.Error()
}

// csvSafe defuses spreadsheet formulas: paths, hosts, and search queries come from visitors.
func csvSafe(v string) string {
	if v != "" && strings.ContainsRune("=+-@\t\r", rune(v[0])) {
		return "'" + v
	}

	return v
}

func truncateError(message string) string {
	if len(message) <= maxErrorLength {
		return message
	}

	return strings.ToValidUTF8(message[:maxErrorLength], "")
}
