package service

import (
	"context"
	"errors"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
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
	assets        map[uuid.UUID]mediadomain.MediaAsset
	usageFilter   mediadomain.UsageFilter
	listFilter    mediadomain.ListFilter
	getErr        error
	createErr     error
	updateErr     error
	clearErr      error
	softDeleteErr error
	abandonedErr  error
	trashedErr    error
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{assets: make(map[uuid.UUID]mediadomain.MediaAsset)}
}

func (r *fakeRepo) Create(_ context.Context, asset mediadomain.MediaAsset) (mediadomain.MediaAsset, error) {
	if r.createErr != nil {
		return mediadomain.MediaAsset{}, r.createErr
	}

	return r.store(asset), nil
}

func (r *fakeRepo) GetByID(_ context.Context, id uuid.UUID) (mediadomain.MediaAsset, error) {
	if r.getErr != nil {
		return mediadomain.MediaAsset{}, r.getErr
	}

	asset, ok := r.assets[id]
	if !ok {
		return mediadomain.MediaAsset{}, ErrNotFound
	}

	return cloneAsset(asset), nil
}

func (r *fakeRepo) Update(_ context.Context, asset mediadomain.MediaAsset) (mediadomain.MediaAsset, error) {
	if r.updateErr != nil {
		return mediadomain.MediaAsset{}, r.updateErr
	}

	return r.store(asset), nil
}

func (r *fakeRepo) List(_ context.Context, filter mediadomain.ListFilter) (mediadomain.ListResult, error) {
	r.listFilter = filter

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
	if r.softDeleteErr != nil {
		return r.softDeleteErr
	}

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
	return r.clearErr
}

func (r *fakeRepo) store(asset mediadomain.MediaAsset) mediadomain.MediaAsset {
	cloned := cloneAsset(asset)
	r.assets[asset.UUID] = cloned

	return cloneAsset(cloned)
}

type fakeObjectStorage struct {
	presignURL     string
	presignHeaders map[string]string
	presignErr     error
	lastPresignKey string
	headInfo       map[string]ports.ObjectInfo
	headErr        error
	headCalls      int
	putErr         error
	puts           map[string]string
	objects        map[string][]byte
	readErr        error
	deleteErr      error
	deleted        []string
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
	if s.presignErr != nil {
		return "", nil, s.presignErr
	}

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

func TestNewIgnoresBlankMIMEEntries(t *testing.T) {
	t.Parallel()

	svc := New(newFakeRepo(), newFakeObjectStorage(), fakeIDs{next: fixedID()}, fakeClock{now: baseTime()},
		" minio ", []string{"  ", " IMAGE/PNG "}, 2048, time.Minute)

	result, err := svc.PresignUpload(t.Context(), nil, "a.png", "image/png", 10, nil)
	require.NoError(t, err)
	require.Equal(t, "minio", result.Asset.Disk)

	_, err = svc.PresignUpload(t.Context(), nil, "a.png", "", 10, nil)
	require.ErrorIs(t, err, ErrValidation, "a blank entry does not allow a blank content type")
}

func TestPresignUploadFailures(t *testing.T) {
	t.Parallel()

	failure := errors.New("backend down")

	tests := []struct {
		name  string
		setup func(*fakeRepo, *fakeObjectStorage)
		want  error
	}{
		{name: "create fails", setup: func(repo *fakeRepo, _ *fakeObjectStorage) { repo.createErr = failure }, want: failure},
		{name: "presign fails", setup: func(_ *fakeRepo, storage *fakeObjectStorage) { storage.presignErr = failure }, want: ErrStorage},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc, repo, storage := newTestService()
			tc.setup(repo, storage)

			_, err := svc.PresignUpload(t.Context(), nil, "a.png", "image/png", 10, nil)

			require.ErrorIs(t, err, tc.want)
		})
	}
}

func TestCompleteUploadFailures(t *testing.T) {
	t.Parallel()

	failure := errors.New("backend down")

	tests := []struct {
		name  string
		setup func(*fakeRepo, *fakeObjectStorage, *mediadomain.MediaAsset)
		want  error
	}{
		{
			name:  "missing asset",
			setup: func(repo *fakeRepo, _ *fakeObjectStorage, _ *mediadomain.MediaAsset) { repo.getErr = ErrNotFound },
			want:  ErrNotFound,
		},
		{
			name:  "lookup fails",
			setup: func(repo *fakeRepo, _ *fakeObjectStorage, _ *mediadomain.MediaAsset) { repo.getErr = failure },
			want:  failure,
		},
		{
			name: "deleted asset",
			setup: func(_ *fakeRepo, _ *fakeObjectStorage, asset *mediadomain.MediaAsset) {
				deleted := baseTime()
				asset.DeletedAt = &deleted
			},
			want: ErrNotFound,
		},
		{
			name: "head fails",
			setup: func(_ *fakeRepo, storage *fakeObjectStorage, _ *mediadomain.MediaAsset) {
				storage.headErr = failure
			},
			want: ErrStorage,
		},
		{
			name: "stored type not allowed",
			setup: func(_ *fakeRepo, storage *fakeObjectStorage, asset *mediadomain.MediaAsset) {
				storage.headInfo[asset.StorageKey] = ports.ObjectInfo{Size: 64, ContentType: "text/html"}
			},
			want: ErrValidation,
		},
		{
			name: "stored size too large",
			setup: func(_ *fakeRepo, storage *fakeObjectStorage, asset *mediadomain.MediaAsset) {
				storage.headInfo[asset.StorageKey] = ports.ObjectInfo{Size: 4096, ContentType: "image/png"}
			},
			want: ErrValidation,
		},
		{
			name: "update fails",
			setup: func(repo *fakeRepo, storage *fakeObjectStorage, asset *mediadomain.MediaAsset) {
				storage.headInfo[asset.StorageKey] = ports.ObjectInfo{Size: 64, ContentType: "image/webp"}
				storage.objects[asset.StorageKey] = mustBase64(t, tinyWebP)
				repo.updateErr = failure
			},
			want: failure,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc, repo, storage := newTestService()
			asset := makePendingAsset()
			tc.setup(repo, storage, &asset)
			repo.store(asset)

			_, err := svc.CompleteUpload(t.Context(), asset.UUID)

			require.ErrorIs(t, err, tc.want)
			require.Equal(t, mediadomain.StatusPending, repo.assets[asset.UUID].Status)
		})
	}
}

