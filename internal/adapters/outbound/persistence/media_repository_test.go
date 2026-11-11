package persistence

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	mediadomain "github.com/turahe/blog-api/internal/core/media/domain"
	"github.com/turahe/blog-api/internal/shared/pagination"
	"gorm.io/gorm"
)

func TestMediaTagsNeverEncodesNull(t *testing.T) {
	t.Parallel()

	for name, tags := range map[string][]string{
		"nil":   nil,
		"empty": {},
	} {
		value, err := mediaTags(tags).Value()
		require.NoError(t, err, name)
		require.Equal(t, "{}", value, name)
	}
}

func TestMediaTagsCopiesInput(t *testing.T) {
	t.Parallel()

	in := []string{"a", "b"}
	out := mediaTags(in)
	in[0] = "changed"

	require.Equal(t, []string{"a", "b"}, []string(out))
}

func insertMediaRow(t *testing.T, tx *gorm.DB, uploader uuid.UUID, name, status, contentType string, size int64, presignExpiry, deletedAt *time.Time) uuid.UUID {
	t.Helper()

	return insertUUID(t, tx,
		`INSERT INTO media_assets (storage_key, original_filename, content_type, size_bytes, disk, status,
		   uploaded_by, presign_expires_at, deleted_at)
		 VALUES (?, ?, ?, ?, 'minio', ?, (SELECT id FROM users WHERE uuid = ?), ?, ?) RETURNING uuid`,
		uniqueSlug("media/repo"), name, contentType, size, status, uploader, presignExpiry, deletedAt)
}

func ids(assets []mediadomain.MediaAsset) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(assets))
	for _, a := range assets {
		out = append(out, a.UUID)
	}

	return out
}

func newMediaAsset(mutate func(*mediadomain.MediaAsset)) mediadomain.MediaAsset {
	asset := mediadomain.MediaAsset{
		UUID: uuid.New(), StorageKey: uniqueSlug("media/create"), OriginalFilename: "photo.png",
		ContentType: "image/png", Disk: "minio", Status: mediadomain.StatusPending,
	}
	if mutate != nil {
		mutate(&asset)
	}

	return asset
}

func TestMediaRepositoryCreate(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewMediaRepository(tx)
	ctx := t.Context()
	uploader := insertUser(t, tx)
	width, height, checksum := 640, 480, "abc123"
	created := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	trashed := created.Add(time.Minute)
	unknown := uuid.New()

	tests := []struct {
		name    string
		mutate  func(*mediadomain.MediaAsset)
		check   func(t *testing.T, got mediadomain.MediaAsset)
		wantErr error
	}{
		{
			name: "all fields",
			mutate: func(a *mediadomain.MediaAsset) {
				a.UploadedByUUID, a.Width, a.Height, a.ChecksumSHA256 = &uploader, &width, &height, &checksum
				a.Tags, a.SizeBytes, a.CreatedAt = []string{"avatar"}, 42, created
			},
			check: func(t *testing.T, got mediadomain.MediaAsset) {
				require.Equal(t, &uploader, got.UploadedByUUID)
				require.Equal(t, &width, got.Width)
				require.Equal(t, &checksum, got.ChecksumSHA256)
				require.Equal(t, []string{"avatar"}, got.Tags)
				require.WithinDuration(t, created, got.CreatedAt, 0)
				require.WithinDuration(t, created, got.UpdatedAt, 0, "updated_at defaults to created_at")
			},
		},
		{
			name: "defaults timestamps",
			check: func(t *testing.T, got mediadomain.MediaAsset) {
				require.Nil(t, got.UploadedByUUID)
				require.Empty(t, got.Tags)
				require.WithinDuration(t, time.Now(), got.CreatedAt, time.Minute)
				require.Equal(t, got.CreatedAt, got.UpdatedAt)
			},
		},
		{
			name:   "soft-deleted",
			mutate: func(a *mediadomain.MediaAsset) { a.DeletedAt = &trashed },
			check: func(t *testing.T, got mediadomain.MediaAsset) {
				require.NotNil(t, got.DeletedAt)
				require.WithinDuration(t, trashed, *got.DeletedAt, 0)
			},
		},
		{name: "unknown uploader", mutate: func(a *mediadomain.MediaAsset) { a.UploadedByUUID = &unknown }, wantErr: errUnknownReference},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := repo.Create(ctx, newMediaAsset(tt.mutate))
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			require.NotZero(t, got.ID)
			tt.check(t, got)
		})
	}

	inSavepoint(t, tx, func() {
		_, err := repo.Create(ctx, newMediaAsset(func(a *mediadomain.MediaAsset) { a.Status = "bogus" }))
		require.Error(t, err)
	})
}

