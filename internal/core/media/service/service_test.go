package service

import (
	"context"
	"errors"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	mediadomain "github.com/turahe/blog-api/internal/core/media/domain"
	"github.com/turahe/blog-api/internal/core/media/ports"
)

func TestPresignUploadRejectsBadFilename(t *testing.T) {
	t.Parallel()

	cases := []string{"", "   ", "../avatar.png", "nested/path.png"}
	for _, filename := range cases {
		t.Run(filename, func(t *testing.T) {
			t.Parallel()

			svc, _, _ := newTestService()

			_, err := svc.PresignUpload(context.Background(), nil, filename, "image/png", 128, nil)
			if !errors.Is(err, ErrValidation) {
				t.Fatalf("expected ErrValidation, got %v", err)
			}
		})
	}
}

func TestPresignUploadRejectsDisallowedMIME(t *testing.T) {
	t.Parallel()

	svc, _, _ := newTestService()

	_, err := svc.PresignUpload(context.Background(), nil, "cover.png", "application/pdf", 128, nil)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

func TestPresignUploadRejectsSizeAboveMax(t *testing.T) {
	t.Parallel()

	svc, _, _ := newTestService()

	_, err := svc.PresignUpload(context.Background(), nil, "cover.png", "image/png", 2049, nil)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

func TestPresignUploadHappyPath(t *testing.T) {
	t.Parallel()

	svc, repo, storage := newTestService()
	uploader := uuid.MustParse("11111111-1111-1111-1111-111111111111")

	result, err := svc.PresignUpload(
		context.Background(),
		&uploader,
		"cover.png",
		"image/png",
		512,
		[]string{"cover"},
	)
	if err != nil {
		t.Fatalf("PresignUpload returned error: %v", err)
	}

	if result.Asset.Status != mediadomain.StatusPending {
		t.Fatalf("expected pending status, got %q", result.Asset.Status)
	}

	if !strings.HasPrefix(result.Asset.StorageKey, "media/") {
		t.Fatalf("expected media/ prefix, got %q", result.Asset.StorageKey)
	}

	if result.UploadURL != storage.presignURL {
		t.Fatalf("expected upload URL %q, got %q", storage.presignURL, result.UploadURL)
	}

	if got := repo.assets[result.Asset.UUID]; got.UUID != result.Asset.UUID {
		t.Fatalf("expected asset to be stored in repo")
	}

	if storage.lastPresignKey != result.Asset.StorageKey {
		t.Fatalf("expected presign key %q, got %q", result.Asset.StorageKey, storage.lastPresignKey)
	}
}

func TestCompleteUploadMissingObject(t *testing.T) {
	t.Parallel()

	svc, repo, storage := newTestService()
	asset := repo.store(makePendingAsset())
	storage.headErr = ports.ErrObjectNotFound

	_, err := svc.CompleteUpload(context.Background(), asset.UUID)
	if !errors.Is(err, ErrUploadIncomplete) {
		t.Fatalf("expected ErrUploadIncomplete, got %v", err)
	}
}

func TestCompleteUploadMarksReadyAfterHeadOK(t *testing.T) {
	t.Parallel()

	svc, repo, storage := newTestService()
	asset := repo.store(makePendingAsset())
	storage.headInfo[asset.StorageKey] = ports.ObjectInfo{
		Size:        1024,
		ContentType: "image/webp",
		ETag:        "etag-1",
	}
	storage.objects[asset.StorageKey] = mustBase64(t, tinyWebP)

	got, err := svc.CompleteUpload(context.Background(), asset.UUID)
	if err != nil {
		t.Fatalf("CompleteUpload returned error: %v", err)
	}

	if got.Status != mediadomain.StatusReady {
		t.Fatalf("expected ready status, got %q", got.Status)
	}

	if got.SizeBytes != 1024 {
		t.Fatalf("expected size 1024, got %d", got.SizeBytes)
	}

	if got.ContentType != "image/webp" {
		t.Fatalf("expected content type image/webp, got %q", got.ContentType)
	}

	stored := repo.assets[asset.UUID]
	if stored.Status != mediadomain.StatusReady {
		t.Fatalf("expected stored asset to be ready, got %q", stored.Status)
	}

	if stored.SizeBytes != 1024 {
		t.Fatalf("expected stored size 1024, got %d", stored.SizeBytes)
	}
}

func TestCompleteUploadAlreadyReadyIsIdempotent(t *testing.T) {
	t.Parallel()

	svc, repo, storage := newTestService()
	asset := makePendingAsset()
	asset.Status = mediadomain.StatusReady
	asset.SizeBytes = 256
	asset = repo.store(asset)

	got, err := svc.CompleteUpload(context.Background(), asset.UUID)
	if err != nil {
		t.Fatalf("CompleteUpload returned error: %v", err)
	}

	if got.UUID != asset.UUID {
		t.Fatalf("expected same asset ID %s, got %s", asset.UUID, got.UUID)
	}

	if storage.headCalls != 0 {
		t.Fatalf("expected no storage head calls, got %d", storage.headCalls)
	}
}

func TestCompleteUploadExpiredPendingAsset(t *testing.T) {
	t.Parallel()

	svc, repo, _ := newTestService()
	asset := makePendingAsset()
	expired := baseTime().Add(-time.Minute)
	asset.PresignExpiresAt = &expired
	repo.store(asset)

	_, err := svc.CompleteUpload(context.Background(), asset.UUID)
	if !errors.Is(err, ErrUploadExpired) {
		t.Fatalf("expected ErrUploadExpired, got %v", err)
	}
}

type fakeRepo struct {
	assets map[uuid.UUID]mediadomain.MediaAsset
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{assets: make(map[uuid.UUID]mediadomain.MediaAsset)}
}

func (r *fakeRepo) Create(_ context.Context, asset mediadomain.MediaAsset) (mediadomain.MediaAsset, error) {
	return r.store(asset), nil
}

func (r *fakeRepo) GetByID(_ context.Context, id uuid.UUID) (mediadomain.MediaAsset, error) {
	asset, ok := r.assets[id]
	if !ok {
		return mediadomain.MediaAsset{}, ErrNotFound
	}

	return cloneAsset(asset), nil
}

func (r *fakeRepo) Update(_ context.Context, asset mediadomain.MediaAsset) (mediadomain.MediaAsset, error) {
	return r.store(asset), nil
}

func (r *fakeRepo) List(_ context.Context, filter mediadomain.ListFilter) (mediadomain.ListResult, error) {
	items := make([]mediadomain.MediaAsset, 0, len(r.assets))
	for _, asset := range r.assets {
		if asset.DeletedAt != nil {
			continue
		}

		items = append(items, cloneAsset(asset))
	}

	return mediadomain.ListResult{Items: items, Total: int64(len(items)), Page: filter.Page, PerPage: filter.PerPage}, nil
}

func (r *fakeRepo) SoftDelete(_ context.Context, id uuid.UUID, deletedAt time.Time) error {
	asset, ok := r.assets[id]
	if !ok || asset.DeletedAt != nil {
		return ErrNotFound
	}

	asset.DeletedAt = &deletedAt
	asset.UpdatedAt = deletedAt
	r.assets[id] = asset

	return nil
}

func (r *fakeRepo) ClearEntityReferences(_ context.Context, _ uuid.UUID) error {
	return nil
}

func (r *fakeRepo) store(asset mediadomain.MediaAsset) mediadomain.MediaAsset {
	cloned := cloneAsset(asset)
	r.assets[asset.UUID] = cloned

	return cloneAsset(cloned)
}

type fakeObjectStorage struct {
	presignURL     string
	presignHeaders map[string]string
	lastPresignKey string
	headInfo       map[string]ports.ObjectInfo
	headErr        error
	headCalls      int
	putErr         error
	puts           map[string]string
	objects        map[string][]byte
	readErr        error
}

func (s *fakeObjectStorage) ReadPrefix(_ context.Context, key string, n int64) ([]byte, error) {
	if s.readErr != nil {
		return nil, s.readErr
	}

	body, ok := s.objects[key]
	if !ok {
		return nil, ports.ErrObjectNotFound
	}

	return body[:min(int64(len(body)), n)], nil
}

func (s *fakeObjectStorage) PutObject(_ context.Context, key, contentType string, _ []byte) error {
	if s.putErr != nil {
		return s.putErr
	}

	if s.puts == nil {
		s.puts = map[string]string{}
	}

	s.puts[key] = contentType

	return nil
}

func newFakeObjectStorage() *fakeObjectStorage {
	return &fakeObjectStorage{
		presignURL: "https://upload.test/put",
		presignHeaders: map[string]string{
			"Content-Type": "image/png",
		},
		headInfo: make(map[string]ports.ObjectInfo),
		objects:  make(map[string][]byte),
	}
}

func (s *fakeObjectStorage) PresignPut(_ context.Context, key, _ string, _ time.Duration) (string, map[string]string, error) {
	s.lastPresignKey = key
	return s.presignURL, cloneHeaders(s.presignHeaders), nil
}

func (s *fakeObjectStorage) HeadObject(_ context.Context, key string) (ports.ObjectInfo, error) {
	s.headCalls++
	if s.headErr != nil {
		return ports.ObjectInfo{}, s.headErr
	}

	info, ok := s.headInfo[key]
	if !ok {
		return ports.ObjectInfo{}, ports.ErrObjectNotFound
	}

	return info, nil
}

type fakeIDs struct {
	next uuid.UUID
}

func (f fakeIDs) New() uuid.UUID {
	return f.next
}

type fakeClock struct {
	now time.Time
}

func (f fakeClock) Now() time.Time {
	return f.now
}

func newTestService() (*Service, *fakeRepo, *fakeObjectStorage) {
	repo := newFakeRepo()
	storage := newFakeObjectStorage()
	svc := New(
		repo,
		storage,
		fakeIDs{next: fixedID()},
		fakeClock{now: baseTime()},
		"minio",
		[]string{"image/png", "image/webp"},
		2048,
		15*time.Minute,
	)

	return svc, repo, storage
}

func makePendingAsset() mediadomain.MediaAsset {
	expiresAt := baseTime().Add(15 * time.Minute)

	return mediadomain.MediaAsset{
		UUID:             fixedID(),
		StorageKey:       "media/22222222-2222-2222-2222-222222222222/cover.png",
		OriginalFilename: "cover.png",
		ContentType:      "image/png",
		Disk:             "minio",
		Status:           mediadomain.StatusPending,
		PresignExpiresAt: &expiresAt,
		CreatedAt:        baseTime(),
		UpdatedAt:        baseTime(),
	}
}

func fixedID() uuid.UUID {
	return uuid.MustParse("22222222-2222-2222-2222-222222222222")
}

func baseTime() time.Time {
	return time.Date(2026, 7, 31, 10, 0, 0, 0, time.UTC)
}

func cloneAsset(asset mediadomain.MediaAsset) mediadomain.MediaAsset {
	if asset.Tags != nil {
		asset.Tags = append([]string(nil), asset.Tags...)
	}

	return asset
}

func cloneHeaders(headers map[string]string) map[string]string {
	if headers == nil {
		return nil
	}

	cloned := make(map[string]string, len(headers))
	maps.Copy(cloned, headers)

	return cloned
}
