package persistence

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
	"gorm.io/gorm"
)

type searchPost struct {
	title, excerpt, content string
	status                  postdomain.Status
	category                *uuid.UUID
}

func createSearchPost(t *testing.T, tx *gorm.DB, author uuid.UUID, p searchPost) postdomain.Post {
	t.Helper()

	if p.status == "" {
		p.status = postdomain.StatusPublished
	}

	now := time.Now().UTC()
	post, err := NewPostRepository(tx).Create(t.Context(), postdomain.Post{
		UUID: uuid.New(), AuthorUUID: author, CategoryUUID: p.category, Title: p.title,
		Slug: uniqueSlug("search"), Excerpt: p.excerpt, Content: p.content, Status: p.status,
		Version: 1, PublishedAt: &now, CreatedAt: now, UpdatedAt: now,
	})
	require.NoError(t, err)

	return post
}

func TestPostSearchRanksFiltersAndHighlights(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	ctx := t.Context()
	author := insertUser(t, tx)
	category := insertCategory(t, tx)
	word := "zq" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")

	inContent := createSearchPost(t, tx, author, searchPost{
		title: "Weekly notes", content: "Some filler text. Then <script>alert(1)</script> " + word + " appears here.",
	})
	inTitle := createSearchPost(t, tx, author, searchPost{
		title: "All about " + word, content: "Nothing else.", category: &category,
	})
	createSearchPost(t, tx, author, searchPost{title: word + " draft", status: postdomain.StatusDraft})
	deleted := createSearchPost(t, tx, author, searchPost{title: word + " deleted"})
	softDelete(t, NewPostRepository(tx), deleted)

	search := NewPostSearch(tx, "simple")

	got, err := search.SearchPublished(ctx, postdomain.SearchFilter{Query: word, Page: 1, PerPage: 10})
	require.NoError(t, err)
	require.EqualValues(t, 2, got.Total, "drafts and deleted posts are excluded")
	require.Len(t, got.Items, 2)
	require.Equal(t, inTitle.UUID, got.Items[0].Post.UUID, "a title match outranks a content match")
	require.Equal(t, inContent.UUID, got.Items[1].Post.UUID)
	require.Greater(t, got.Items[0].Rank, got.Items[1].Rank)
	require.Equal(t, author, got.Items[0].Post.AuthorUUID)

	require.Equal(t, "All about <mark>"+word+"</mark>", got.Items[0].Title)
	snippet := got.Items[1].Snippet
	require.Contains(t, snippet, "<mark>"+word+"</mark>")
	require.NotContains(t, snippet, "<script>", "content is HTML-escaped")
	require.Equal(t, "Weekly notes", got.Items[1].Title)

	got, err = search.SearchPublished(ctx, postdomain.SearchFilter{Query: word, Page: 2, PerPage: 1})
	require.NoError(t, err)
	require.EqualValues(t, 2, got.Total)
	require.Len(t, got.Items, 1)
	require.Equal(t, inContent.UUID, got.Items[0].Post.UUID)

	got, err = search.SearchPublished(ctx, postdomain.SearchFilter{Query: word, Page: 1, PerPage: 10, CategoryUUID: &category})
	require.NoError(t, err)
	require.Len(t, got.Items, 1)
	require.Equal(t, inTitle.UUID, got.Items[0].Post.UUID)

	tag := insertTag(t, tx, inContent.UUID)
	got, err = search.SearchPublished(ctx, postdomain.SearchFilter{Query: word, Page: 1, PerPage: 10, TagUUID: &tag})
	require.NoError(t, err)
	require.Len(t, got.Items, 1)
	require.Equal(t, inContent.UUID, got.Items[0].Post.UUID)

	got, err = search.SearchPublished(ctx, postdomain.SearchFilter{Query: word + " -about", Page: 1, PerPage: 10})
	require.NoError(t, err)
	require.Len(t, got.Items, 1, "web search syntax excludes terms")
	require.Equal(t, inContent.UUID, got.Items[0].Post.UUID)

	got, err = search.SearchPublished(ctx, postdomain.SearchFilter{Query: `"` + word + ` nope"`, Page: 1, PerPage: 10})
	require.NoError(t, err)
	require.Empty(t, got.Items)
	require.NotNil(t, got.Items)
}

//nolint:paralleltest // the rebuild locks posts exclusively, which would stall the other tests' transactions
func TestRebuildPostSearchIndex(t *testing.T) {
	tx := integrationTx(t)
	ctx := t.Context()
	require.NoError(t, tx.Exec("SET LOCAL lock_timeout = '10s'").Error)

	language, err := IndexedSearchLanguage(ctx, tx)
	require.NoError(t, err)
	require.Equal(t, "simple", language, "migration 00028 builds the index for simple")

	var before string
	require.NoError(t, tx.Raw(`SELECT pg_get_expr(d.adbin, d.adrelid) FROM pg_attrdef d
		JOIN pg_attribute a ON a.attrelid = d.adrelid AND a.attnum = d.adnum
		WHERE d.adrelid = 'posts'::regclass AND a.attname = 'search_vector'`).Scan(&before).Error)

	require.ErrorIs(t, RebuildPostSearchIndex(ctx, tx, "klingon"), ErrUnknownSearchLanguage)
	require.ErrorIs(t, RebuildPostSearchIndex(ctx, tx, "simple'; DROP TABLE posts; --"), ErrUnknownSearchLanguage)

	require.NoError(t, RebuildPostSearchIndex(ctx, tx, "simple"))

	var after string
	require.NoError(t, tx.Raw(`SELECT pg_get_expr(d.adbin, d.adrelid) FROM pg_attrdef d
		JOIN pg_attribute a ON a.attrelid = d.adrelid AND a.attnum = d.adnum
		WHERE d.adrelid = 'posts'::regclass AND a.attname = 'search_vector'`).Scan(&after).Error)
	require.Equal(t, before, after, "the rebuild expression matches migration 00028")

	require.NoError(t, RebuildPostSearchIndex(ctx, tx, "english"))
	language, err = IndexedSearchLanguage(ctx, tx)
	require.NoError(t, err)
	require.Equal(t, "english", language)

	author := insertUser(t, tx)
	post := createSearchPost(t, tx, author, searchPost{title: "Running shoes"})

	got, err := NewPostSearch(tx, language).SearchPublished(ctx, postdomain.SearchFilter{Query: "run", Page: 1, PerPage: 50})
	require.NoError(t, err)
	require.Contains(t, searchHitIDs(got.Items), post.UUID, "english stems running to run")
}

func searchHitIDs(items []postdomain.SearchHit) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.Post.UUID)
	}

	return ids
}