func TestMediaRepositoryUpdate(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewMediaRepository(tx)
	ctx := t.Context()
	uploader := insertUser(t, tx)
	unknown := uuid.New()

	asset, err := repo.Create(ctx, newMediaAsset(nil))
	require.NoError(t, err)

	checksum := "def456"
	asset.Status, asset.SizeBytes, asset.ChecksumSHA256 = mediadomain.StatusReady, 2048, &checksum
	asset.Tags, asset.UploadedByUUID, asset.UpdatedAt = []string{"cover", "hero"}, &uploader, time.Time{}

	updated, err := repo.Update(ctx, asset)
	require.NoError(t, err)
	require.Equal(t, mediadomain.StatusReady, updated.Status)
	require.Equal(t, int64(2048), updated.SizeBytes)
	require.Equal(t, &checksum, updated.ChecksumSHA256)
	require.Equal(t, []string{"cover", "hero"}, updated.Tags)
	require.Equal(t, &uploader, updated.UploadedByUUID)
	require.WithinDuration(t, time.Now(), updated.UpdatedAt, time.Minute)

	_, err = repo.Update(ctx, newMediaAsset(nil))
	require.ErrorIs(t, err, mediadomain.ErrNotFound, "updating a missing asset reports it missing")

	asset.UploadedByUUID = &unknown
	_, err = repo.Update(ctx, asset)
	require.ErrorIs(t, err, errUnknownReference)

	inSavepoint(t, tx, func() {
		asset.UploadedByUUID, asset.Status = nil, "bogus"
		_, err := repo.Update(ctx, asset)
		require.Error(t, err)
	})

	_, err = repo.GetByID(canceledContext(t), asset.UUID)
	require.ErrorIs(t, err, context.Canceled)
}

func TestMediaRepositorySoftDelete(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewMediaRepository(tx)
	ctx := t.Context()
	asset := insertReadyMedia(t, tx)
	deletedAt := time.Now().UTC().Truncate(time.Microsecond)

	require.NoError(t, repo.SoftDelete(ctx, asset, deletedAt))

	_, err := repo.GetByID(ctx, asset)
	require.ErrorIs(t, err, mediadomain.ErrNotFound)

	trashed, err := repo.Trashed(ctx, deletedAt.Add(time.Second), 1000)
	require.NoError(t, err)
	require.Contains(t, ids(trashed), asset)

	require.ErrorIs(t, repo.SoftDelete(ctx, asset, deletedAt), mediadomain.ErrNotFound, "already deleted")
	require.ErrorIs(t, repo.SoftDelete(canceledContext(t), asset, deletedAt), context.Canceled)
}

func TestMediaRepositoryClearEntityReferences(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewMediaRepository(tx)
	ctx := t.Context()
	asset, other := insertReadyMedia(t, tx), insertReadyMedia(t, tx)
	user, category := insertUser(t, tx), insertCategory(t, tx)
	post := createPost(t, NewPostRepository(tx), postFixture{author: user, createdAt: time.Now().UTC()}).UUID

	mediaID := "(SELECT id FROM media_assets WHERE uuid = ?)"
	require.NoError(t, tx.Exec("UPDATE users SET avatar_id = "+mediaID+" WHERE uuid = ?", asset, user).Error)
	require.NoError(t, tx.Exec("UPDATE categories SET image_id = "+mediaID+" WHERE uuid = ?", asset, category).Error)
	require.NoError(t, tx.Exec("UPDATE posts SET cover_image_media_id = "+mediaID+" WHERE uuid = ?", asset, post).Error)
	require.NoError(t, NewPostMediaRepository(tx).ReplaceAll(ctx, post, []mediadomain.PostMediaItem{
		{MediaAssetUUID: asset, Kind: mediadomain.KindInlineImage},
		{MediaAssetUUID: other, Kind: mediadomain.KindAttachment},
	}))

	require.NoError(t, repo.ClearEntityReferences(ctx, asset))

	var references int64
	require.NoError(t, tx.Raw(`SELECT
		(SELECT count(*) FROM users WHERE avatar_id = `+mediaID+`) +
		(SELECT count(*) FROM categories WHERE image_id = `+mediaID+`) +
		(SELECT count(*) FROM posts WHERE cover_image_media_id = `+mediaID+`) +
		(SELECT count(*) FROM post_media WHERE media_asset_id = `+mediaID+`)`,
		asset, asset, asset, asset).Row().Scan(&references))
	require.Zero(t, references)

	items, err := NewPostMediaRepository(tx).ListByPostID(ctx, post)
	require.NoError(t, err)
	require.Equal(t, []mediadomain.PostMediaItem{{MediaAssetUUID: other, Kind: mediadomain.KindAttachment}}, items,
		"other assets keep their attachments")

	require.NoError(t, repo.ClearEntityReferences(ctx, uuid.New()), "an unknown asset has nothing to clear")
	require.ErrorIs(t, repo.ClearEntityReferences(canceledContext(t), asset), context.Canceled)
}

