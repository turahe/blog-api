package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	mediadomain "github.com/turahe/blog-api/internal/core/media/domain"
)

// Usage report bounds.
const (
	defaultTopUploaders = 10
	maxTopUploaders     = 100
)

// AbandonedGrace is how long after its presign expired a never-completed upload is kept.
const AbandonedGrace = 24 * time.Hour

// PurgePolicy bounds one orphan cleanup run.
type PurgePolicy struct {
	// TrashRetention is how long soft-deleted assets keep their objects; zero keeps them.
	TrashRetention time.Duration
	// Limit bounds the assets of each kind removed per run.
	Limit int
}

// PurgeOrphans deletes the objects and rows of abandoned uploads and of assets soft-deleted
// longer than the retention. The object goes first: a row whose object is already gone is
// harmless and retried next run, while an object without a row could never be found again.
func (s *Service) PurgeOrphans(ctx context.Context, policy PurgePolicy) (mediadomain.PurgeResult, error) {
	var result mediadomain.PurgeResult

	if policy.Limit < 1 {
		return result, nil
	}

	now := s.clock.Now()

	abandoned, err := s.repo.Abandoned(ctx, now.Add(-AbandonedGrace), policy.Limit)
	if err != nil {
		return result, err
	}

	var errs []error

	for _, asset := range abandoned {
		purged, err := s.purge(ctx, asset)
		errs = append(errs, err)

		if purged {
			result.Abandoned++
			result.Bytes += asset.SizeBytes
		}
	}

	if policy.TrashRetention > 0 {
		trashed, err := s.repo.Trashed(ctx, now.Add(-policy.TrashRetention), policy.Limit)
		if err != nil {
			return result, errors.Join(append(errs, err)...)
		}

		for _, asset := range trashed {
			purged, err := s.purge(ctx, asset)
			errs = append(errs, err)

			if purged {
				result.Trashed++
				result.Bytes += asset.SizeBytes
			}
		}
	}

	return result, errors.Join(errs...)
}

func (s *Service) purge(ctx context.Context, asset mediadomain.MediaAsset) (bool, error) {
	if err := s.storage.DeleteObject(ctx, asset.StorageKey); err != nil {
		return false, fmt.Errorf("%w: delete object of %s: %w", ErrStorage, asset.UUID, err)
	}

	return s.repo.Purge(ctx, asset.UUID)
}

// Usage reports stored assets and bytes by status, content type, and uploader.
func (s *Service) Usage(ctx context.Context, filter mediadomain.UsageFilter) (mediadomain.Usage, error) {
	if filter.TopLimit < 1 {
		filter.TopLimit = defaultTopUploaders
	}

	filter.TopLimit = min(filter.TopLimit, maxTopUploaders)

	return s.repo.Usage(ctx, filter)
}
