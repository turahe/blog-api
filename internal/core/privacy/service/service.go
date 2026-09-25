// Package service runs personal data exports and erasures: requests are queued by the API
// and processed asynchronously by app scheduler.
package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/core/audit"
	"github.com/turahe/blog-api/internal/core/event"
	"github.com/turahe/blog-api/internal/core/privacy/domain"
	"github.com/turahe/blog-api/internal/core/privacy/ports"
	"github.com/turahe/blog-api/internal/core/readcache"
)

const (
	// maxAttempts is how many times a request is tried before it is marked failed.
	maxAttempts = 3
	// staleAfter reclaims a running request whose processor died.
	staleAfter = 15 * time.Minute
	// archivePrefix is where export archives live in the bucket; it must not be public.
	archivePrefix = "privacy-exports/"
	// purgeBatch bounds how many expired archives one purge deletes.
	purgeBatch = 100
	// maxErrorLen bounds the error text stored on a request.
	maxErrorLen = 500
)

// Clock returns the current time.
type Clock interface {
	Now() time.Time
}

// IDGenerator returns new UUIDs.
type IDGenerator interface {
	New() uuid.UUID
}

// Config sets archive lifetimes.
type Config struct {
	// ExportRetention is how long an export archive is kept.
	ExportRetention time.Duration
	// DownloadTTL is the lifetime of each presigned download link.
	DownloadTTL time.Duration
}

// Service queues and processes export and erasure requests.
type Service struct {
	repo     ports.Repository
	data     ports.DataSource
	eraser   ports.Eraser
	verifier ports.PasswordVerifier
	ids      IDGenerator
	clock    Clock
	cfg      Config
	archives ports.ArchiveStore
	events   event.Unit
	cache    readcache.Cache
	modules  []ports.Eraser
}

// New returns a Service; exports stay unavailable until WithArchives.
func New(
	repo ports.Repository, data ports.DataSource, eraser ports.Eraser, verifier ports.PasswordVerifier,
	ids IDGenerator, clock Clock, cfg Config,
) *Service {
	return &Service{repo: repo, data: data, eraser: eraser, verifier: verifier, ids: ids, clock: clock, cfg: cfg}
}

// WithArchives stores export archives in store.
func (s *Service) WithArchives(store ports.ArchiveStore) *Service {
	s.archives = store
	return s
}

// WithEvents records request events in the outbox.
func (s *Service) WithEvents(events event.Unit) *Service {
	s.events = events
	return s
}

// WithModuleErasers also erases the user's data held by other modules (newsletter
// subscriptions), in the same transaction as the account.
func (s *Service) WithModuleErasers(erasers ...ports.Eraser) *Service {
	s.modules = append(s.modules, erasers...)
	return s
}

// WithCache invalidates cached user and post reads after an erasure.
func (s *Service) WithCache(cache readcache.Cache) *Service {
	s.cache = cache
	return s
}

// Export is an export request with a download link once its archive is ready.
type Export struct {
	Request           domain.Request
	DownloadURL       string
	DownloadExpiresAt *time.Time
}

// RequestExport returns the user's open or still downloadable export, or queues a new one;
// created reports whether a request was queued.
func (s *Service) RequestExport(ctx context.Context, userID uuid.UUID) (export Export, created bool, err error) {
	if s.archives == nil {
		return Export{}, false, domain.ErrExportUnavailable
	}

	now := s.clock.Now()

	latest, err := s.latest(ctx, userID, domain.KindExport)
	if err != nil {
		return Export{}, false, err
	}

	switch {
	case latest != nil && latest.Open():
		return Export{Request: *latest}, false, nil
	case latest != nil && latest.Downloadable(now):
		export, err := s.download(ctx, *latest, now)
		return export, false, err
	}

	request, created, err := s.create(ctx, userID, domain.KindExport, now)

	return Export{Request: request}, created, err
}

// RequestErase verifies the password and queues the anonymization of the user, returning the
// open request instead when one exists; created reports whether a request was queued.
func (s *Service) RequestErase(ctx context.Context, userID uuid.UUID, password string) (domain.Request, bool, error) {
	if err := s.verify(ctx, userID, password); err != nil {
		return domain.Request{}, false, err
	}

	latest, err := s.latest(ctx, userID, domain.KindErase)
	if err != nil {
		return domain.Request{}, false, err
	}

	if latest != nil && latest.Open() {
		return *latest, false, nil
	}

	return s.create(ctx, userID, domain.KindErase, s.clock.Now())
}

// ProcessPending processes up to limit queued requests, returning how many completed. A failed
// request is retried on a later run until maxAttempts.
func (s *Service) ProcessPending(ctx context.Context, limit int) (int, error) {
	var (
		done int
		errs []error
	)

	for range limit {
		now := s.clock.Now()

		request, ok, err := s.repo.ClaimNext(ctx, now, now.Add(-staleAfter))
		if err != nil || !ok {
			return done, errors.Join(append(errs, err)...)
		}

		if err := s.process(ctx, request); err != nil {
			final := request.Attempts >= maxAttempts
			failErr := s.repo.Fail(ctx, request.UUID, truncate(err.Error()), final, s.clock.Now())
			errs = append(errs, fmt.Errorf("privacy request %s: %w", request.UUID, err), failErr)

			continue
		}

		done++
	}

	return done, errors.Join(errs...)
}

// PurgeExpired deletes export archives past their retention, returning how many were removed.
func (s *Service) PurgeExpired(ctx context.Context) (int, error) {
	if s.archives == nil {
		return 0, nil
	}

	return s.purgeArchives(ctx, nil, s.clock.Now())
}

