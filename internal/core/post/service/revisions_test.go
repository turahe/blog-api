package service

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/core/event"
	"github.com/turahe/blog-api/internal/core/event/eventtest"
	mediadomain "github.com/turahe/blog-api/internal/core/media/domain"
	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
	"github.com/turahe/blog-api/internal/core/post/ports"
	tagdomain "github.com/turahe/blog-api/internal/core/tag/domain"
)

type randomIDs struct{}

func (randomIDs) New() uuid.UUID { return uuid.New() }

// memRevisions is an in-memory ports.RevisionRepository; live holds the ids Existing
// reports as still present.
type memRevisions struct {
	byPost map[uuid.UUID][]postdomain.Revision
	live   map[uuid.UUID]bool
}

func newMemRevisions() *memRevisions {
	return &memRevisions{byPost: map[uuid.UUID][]postdomain.Revision{}, live: map[uuid.UUID]bool{}}
}

func (m *memRevisions) Latest(_ context.Context, postID uuid.UUID) (postdomain.Revision, error) {
	revs := m.byPost[postID]
	if len(revs) == 0 {
		return postdomain.Revision{}, postdomain.ErrRevisionNotFound
	}

	return revs[len(revs)-1], nil
}

func (m *memRevisions) Create(_ context.Context, rev postdomain.Revision) (postdomain.Revision, error) {
	if want := len(m.byPost[rev.PostUUID]) + 1; rev.Number != want {
		return postdomain.Revision{}, postdomain.ErrStaleVersion
	}

	m.byPost[rev.PostUUID] = append(m.byPost[rev.PostUUID], rev)

	return rev, nil
}

func (m *memRevisions) List(_ context.Context, filter postdomain.RevisionFilter) (postdomain.RevisionPage, error) {
	items := slices.Clone(m.byPost[filter.PostUUID])
	slices.Reverse(items)

	return postdomain.RevisionPage{Items: items, Total: int64(len(items)), Page: filter.Page, PerPage: filter.PerPage}, nil
}

func (m *memRevisions) Get(_ context.Context, postID uuid.UUID, ref postdomain.RevisionRef) (postdomain.Revision, error) {
	for _, rev := range m.byPost[postID] {
		if (ref.UUID != nil && rev.UUID == *ref.UUID) || (ref.Number != nil && rev.Number == *ref.Number) {
			return rev, nil
		}
	}

	return postdomain.Revision{}, postdomain.ErrRevisionNotFound
}

func (m *memRevisions) Existing(_ context.Context, refs ports.References) (ports.References, error) {
	keep := func(ids []uuid.UUID) []uuid.UUID {
		out := []uuid.UUID{}

		for _, id := range ids {
			if m.live[id] {
				out = append(out, id)
			}
		}

		return out
	}

	return ports.References{Categories: keep(refs.Categories), Tags: keep(refs.Tags), Media: keep(refs.Media)}, nil
}

func (m *memRevisions) Prune(context.Context, int) (int64, error) { return 0, nil }

type fakePostMedia struct {
	items    []mediadomain.PostMediaItem
	replaced []mediadomain.PostMediaItem
}

func (f *fakePostMedia) ReplaceAll(_ context.Context, _ uuid.UUID, items []mediadomain.PostMediaItem) error {
	f.replaced = slices.Clone(items)
	f.items = slices.Clone(items)

	return nil
}

func (f *fakePostMedia) ListByPostID(context.Context, uuid.UUID) ([]mediadomain.PostMediaItem, error) {
	return f.items, nil
}

func revisionService(repo *fakePostRepo, revs *memRevisions, events *eventtest.Recorder) *PostService {
	return New(repo, randomIDs{}, fixedClock{now: lifecycleNow}).WithEvents(events.Unit()).WithRevisions(revs)
}

