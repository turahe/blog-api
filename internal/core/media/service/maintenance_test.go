package service

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	mediadomain "github.com/turahe/blog-api/internal/core/media/domain"
)

func (r *fakeRepo) Abandoned(_ context.Context, before time.Time, limit int) ([]mediadomain.MediaAsset, error) {
	return r.matching(limit, func(a mediadomain.MediaAsset) bool {
		return a.DeletedAt == nil && a.Status != mediadomain.StatusReady && a.PresignExpiresAt != nil && a.PresignExpiresAt.Before(before)
	}), nil
}

func (r *fakeRepo) Trashed(_ context.Context, before time.Time, limit int) ([]mediadomain.MediaAsset, error) {
	return r.matching(limit, func(a mediadomain.MediaAsset) bool {
		return a.DeletedAt != nil && a.DeletedAt.Before(before)
	}), nil
}

func (r *fakeRepo) matching(limit int, keep func(mediadomain.MediaAsset) bool) []mediadomain.MediaAsset {
	var out []mediadomain.MediaAsset

	for _, a := range r.assets {
		if keep(a) && len(out) < limit {
			out = append(out, cloneAsset(a))
		}
	}

	return out
}

func (r *fakeRepo) Purge(_ context.Context, id uuid.UUID) (bool, error) {
	_, ok := r.assets[id]
	delete(r.assets, id)

	return ok, nil
}

func (r *fakeRepo) Usage(_ context.Context, filter mediadomain.UsageFilter) (mediadomain.Usage, error) {
	r.usageFilter = filter
	return mediadomain.Usage{}, nil
}

func (s *fakeObjectStorage) DeleteObject(_ context.Context, key string) error {
	if s.deleteErr != nil {
		return s.deleteErr
	}

	s.deleted = append(s.deleted, key)

	return nil
}