func TestMediaRepositoryOrphansPurgeAndUsage(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	ctx := t.Context()
	repo := NewMediaRepository(tx)
	uploader := insertUser(t, tx)

	epoch := time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)
	expired, trashedAt := epoch, epoch.Add(time.Hour)
	name := uniqueSlug("orphan")

	abandoned := insertMediaRow(t, tx, uploader, name+"-a.png", mediadomain.StatusPending, "image/png", 0, &expired, nil)
	used := insertMediaRow(t, tx, uploader, name+"-u.png", mediadomain.StatusReady, "image/png", 400, &expired, nil)
	unused := insertMediaRow(t, tx, uploader, name+"-n.webp", mediadomain.StatusReady, "image/webp", 200, &expired, nil)
	trashed := insertMediaRow(t, tx, uploader, name+"-t.png", mediadomain.StatusReady, "image/png", 100, &expired, &trashedAt)

	require.NoError(t, tx.Exec(`UPDATE users SET avatar_id = (SELECT id FROM media_assets WHERE uuid = ?) WHERE uuid = ?`, used, uploader).Error)

	cutoff := epoch.Add(2 * time.Hour)

	gotAbandoned, err := repo.Abandoned(ctx, cutoff, 100)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{abandoned}, ids(gotAbandoned))

	gotTrashed, err := repo.Trashed(ctx, cutoff, 100)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{trashed}, ids(gotTrashed))

	unusedPage, err := repo.List(ctx, mediadomain.ListFilter{Query: name, Unused: true, PageRequest: pagination.PageRequest{Page: 1, Limit: 10}})
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{unused}, ids(unusedPage.Items))

	usage, err := repo.Usage(ctx, mediadomain.UsageFilter{UploadedBy: &uploader, TopLimit: 5})
	require.NoError(t, err)
	require.Equal(t, mediadomain.UsageRow{Count: 4, Bytes: 700}, usage.Total)
	require.ElementsMatch(t, []mediadomain.UsageRow{
		{Key: mediadomain.StatusReady, Count: 2, Bytes: 600},
		{Key: mediadomain.StatusDeleted, Count: 1, Bytes: 100},
		{Key: mediadomain.StatusPending, Count: 1, Bytes: 0},
	}, usage.ByStatus)
	require.ElementsMatch(t, []mediadomain.UsageRow{
		{Key: "image/png", Count: 3, Bytes: 500},
		{Key: "image/webp", Count: 1, Bytes: 200},
	}, usage.ByContentType)
	require.Len(t, usage.TopUploaders, 1)
	require.Equal(t, &uploader, usage.TopUploaders[0].UserUUID)
	require.Equal(t, int64(700), usage.TopUploaders[0].Bytes)

	for _, id := range []uuid.UUID{abandoned, trashed} {
		purged, err := repo.Purge(ctx, id)
		require.NoError(t, err)
		require.True(t, purged)
	}

	purged, err := repo.Purge(ctx, used)
	require.NoError(t, err)
	require.False(t, purged, "a live ready asset is never purged")

	_, err = repo.GetByID(ctx, used)
	require.NoError(t, err)
}