func TestPostRevisionsRecordEveryWrite(t *testing.T) {
	t.Parallel()

	authorID := uuid.New()
	editorID := uuid.New()
	repo := newFakePostRepo()
	revs := newMemRevisions()
	events := &eventtest.Recorder{}
	svc := revisionService(repo, revs, events)

	post, _, err := svc.CreateDraft(t.Context(), authorID, "First", "", "", "body", nil, nil)
	require.NoError(t, err)

	title := "Second"
	_, _, err = svc.Update(t.Context(), post.UUID, authorID, false, postdomain.UpdateInput{Title: &title})
	require.NoError(t, err)

	editorCtx := postdomain.WithEditor(t.Context(), postdomain.Editor{UserID: &editorID, RequestID: "req-1"})
	_, err = svc.Publish(editorCtx, post.UUID)
	require.NoError(t, err)
	require.NoError(t, svc.Delete(editorCtx, post.UUID))
	_, err = svc.Restore(editorCtx, post.UUID)
	require.NoError(t, err)

	got := revs.byPost[post.UUID]
	require.Len(t, got, 5)

	types := make([]postdomain.RevisionType, 0, len(got))
	for i, rev := range got {
		require.Equal(t, i+1, rev.Number)
		types = append(types, rev.Type)
	}

	require.Equal(t, []postdomain.RevisionType{
		postdomain.RevisionCreate, postdomain.RevisionUpdate, postdomain.RevisionPublish,
		postdomain.RevisionDelete, postdomain.RevisionUndelete,
	}, types)

	require.Equal(t, &authorID, got[0].AuthorUUID)
	require.Equal(t, "Created post", got[0].Changelog)
	require.Equal(t, []string{postdomain.FieldTitle}, got[1].ChangedFields)
	require.Equal(t, "Updated title", got[1].Changelog)
	require.Equal(t, &editorID, got[2].AuthorUUID, "an actorless transition falls back to the context editor")
	require.Equal(t, "req-1", got[2].RequestID)
	require.Equal(t, []string{postdomain.FieldStatus}, got[2].ChangedFields)
	require.Equal(t, "Moved to trash", got[3].Changelog)
	require.Equal(t, postdomain.StatusDraft, got[4].Snapshot.Status)

	var created int

	for _, e := range events.Events() {
		if e.Type == event.PostRevisionCreated {
			created++
		}
	}

	require.Equal(t, 5, created)
}

func TestPostRevisionWriteFailureRollsBackThePostWrite(t *testing.T) {
	t.Parallel()

	postID := uuid.New()
	authorID := uuid.New()
	repo := newFakePostRepo(postdomain.Post{
		UUID: postID, AuthorUUID: authorID, Title: "Old", Slug: "old", Status: postdomain.StatusDraft, Version: 1,
	})
	revs := newMemRevisions()
	revs.byPost[postID] = []postdomain.Revision{{Number: 2}}
	svc := revisionService(repo, revs, &eventtest.Recorder{})

	title := "New"
	_, _, err := svc.Update(t.Context(), postID, authorID, false, postdomain.UpdateInput{Title: &title})

	require.ErrorIs(t, err, postdomain.ErrStaleVersion)
}

