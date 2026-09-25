package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	commentdomain "github.com/turahe/blog-api/internal/core/comment/domain"
)

type upperRenderer struct{}

func (upperRenderer) Render(source string) string { return "<p>" + source + "!</p>" }

func TestCreateAndUpdateStoreRenderedHTML(t *testing.T) {
	t.Parallel()

	f := newFixture(Config{Renderer: upperRenderer{}})
	ctx := context.Background()

	created, err := f.svc.Create(ctx, CreateInput{PostUUID: f.post, AuthorUUID: &f.user, Content: " hi "})
	require.NoError(t, err)
	require.Equal(t, "<p>hi!</p>", f.repo.comments[created.UUID].ContentHTML)

	updated, err := f.svc.Update(ctx, f.user, created.UUID, "edited")
	require.NoError(t, err)
	require.Equal(t, "<p>edited!</p>", updated.ContentHTML)
	require.Equal(t, "<p>edited!</p>", f.repo.comments[created.UUID].ContentHTML)
}

func TestReadsRenderRowsWithoutStoredHTML(t *testing.T) {
	t.Parallel()

	f := newFixture(Config{Renderer: upperRenderer{}})
	legacy := f.seed(commentdomain.Comment{Content: "old"})
	stored := f.seed(commentdomain.Comment{Content: "new", ContentHTML: "<p>kept</p>"})

	thread, err := f.svc.GetThread(context.Background(), legacy.UUID)
	require.NoError(t, err)
	require.Equal(t, "<p>old!</p>", thread.Comment.ContentHTML)

	review, err := f.svc.AdminGet(context.Background(), stored.UUID)
	require.NoError(t, err)
	require.Equal(t, "<p>kept</p>", review.Comment.ContentHTML)

	found, err := f.svc.repo.GetByIDs(context.Background(), []uuid.UUID{legacy.UUID})
	require.NoError(t, err)
	require.Equal(t, "<p>old!</p>", found[0].ContentHTML)
}

func TestDefaultRendererEscapes(t *testing.T) {
	t.Parallel()

	f := newFixture(Config{})

	got, err := f.svc.Create(context.Background(), CreateInput{
		PostUUID: f.post, AuthorUUID: &f.user, Content: "<b>x</b> & y\nline\n\nnext",
	})
	require.NoError(t, err)
	require.Equal(t, "<p>&lt;b&gt;x&lt;/b&gt; &amp; y<br>line</p>\n<p>next</p>\n", got.ContentHTML)
}
