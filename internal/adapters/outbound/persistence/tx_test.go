package persistence

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	tagdomain "github.com/turahe/blog-api/internal/core/tag/domain"
)

func TestTransactorInTx(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	tags := NewTagRepository(tx)
	transactor := NewTransactor(tx)
	errRetry := errors.New("retry")

	create := func(ctx context.Context, slug string) error {
		_, err := tags.Create(ctx, tagdomain.Tag{UUID: uuid.New(), Name: slug, Slug: slug, CreatedAt: time.Now().UTC()})
		return err
	}

	exists := func(slug string) bool {
		t.Helper()

		_, err := tags.GetBySlug(t.Context(), slug)
		if errors.Is(err, tagdomain.ErrNotFound) {
			return false
		}

		require.NoError(t, err)

		return true
	}

	kept, discarded, outer, inner := uniqueSlug("kept"), uniqueSlug("discarded"), uniqueSlug("outer"), uniqueSlug("inner")

	require.NoError(t, transactor.InTx(t.Context(), func(ctx context.Context) error { return create(ctx, kept) }))
	require.True(t, exists(kept))

	err := transactor.InTx(t.Context(), func(ctx context.Context) error {
		require.NoError(t, create(ctx, discarded))
		return errRetry
	})
	require.ErrorIs(t, err, errRetry)
	require.False(t, exists(discarded), "a failed fn rolls back its writes")

	require.NoError(t, transactor.InTx(t.Context(), func(ctx context.Context) error {
		require.NoError(t, create(ctx, outer))

		require.ErrorIs(t, transactor.InTx(ctx, func(ctx context.Context) error {
			require.NoError(t, create(ctx, inner))
			return errRetry
		}), errRetry)

		require.True(t, exists(outer), "the caller's transaction stays usable after a nested failure")

		return nil
	}))
	require.True(t, exists(outer))
	require.False(t, exists(inner))
}