func TestPostRestoreRevisionCopiesContentAndSkipsMissingReferences(t *testing.T) {
	t.Parallel()

	authorID := uuid.New()
	liveTag, goneTag := uuid.New(), uuid.New()
	goneCategory := uuid.New()
	liveMedia, goneMedia := uuid.New(), uuid.New()
	postID := uuid.New()

	repo := newFakePostRepo(postdomain.Post{
		UUID: postID, AuthorUUID: authorID, Title: "Current", Slug: "current", Content: "new body",
		Status: postdomain.StatusPublished, CommentPolicy: postdomain.CommentPolicyOpen, Version: 4,
		PublishedAt: &lifecycleNow,
	})
	repo.slugTakenBySlug["taken"] = true

	revs := newMemRevisions()
	revs.live[liveTag] = true
	revs.live[liveMedia] = true
	source := postdomain.Revision{
		UUID: uuid.New(), PostUUID: postID, Number: 1, Type: postdomain.RevisionCreate,
		Snapshot: postdomain.Snapshot{
			Title: "Original", Slug: "taken", Excerpt: "short", Content: "old body",
			Status: postdomain.StatusDraft, CommentPolicy: postdomain.CommentPolicyReadOnly,
			CategoryUUID: &goneCategory, CoverImageMediaUUID: &goneMedia,
			Tags: []postdomain.RevisionTag{{ID: liveTag, Name: "go"}, {ID: goneTag, Name: "gone"}},
			Media: []postdomain.RevisionMedia{
				{MediaAssetID: liveMedia, Kind: "inline_image"},
				{MediaAssetID: goneMedia, Kind: "cover"},
			},
		},
	}
	revs.byPost[postID] = []postdomain.Revision{source}

	tags := &fakeTagLinker{listTags: []tagdomain.Tag{{UUID: liveTag, Name: "go"}}}
	media := &fakePostMedia{}
	events := &eventtest.Recorder{}
	svc := revisionService(repo, revs, events).WithTags(tags)
	svc.postMedia = media

	one := 1
	result, err := svc.RestoreRevision(t.Context(), postID, postdomain.RevisionRef{Number: &one}, authorID, false, "undo")
	require.NoError(t, err)

	require.Equal(t, "Original", result.Post.Title)
	require.Equal(t, "old body", result.Post.Content)
	require.Equal(t, "current", result.Post.Slug, "a slug another post holds is not restored")
	require.Equal(t, postdomain.CommentPolicyReadOnly, result.Post.CommentPolicy)
	require.Equal(t, postdomain.StatusPublished, result.Post.Status, "restore keeps the current status")
	require.Nil(t, result.Post.CategoryUUID)
	require.Nil(t, result.Post.CoverImageMediaUUID)

	require.Equal(t, "taken", result.Skipped.Slug)
	require.Equal(t, []uuid.UUID{goneCategory}, result.Skipped.Categories)
	require.Equal(t, []uuid.UUID{goneTag}, result.Skipped.Tags)
	require.Equal(t, []uuid.UUID{goneMedia}, result.Skipped.Media)

	require.Equal(t, []uuid.UUID{liveTag}, tags.replaceTagIDs)
	require.Len(t, media.replaced, 1)
	require.Equal(t, liveMedia, media.replaced[0].MediaAssetUUID)

	require.Equal(t, postdomain.RevisionRestore, result.Revision.Type)
	require.Equal(t, 2, result.Revision.Number)
	require.Equal(t, &source.UUID, result.Revision.RestoreFromUUID)
	require.Equal(t, "undo", result.Revision.EditorNote)
	require.Equal(t, "Restored revision 1; changed slug, category, cover image, tags and media", result.Revision.Changelog,
		"the diff is against the previous revision, here the source, so only the skipped parts differ")
	require.Equal(t, []string{event.PostRevisionCreated, event.PostRevisionRestored}, events.Types())
}

func TestPostRevisionsHideOtherAuthorsPosts(t *testing.T) {
	t.Parallel()

	postID := uuid.New()
	repo := newFakePostRepo(postdomain.Post{UUID: postID, AuthorUUID: uuid.New(), Title: "T", Slug: "t", Version: 1})
	revs := newMemRevisions()
	revs.byPost[postID] = []postdomain.Revision{{UUID: uuid.New(), PostUUID: postID, Number: 1}}
	svc := revisionService(repo, revs, &eventtest.Recorder{})
	stranger := uuid.New()
	one := 1

	_, err := svc.ListRevisions(t.Context(), stranger, false, postdomain.RevisionFilter{PostUUID: postID})
	require.ErrorIs(t, err, postdomain.ErrNotFound)

	_, err = svc.GetRevision(t.Context(), postID, postdomain.RevisionRef{Number: &one}, stranger, false)
	require.ErrorIs(t, err, postdomain.ErrNotFound)

	_, err = svc.RestoreRevision(t.Context(), postID, postdomain.RevisionRef{Number: &one}, stranger, false, "")
	require.ErrorIs(t, err, postdomain.ErrNotFound)

	page, err := svc.ListRevisions(t.Context(), stranger, true, postdomain.RevisionFilter{PostUUID: postID, PerPage: 500})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Equal(t, 20, page.PerPage)
}

func TestPostListRevisionsRejectsInvertedDateRange(t *testing.T) {
	t.Parallel()

	postID := uuid.New()
	authorID := uuid.New()
	repo := newFakePostRepo(postdomain.Post{UUID: postID, AuthorUUID: authorID, Title: "T", Slug: "t", Version: 1})
	svc := revisionService(repo, newMemRevisions(), &eventtest.Recorder{})
	from := lifecycleNow
	to := lifecycleNow.Add(-time.Hour)

	_, err := svc.ListRevisions(t.Context(), authorID, false, postdomain.RevisionFilter{PostUUID: postID, From: &from, To: &to})
	require.ErrorIs(t, err, ErrValidation)
}
