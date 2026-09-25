package persistence

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/core/event"
	"github.com/turahe/blog-api/internal/core/event/eventtest"
	mediadomain "github.com/turahe/blog-api/internal/core/media/domain"
	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
	"github.com/turahe/blog-api/internal/core/post/ports"
	postservice "github.com/turahe/blog-api/internal/core/post/service"
	tagservice "github.com/turahe/blog-api/internal/core/tag/service"
	"github.com/turahe/blog-api/internal/platform/system"
	"gorm.io/gorm"
)

func revisionPosts(tx *gorm.DB) (*postservice.PostService, *PostRevisionRepository) {
	revs := NewPostRevisionRepository(tx)
	events := event.Unit{Tx: NewTransactor(tx), Recorder: &eventtest.Recorder{}}
	posts := postservice.New(NewPostRepository(tx), system.UUIDGenerator{}, system.Clock{}).
		WithEvents(events).
		WithTags(tagservice.New(NewTagRepository(tx), system.UUIDGenerator{}, system.Clock{})).
		WithMedia(NewPostMediaRepository(tx), NewMediaRepository(tx)).
		WithRevisions(revs)

	return posts, revs
}

func insertReadyMedia(t *testing.T, tx *gorm.DB) uuid.UUID {
	t.Helper()

	return insertUUID(t, tx,
		`INSERT INTO media_assets (storage_key, original_filename, content_type, disk, status)
		 VALUES (?, 'a.png', 'image/png', 'minio', 'ready') RETURNING uuid`, uniqueSlug("media/rev"))
}

func TestPostRevisionsCaptureRestoreAndPrune(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	ctx := t.Context()
	posts, revs := revisionPosts(tx)
	author := insertUser(t, tx)
	category := insertCategory(t, tx)
	media := insertReadyMedia(t, tx)

	editorCtx := postdomain.WithEditor(ctx, postdomain.Editor{UserID: &author, RequestID: "req-rev"})
	tags := []string{"rev-" + uuid.NewString()[:8]}
	post, _, err := posts.CreateDraft(editorCtx, author, "Draft one", "", "intro", "first body", &category, &tags)
	require.NoError(t, err)

	_, err = posts.ReplaceMedia(editorCtx, post.UUID, []mediadomain.PostMediaItem{
		{MediaAssetUUID: media, Kind: mediadomain.KindCover},
	}, true)
	require.NoError(t, err)

	title, content, noTags := "Draft two", "second body", []string{}
	_, _, err = posts.Update(editorCtx, post.UUID, author, false, postdomain.UpdateInput{
		Title: &title, Content: &content, Tags: &noTags, CategoryUUID: postdomain.OptionalCategoryID{Present: true},
	})
	require.NoError(t, err)

	first, err := revs.Get(ctx, post.UUID, postdomain.RevisionRef{Number: new(1)})
	require.NoError(t, err)
	require.Equal(t, postdomain.RevisionCreate, first.Type)
	require.Equal(t, "first body", first.Snapshot.Content)
	require.Equal(t, &category, first.Snapshot.CategoryUUID)
	require.Len(t, first.Snapshot.Tags, 1)
	require.Equal(t, &author, first.AuthorUUID)
	require.Equal(t, "req-rev", first.RequestID)

	second, err := revs.Get(ctx, post.UUID, postdomain.RevisionRef{UUID: new(uuid.UUID)})
	require.ErrorIs(t, err, postdomain.ErrRevisionNotFound)
	require.Zero(t, second.Number)

	third, err := revs.Latest(ctx, post.UUID)
	require.NoError(t, err)
	require.Equal(t, 3, third.Number)
	require.Equal(t, []string{
		postdomain.FieldTitle, postdomain.FieldContent, postdomain.FieldCategory, postdomain.FieldTags,
	}, third.ChangedFields)
	require.Len(t, third.Snapshot.Media, 1, "the media set in revision 2 carries into later snapshots")

	one := 1
	result, err := posts.RestoreRevision(editorCtx, post.UUID, postdomain.RevisionRef{Number: &one}, author, false, "back to one")
	require.NoError(t, err)
	require.Equal(t, "Draft one", result.Post.Title)
	require.Equal(t, "first body", result.Post.Content)
	require.Equal(t, &category, result.Post.CategoryUUID)
	require.Len(t, result.Tags, 1)
	require.Empty(t, result.Skipped.Tags)
	require.Equal(t, 4, result.Revision.Number)

	restored, err := revs.Get(ctx, post.UUID, postdomain.RevisionRef{UUID: &result.Revision.UUID})
	require.NoError(t, err)
	require.Equal(t, &first.UUID, restored.RestoreFromUUID)
	require.Equal(t, &one, restored.RestoreFromNumber)
	require.Equal(t, "back to one", restored.EditorNote)
	require.Empty(t, restored.Snapshot.Media, "revision 1 had no media, so the restore cleared it")

	from := time.Now().Add(-time.Hour)
	page, err := revs.List(ctx, postdomain.RevisionFilter{PostUUID: post.UUID, AuthorUUID: &author, From: &from, Page: 1, PerPage: 2})
	require.NoError(t, err)
	require.EqualValues(t, 4, page.Total)
	require.Len(t, page.Items, 2)
	require.Equal(t, 4, page.Items[0].Number)
	require.Empty(t, page.Items[0].Snapshot.Content, "lists skip the full text")

	existing, err := revs.Existing(ctx, ports.References{
		Categories: []uuid.UUID{category, uuid.New()}, Tags: []uuid.UUID{uuid.New()}, Media: []uuid.UUID{media},
	})
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{category}, existing.Categories)
	require.Empty(t, existing.Tags)
	require.Equal(t, []uuid.UUID{media}, existing.Media)

	deleted, err := revs.Prune(ctx, 2)
	require.NoError(t, err)
	require.GreaterOrEqual(t, deleted, int64(2))

	page, err = revs.List(ctx, postdomain.RevisionFilter{PostUUID: post.UUID, Page: 1, PerPage: 10})
	require.NoError(t, err)
	require.EqualValues(t, 2, page.Total)
	require.Nil(t, page.Items[0].RestoreFromNumber, "pruning the source clears the restore link")
}