func (s *Service) process(ctx context.Context, request domain.Request) error {
	switch request.Kind {
	case domain.KindExport:
		return s.export(ctx, request)
	case domain.KindErase:
		return s.erase(ctx, request)
	default:
		return fmt.Errorf("unknown privacy request kind %q", request.Kind)
	}
}

func (s *Service) export(ctx context.Context, request domain.Request) error {
	if s.archives == nil {
		return domain.ErrExportUnavailable
	}

	now := s.clock.Now()

	data, err := s.data.ExportUser(ctx, request.UserUUID, now)
	if err != nil {
		return err
	}

	key, err := archiveKey(request)
	if err != nil {
		return err
	}

	if err := s.archives.PutObject(ctx, key, "application/json", data); err != nil {
		return fmt.Errorf("store export archive: %w", err)
	}

	expires := now.Add(s.cfg.ExportRetention)

	return s.repo.Complete(ctx, request.UUID, &key, &expires, now)
}

// erase deletes the user's export archives, then anonymizes the account. Both steps are
// idempotent, so a retry after a partial failure is safe.
func (s *Service) erase(ctx context.Context, request domain.Request) error {
	if s.archives != nil {
		if _, err := s.purgeArchives(ctx, &request.UserUUID, time.Time{}); err != nil {
			return err
		}
	}

	now := s.clock.Now()

	err := s.events.InTx(ctx, func(ctx context.Context) error {
		for _, eraser := range append(slices.Clone(s.modules), s.eraser) {
			if err := eraser.EraseUser(ctx, request.UserUUID, now); err != nil {
				return err
			}
		}

		return s.repo.Complete(ctx, request.UUID, nil, nil, now)
	})
	if err != nil {
		return err
	}

	readcache.Invalidate(ctx, s.cache, readcache.Users, readcache.Posts)

	return nil
}

func (s *Service) purgeArchives(ctx context.Context, userID *uuid.UUID, before time.Time) (int, error) {
	requests, err := s.repo.Archives(ctx, userID, before, purgeBatch)
	if err != nil {
		return 0, err
	}

	for i, request := range requests {
		if err := s.archives.DeleteObject(ctx, *request.StorageKey); err != nil {
			return i, fmt.Errorf("delete export archive: %w", err)
		}

		if err := s.repo.ClearArchive(ctx, request.UUID); err != nil {
			return i, err
		}
	}

	return len(requests), nil
}

func (s *Service) download(ctx context.Context, request domain.Request, now time.Time) (Export, error) {
	ttl := min(s.cfg.DownloadTTL, request.ExpiresAt.Sub(now))

	url, err := s.archives.PresignGet(ctx, *request.StorageKey, ttl)
	if err != nil {
		return Export{}, fmt.Errorf("presign export archive: %w", err)
	}

	expires := now.Add(ttl)

	return Export{Request: request, DownloadURL: url, DownloadExpiresAt: &expires}, nil
}

func (s *Service) create(ctx context.Context, userID uuid.UUID, kind domain.Kind, now time.Time) (domain.Request, bool, error) {
	request := domain.Request{UUID: s.ids.New(), UserUUID: userID, Kind: kind, Status: domain.StatusPending, CreatedAt: now}

	err := s.events.InTx(ctx, func(ctx context.Context) error {
		if err := s.repo.Create(ctx, request); err != nil {
			return err
		}

		return s.events.Record(ctx, requestedEvent(request))
	})
	if errors.Is(err, domain.ErrAlreadyOpen) {
		// A concurrent call queued one first.
		latest, err := s.repo.Latest(ctx, userID, kind)
		return latest, false, err
	}

	if err != nil {
		return domain.Request{}, false, err
	}

	audit.AddMetadata(ctx, "privacy_request_id", request.UUID.String())

	return request, true, nil
}

func (s *Service) latest(ctx context.Context, userID uuid.UUID, kind domain.Kind) (*domain.Request, error) {
	request, err := s.repo.Latest(ctx, userID, kind)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	return &request, nil
}

func (s *Service) verify(ctx context.Context, userID uuid.UUID, password string) error {
	if s.verifier == nil || password == "" {
		return domain.ErrReauth
	}

	ok, err := s.verifier.VerifyPassword(ctx, userID, password)
	if err != nil {
		return err
	}

	if !ok {
		return domain.ErrReauth
	}

	return nil
}

// archiveKey is unguessable so a leaked listing of user ids does not reveal archive paths.
func archiveKey(request domain.Request) (string, error) {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("archive key: %w", err)
	}

	return archivePrefix + request.UserUUID.String() + "/" + request.UUID.String() + "-" + hex.EncodeToString(random) + ".json", nil
}

// scopeFullProfile is the only scope offered: exports and erasures cover the whole account.
const scopeFullProfile = "full_profile"

type exportRequestedPayload struct {
	JobID            uuid.UUID `json:"job_id"`
	Scope            string    `json:"scope"`
	RequestedByAdmin bool      `json:"requested_by_admin"`
}

type erasureRequestedPayload struct {
	ErasureID uuid.UUID `json:"erasure_id"`
	Scope     string    `json:"scope"`
}

func requestedEvent(request domain.Request) event.Event {
	actor := request.UserUUID
	if request.Kind == domain.KindErase {
		return event.New(event.UserErasureRequested, event.AggregateUser, request.UserUUID, &actor, request.CreatedAt,
			erasureRequestedPayload{ErasureID: request.UUID, Scope: scopeFullProfile})
	}

	return event.New(event.UserExportRequested, event.AggregateUser, request.UserUUID, &actor, request.CreatedAt,
		exportRequestedPayload{JobID: request.UUID, Scope: scopeFullProfile})
}

func truncate(message string) string {
	if len(message) <= maxErrorLen {
		return message
	}

	return message[:maxErrorLen]
}
