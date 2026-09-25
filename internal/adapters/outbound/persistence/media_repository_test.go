package persistence

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	mediadomain "github.com/turahe/blog-api/internal/core/media/domain"
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

	unusedPage, err := repo.List(ctx, mediadomain.ListFilter{Query: name, Unused: true, Page: 1, PerPage: 10})
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
