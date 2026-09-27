package persistence

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	mediadomain "github.com/turahe/blog-api/internal/core/media/domain"
)

func TestPostMediaRepositoryReplaceAll(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewPostMediaRepository(tx)
	ctx := t.Context()
	post := createPost(t, NewPostRepository(tx), postFixture{author: insertUser(t, tx), createdAt: time.Now().UTC()}).UUID
	cover, inline, attachment := insertReadyMedia(t, tx), insertReadyMedia(t, tx), insertReadyMedia(t, tx)

	listed := func() []mediadomain.PostMediaItem {
		t.Helper()

		items, err := repo.ListByPostID(ctx, post)
		require.NoError(t, err)

		return items
	}

	require.NoError(t, repo.ReplaceAll(ctx, post, []mediadomain.PostMediaItem{
		{MediaAssetUUID: attachment, Kind: mediadomain.KindAttachment, SortOrder: 2},
		{MediaAssetUUID: cover, Kind: mediadomain.KindCover, SortOrder: 0},
		{MediaAssetUUID: inline, Kind: mediadomain.KindInlineImage, SortOrder: 1},
	}))

	want := []mediadomain.PostMediaItem{
		{MediaAssetUUID: cover, Kind: mediadomain.KindCover, SortOrder: 0},
		{MediaAssetUUID: inline, Kind: mediadomain.KindInlineImage, SortOrder: 1},
		{MediaAssetUUID: attachment, Kind: mediadomain.KindAttachment, SortOrder: 2},
	}
	require.Equal(t, want, listed(), "listed in sort order")

	err := repo.ReplaceAll(ctx, post, []mediadomain.PostMediaItem{
		{MediaAssetUUID: cover, Kind: mediadomain.KindCover},
		{MediaAssetUUID: uuid.New(), Kind: mediadomain.KindAttachment},
	})
	require.ErrorIs(t, err, errUnknownReference)
	require.Equal(t, want, listed(), "an unknown asset rolls back the replacement")

	require.ErrorIs(t, repo.ReplaceAll(ctx, uuid.New(), nil), errUnknownReference)

	require.NoError(t, repo.ReplaceAll(ctx, post, nil))
	require.Empty(t, listed())

	_, err = repo.ListByPostID(canceledContext(t), post)
	require.ErrorIs(t, err, context.Canceled)
}

func TestPostMediaRepositoryReplaceAllRejectsDuplicateAttachment(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewPostMediaRepository(tx)
	post := createPost(t, NewPostRepository(tx), postFixture{author: insertUser(t, tx), createdAt: time.Now().UTC()}).UUID
	asset := insertReadyMedia(t, tx)

	err := repo.ReplaceAll(t.Context(), post, []mediadomain.PostMediaItem{
		{MediaAssetUUID: asset, Kind: mediadomain.KindCover},
		{MediaAssetUUID: asset, Kind: mediadomain.KindCover},
	})
	require.True(t, IsUniqueViolation(err), "an asset is attached once per kind: %v", err)
}