func TestPurgeOrphansRemovesAbandonedAndOldTrash(t *testing.T) {
	t.Parallel()

	svc, repo, storage := newTestService()
	now := baseTime()
	longAgo, recently := now.Add(-48*time.Hour), now.Add(-time.Hour)
	oldTrash, newTrash := now.Add(-40*24*time.Hour), now.Add(-2*24*time.Hour)

	add := func(status string, presignExpiry, deletedAt *time.Time, size int64) uuid.UUID {
		id := uuid.New()
		repo.assets[id] = mediadomain.MediaAsset{
			UUID: id, StorageKey: "media/" + id.String(), Status: status, SizeBytes: size,
			PresignExpiresAt: presignExpiry, DeletedAt: deletedAt,
		}

		return id
	}

	abandoned := add(mediadomain.StatusPending, &longAgo, nil, 0)
	stillUploading := add(mediadomain.StatusPending, &recently, nil, 0)
	ready := add(mediadomain.StatusReady, &longAgo, nil, 100)
	trashed := add(mediadomain.StatusReady, &longAgo, &oldTrash, 300)
	justTrashed := add(mediadomain.StatusReady, &longAgo, &newTrash, 50)

	result, err := svc.PurgeOrphans(context.Background(), PurgePolicy{TrashRetention: 30 * 24 * time.Hour, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}

	if result.Abandoned != 1 || result.Trashed != 1 || result.Bytes != 300 {
		t.Fatalf("result = %+v", result)
	}

	for _, id := range []uuid.UUID{abandoned, trashed} {
		if _, ok := repo.assets[id]; ok || !slices.Contains(storage.deleted, "media/"+id.String()) {
			t.Fatalf("asset %s not purged", id)
		}
	}

	for _, id := range []uuid.UUID{stillUploading, ready, justTrashed} {
		if _, ok := repo.assets[id]; !ok {
			t.Fatalf("asset %s purged too early", id)
		}
	}
}

func TestPurgeOrphansKeepsRowWhenObjectDeleteFails(t *testing.T) {
	t.Parallel()

	svc, repo, storage := newTestService()
	expired := baseTime().Add(-48 * time.Hour)
	id := uuid.New()
	repo.assets[id] = mediadomain.MediaAsset{UUID: id, StorageKey: "k", Status: mediadomain.StatusPending, PresignExpiresAt: &expired}
	storage.deleteErr = errors.New("bucket down")

	result, err := svc.PurgeOrphans(context.Background(), PurgePolicy{Limit: 10})
	if !errors.Is(err, ErrStorage) || result.Abandoned != 0 {
		t.Fatalf("result %+v, err %v", result, err)
	}

	if _, ok := repo.assets[id]; !ok {
		t.Fatal("row removed although its object was not")
	}
}

func TestPurgeOrphansZeroRetentionKeepsTrash(t *testing.T) {
	t.Parallel()

	svc, repo, _ := newTestService()
	old := baseTime().Add(-365 * 24 * time.Hour)
	id := uuid.New()
	repo.assets[id] = mediadomain.MediaAsset{UUID: id, StorageKey: "k", Status: mediadomain.StatusReady, DeletedAt: &old}

	if _, err := svc.PurgeOrphans(context.Background(), PurgePolicy{Limit: 10}); err != nil {
		t.Fatal(err)
	}

	if _, ok := repo.assets[id]; !ok {
		t.Fatal("trashed asset purged with zero retention")
	}
}

func TestUsageClampsTopLimit(t *testing.T) {
	t.Parallel()

	svc, repo, _ := newTestService()

	for in, want := range map[int]int{0: 10, 5: 5, 1000: 100} {
		if _, err := svc.Usage(context.Background(), mediadomain.UsageFilter{TopLimit: in}); err != nil {
			t.Fatal(err)
		}

		if repo.usageFilter.TopLimit != want {
			t.Fatalf("top %d: got %d, want %d", in, repo.usageFilter.TopLimit, want)
		}
	}
}

type fakePolicy struct {
	policy mediadomain.TransformPolicy
}

func (f fakePolicy) TransformPolicy(context.Context) (mediadomain.TransformPolicy, error) {
	return f.policy, nil
}

func TestVariantsAndPolicyDefaults(t *testing.T) {
	t.Parallel()

	svc, repo, _ := newTestService()
	ready := seedAsset(repo, "image/png", mediadomain.StatusReady)
	svg := seedAsset(repo, "image/svg+xml", mediadomain.StatusReady)

	if got, err := svc.Variants(context.Background(), repo.assets[ready]); got != nil || err != nil {
		t.Fatalf("variants without transforms = %v, %v", got, err)
	}

	transformer := &fakeTransformer{}
	svc.WithTransforms(transformer, []int{256}).WithPolicy(fakePolicy{mediadomain.TransformPolicy{
		Quality: 70, Format: mediadomain.FormatWebP,
		Variants: []mediadomain.Variant{{Name: "thumb", Width: 100}, {Name: "hero", Width: 1200, Format: mediadomain.FormatAVIF}},
	}})

	got, err := svc.Variants(context.Background(), repo.assets[ready], repo.assets[svg])
	if err != nil {
		t.Fatal(err)
	}

	if len(got[ready]) != 2 || len(got[svg]) != 0 || got[svg] == nil {
		t.Fatalf("variants = %v", got)
	}

	want := []mediadomain.Transform{
		{Width: 100, Format: mediadomain.FormatWebP, Quality: 70},
		{Width: 1200, Format: mediadomain.FormatAVIF, Quality: 70},
	}
	if !slices.Equal(transformer.got, want) {
		t.Fatalf("transforms = %v", transformer.got)
	}

	if _, err := svc.TransformURL(context.Background(), ready, mediadomain.Transform{Width: 256}); err != nil {
		t.Fatal(err)
	}

	if last := transformer.got[len(transformer.got)-1]; last != (mediadomain.Transform{Width: 256, Format: mediadomain.FormatWebP, Quality: 70}) {
		t.Fatalf("TransformURL ignored the policy: %+v", last)
	}
}
