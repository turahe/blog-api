package persistence

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	commentdomain "github.com/turahe/blog-api/internal/core/comment/domain"
	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
	"gorm.io/gorm"
)

func insertPublishedPost(t *testing.T, tx *gorm.DB) uuid.UUID {
	t.Helper()

	published := time.Now().UTC()

	return createPost(t, NewPostRepository(tx), postFixture{
		author: insertUser(t, tx), status: postdomain.StatusPublished, publishedAt: &published, createdAt: published,
	}).UUID
}

func createComment(t *testing.T, repo *CommentRepository, c commentdomain.Comment) commentdomain.Comment {
	t.Helper()

	if c.UUID == uuid.Nil {
		c.UUID = uuid.New()
	}

	if c.Status == "" {
		c.Status = commentdomain.StatusApproved
	}

	if c.CreatedAt.IsZero() {
		c.CreatedAt = time.Now().UTC()
		c.UpdatedAt = c.CreatedAt
	}

	got, err := repo.Create(t.Context(), c)
	require.NoError(t, err)

	return got
}

func TestCommentRepositoryPersistsContentHTML(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewCommentRepository(tx)
	post := insertPublishedPost(t, tx)
	author := insertUser(t, tx)

	created := createComment(t, repo, commentdomain.Comment{
		PostUUID: post, AuthorUUID: &author, Content: "**hi**", ContentHTML: "<p><strong>hi</strong></p>",
	})
	require.Equal(t, "<p><strong>hi</strong></p>", created.ContentHTML)

	created.Content, created.ContentHTML = "_edited_", "<p><em>edited</em></p>"
	updated, err := repo.Update(t.Context(), created)
	require.NoError(t, err)
	require.Equal(t, "<p><em>edited</em></p>", updated.ContentHTML)

	list, err := repo.List(t.Context(), commentdomain.ListFilter{
		PostUUID: &post, Statuses: commentdomain.PublicStatuses, Page: 1, PerPage: 10,
	})
	require.NoError(t, err)
	require.Len(t, list.Items, 1)
	require.Equal(t, "<p><em>edited</em></p>", list.Items[0].ContentHTML)
}

func TestCommentRepositoryPostPolicy(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	posts := NewPostRepository(tx)
	repo := NewCommentRepository(tx)

	postID := insertPublishedPost(t, tx)

	policy, err := repo.PostPolicy(t.Context(), postID)
	require.NoError(t, err)
	require.Equal(t, commentdomain.PolicyOpen, policy)

	post, err := posts.GetByID(t.Context(), postID)
	require.NoError(t, err)
	require.Equal(t, postdomain.CommentPolicyOpen, post.CommentPolicy)

	post.CommentPolicy = postdomain.CommentPolicyDisabled
	post.Version++
	_, err = posts.Update(t.Context(), post)
	require.NoError(t, err)

	policy, err = repo.PostPolicy(t.Context(), postID)
	require.NoError(t, err)
	require.Equal(t, commentdomain.PolicyDisabled, policy)

	draft := createPost(t, posts, postFixture{author: insertUser(t, tx), createdAt: time.Now().UTC()})
	_, err = repo.PostPolicy(t.Context(), draft.UUID)
	require.ErrorIs(t, err, commentdomain.ErrPostNotFound)
}

func TestCommentRepositoryHardDeleteScrubClearsContentHTML(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewCommentRepository(tx)
	post := insertPublishedPost(t, tx)
	author := insertUser(t, tx)

	parent := createComment(t, repo, commentdomain.Comment{
		PostUUID: post, AuthorUUID: &author, Content: "secret", ContentHTML: "<p>secret</p>",
	})
	createComment(t, repo, commentdomain.Comment{
		PostUUID: post, ParentUUID: &parent.UUID, AuthorUUID: &author, Content: "reply", Depth: 1,
	})

	scrubbed, err := repo.HardDelete(t.Context(), parent.UUID, commentdomain.ModerationEntry{
		UUID: uuid.New(), CommentUUID: parent.UUID, Action: commentdomain.ActionHardDelete,
		FromStatus: commentdomain.StatusApproved, ToStatus: commentdomain.StatusDeleted, CreatedAt: time.Now().UTC(),
	})
	require.NoError(t, err)
	require.True(t, scrubbed)

	got, err := repo.GetByID(t.Context(), parent.UUID)
	require.NoError(t, err)
	require.Empty(t, got.Content)
	require.Empty(t, got.ContentHTML)
}