func TestCompleteUploadWithoutPresignExpiryNeverExpires(t *testing.T) {
	t.Parallel()

	svc, repo, storage := newTestService()
	asset := makePendingAsset()
	asset.PresignExpiresAt = nil
	repo.store(asset)
	storage.headInfo[asset.StorageKey] = ports.ObjectInfo{Size: 64, ContentType: "image/webp"}
	storage.objects[asset.StorageKey] = mustBase64(t, tinyWebP)

	got, err := svc.CompleteUpload(t.Context(), asset.UUID)

	require.NoError(t, err)
	require.Equal(t, mediadomain.StatusReady, got.Status)
}

func TestListNormalisesFilter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   mediadomain.ListFilter
		want mediadomain.ListFilter
	}{
		{
			name: "defaults and trims",
			in:   mediadomain.ListFilter{Page: 0, PerPage: 0, Query: "  cat  ", Disk: " MINIO ", Status: " Ready "},
			want: mediadomain.ListFilter{Page: 1, PerPage: 20, Query: "cat", Disk: "minio", Status: "ready"},
		},
		{
			name: "caps page size",
			in:   mediadomain.ListFilter{Page: 3, PerPage: 101},
			want: mediadomain.ListFilter{Page: 3, PerPage: 20},
		},
		{
			name: "keeps valid paging",
			in:   mediadomain.ListFilter{Page: 2, PerPage: 100},
			want: mediadomain.ListFilter{Page: 2, PerPage: 100},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc, repo, _ := newTestService()
			repo.store(makePendingAsset())

			got, err := svc.List(t.Context(), tc.in)

			require.NoError(t, err)
			require.Equal(t, tc.want, repo.listFilter)
			require.Len(t, got.Items, 1)
		})
	}
}

func TestGetPassesThroughLookupFailure(t *testing.T) {
	t.Parallel()

	failure := errors.New("backend down")
	svc, repo, _ := newTestService()
	repo.getErr = failure

	_, err := svc.Get(t.Context(), fixedID())

	require.ErrorIs(t, err, failure)
}

func TestDeleteFailures(t *testing.T) {
	t.Parallel()

	failure := errors.New("backend down")

	tests := []struct {
		name  string
		setup func(*fakeRepo)
		id    uuid.UUID
		want  error
	}{
		{name: "missing asset", setup: func(*fakeRepo) {}, id: uuid.New(), want: ErrNotFound},
		{name: "clearing references fails", setup: func(repo *fakeRepo) { repo.clearErr = failure }, id: fixedID(), want: failure},
		{name: "soft delete fails", setup: func(repo *fakeRepo) { repo.softDeleteErr = failure }, id: fixedID(), want: failure},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc, repo, _ := newTestService()
			repo.store(makePendingAsset())
			tc.setup(repo)

			require.ErrorIs(t, svc.Delete(t.Context(), tc.id), tc.want)
			require.Nil(t, repo.assets[fixedID()].DeletedAt)
		})
	}
}

func TestUpdateTags(t *testing.T) {
	t.Parallel()

	failure := errors.New("backend down")
	many := make([]string, 51)
	for i := range many {
		many[i] = "tag-" + strings.Repeat("x", i+1)
	}

	tests := []struct {
		name  string
		id    uuid.UUID
		tags  []string
		setup func(*fakeRepo)
		want  []string
		err   error
	}{
		{
			name: "trims, drops blanks, and de-duplicates case-insensitively",
			id:   fixedID(),
			tags: []string{"  Cats ", "", "   ", "cats", "Dogs"},
			want: []string{"Cats", "Dogs"},
		},
		{name: "clears with no tags", id: fixedID(), tags: nil, want: []string{}},
		{name: "missing asset", id: uuid.New(), tags: []string{"a"}, err: ErrNotFound},
		{name: "tag too long", id: fixedID(), tags: []string{strings.Repeat("a", 65)}, err: ErrValidation},
		{name: "too many tags", id: fixedID(), tags: many, err: ErrValidation},
		{name: "update fails", id: fixedID(), tags: []string{"a"}, setup: func(repo *fakeRepo) { repo.updateErr = failure }, err: failure},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc, repo, _ := newTestService()
			original := makePendingAsset()
			original.Tags = []string{"old"}
			repo.store(original)

			if tc.setup != nil {
				tc.setup(repo)
			}

			got, err := svc.UpdateTags(t.Context(), tc.id, tc.tags)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
				require.Equal(t, []string{"old"}, repo.assets[fixedID()].Tags)

				return
			}

			require.NoError(t, err)
			require.ElementsMatch(t, tc.want, got.Tags)
			require.ElementsMatch(t, tc.want, repo.assets[fixedID()].Tags)
			require.Equal(t, baseTime(), got.UpdatedAt)
		})
	}
}

func TestSanitizeFilenameRejectsDotNames(t *testing.T) {
	t.Parallel()

	for _, name := range []string{".", ".."} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := sanitizeFilename(name)
			require.ErrorIs(t, err, ErrValidation)
		})
	}
}
