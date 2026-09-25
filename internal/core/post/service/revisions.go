package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/core/audit"
	"github.com/turahe/blog-api/internal/core/event"
	mediadomain "github.com/turahe/blog-api/internal/core/media/domain"
	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
	"github.com/turahe/blog-api/internal/core/post/ports"
	tagdomain "github.com/turahe/blog-api/internal/core/tag/domain"
)

// MaxRestoreNoteLength caps the note stored with a restore revision.
const MaxRestoreNoteLength = 1000

// RestoreSkips lists what a restore could not bring back because it no longer exists
// or is now taken.
type RestoreSkips struct {
	// Slug is the revision's slug when another live post holds it; the current slug stays.
	Slug       string      `json:"slug,omitempty"`
	Categories []uuid.UUID `json:"category_ids"`
	Tags       []uuid.UUID `json:"tag_ids"`
	Media      []uuid.UUID `json:"media_asset_ids"`
}

// RestoreResult is the outcome of restoring a revision.
type RestoreResult struct {
	Post     postdomain.Post
	Tags     []tagdomain.Tag
	Revision postdomain.Revision
	Skipped  RestoreSkips
}

// revisionSource is the revision a restore copies.
type revisionSource struct {
	revision postdomain.Revision
	note     string
}

// WithRevisions records a revision of every post write in the write's transaction.
func (s *PostService) WithRevisions(revisions ports.RevisionRepository) *PostService {
	s.revisions = revisions
	return s
}

// ListRevisions returns a page of the post's revisions, newest first. Callers without
// unrestricted access only see revisions of their own posts.
func (s *PostService) ListRevisions(
	ctx context.Context, viewerID uuid.UUID, unrestricted bool, filter postdomain.RevisionFilter,
) (postdomain.RevisionPage, error) {
	if s.revisions == nil {
		return postdomain.RevisionPage{}, fmt.Errorf("%w: revisions not configured", ErrValidation)
	}

	if _, err := s.editablePost(ctx, filter.PostUUID, viewerID, unrestricted); err != nil {
		return postdomain.RevisionPage{}, err
	}

	if filter.Page < 1 {
		filter.Page = 1
	}

	if filter.PerPage < 1 || filter.PerPage > 100 {
		filter.PerPage = 20
	}

	if filter.From != nil && filter.To != nil && filter.From.After(*filter.To) {
		return postdomain.RevisionPage{}, fmt.Errorf("%w: from_date must not be after to_date", ErrValidation)
	}

	return s.revisions.List(ctx, filter)
}

// GetRevision returns one revision of the post with its full snapshot and diff.
func (s *PostService) GetRevision(
	ctx context.Context, postID uuid.UUID, ref postdomain.RevisionRef, viewerID uuid.UUID, unrestricted bool,
) (postdomain.Revision, error) {
	if s.revisions == nil {
		return postdomain.Revision{}, fmt.Errorf("%w: revisions not configured", ErrValidation)
	}

	if _, err := s.editablePost(ctx, postID, viewerID, unrestricted); err != nil {
		return postdomain.Revision{}, err
	}

	return s.revisions.Get(ctx, postID, ref)
}

// RestoreRevision copies a revision's content, meta, tags, and media back onto the post
// as a new restore revision. The post's status and published_at stay as they are, and
// references that no longer exist are skipped and reported.
func (s *PostService) RestoreRevision(
	ctx context.Context, postID uuid.UUID, ref postdomain.RevisionRef, actorID uuid.UUID, unrestricted bool, note string,
) (RestoreResult, error) {
	if s.revisions == nil {
		return RestoreResult{}, fmt.Errorf("%w: revisions not configured", ErrValidation)
	}

	if len(note) > MaxRestoreNoteLength {
		return RestoreResult{}, fmt.Errorf("%w: restore_note must be at most %d characters", ErrValidation, MaxRestoreNoteLength)
	}

	post, err := s.editablePost(ctx, postID, actorID, unrestricted)
	if err != nil {
		return RestoreResult{}, err
	}

	source, err := s.revisions.Get(ctx, postID, ref)
	if err != nil {
		return RestoreResult{}, err
	}

	snap, skipped, err := s.restorable(ctx, post, source.Snapshot)
	if err != nil {
		return RestoreResult{}, err
	}

	post.Title = snap.Title
	post.Slug = snap.Slug
	post.Excerpt = snap.Excerpt
	post.Content = snap.Content
	post.CommentPolicy = snap.CommentPolicy
	post.CategoryUUID = snap.CategoryUUID
	post.CoverImageMediaUUID = snap.CoverImageMediaUUID
	post.UpdatedAt = s.clock.Now()
	post.Version++

	result := RestoreResult{Skipped: skipped}

	err = s.events.InTx(ctx, func(ctx context.Context) error {
		var werr error

		result.Post, result.Tags, result.Revision, werr = s.storeRestore(ctx, post, snap, actorID, revisionSource{source, note})

		return werr
	})
	if err != nil {
		return RestoreResult{}, err
	}

	audit.AddMetadata(ctx, "restored_revision", source.Number)
	audit.AddMetadata(ctx, "new_revision", result.Revision.Number)
	s.invalidate(ctx)

	return result, nil
}

func (s *PostService) storeRestore(
	ctx context.Context, post postdomain.Post, snap postdomain.Snapshot, actorID uuid.UUID, source revisionSource,
) (postdomain.Post, []tagdomain.Tag, postdomain.Revision, error) {
	updated, err := s.repo.Update(ctx, post)
	if err != nil {
		return postdomain.Post{}, nil, postdomain.Revision{}, err
	}

	var tags []tagdomain.Tag

	if s.tags != nil {
		ids := make([]uuid.UUID, 0, len(snap.Tags))
		for _, tag := range snap.Tags {
			ids = append(ids, tag.ID)
		}

		if err := s.tags.ReplacePostTags(ctx, updated.UUID, ids); err != nil {
			return postdomain.Post{}, nil, postdomain.Revision{}, err
		}

		if tags, err = s.tags.ListByPostID(ctx, updated.UUID); err != nil {
			return postdomain.Post{}, nil, postdomain.Revision{}, err
		}
	}

	if s.postMedia != nil {
		items := make([]mediadomain.PostMediaItem, 0, len(snap.Media))
		for _, m := range snap.Media {
			items = append(items, mediadomain.PostMediaItem{MediaAssetUUID: m.MediaAssetID, Kind: m.Kind, SortOrder: m.SortOrder})
		}

		if err := s.postMedia.ReplaceAll(ctx, updated.UUID, items); err != nil {
			return postdomain.Post{}, nil, postdomain.Revision{}, err
		}
	}

	if err := s.restoreSEO(ctx, updated, snap.SEO); err != nil {
		return postdomain.Post{}, nil, postdomain.Revision{}, err
	}

	rev, err := s.revise(ctx, updated, postdomain.RevisionRestore, &actorID, &source)
	if err != nil {
		return postdomain.Post{}, nil, postdomain.Revision{}, err
	}

	return updated, tags, rev, nil
}

// restoreSEO saves the snapshot's SEO onto post; snapshots taken without SEO support
// leave the current SEO alone.
func (s *PostService) restoreSEO(ctx context.Context, post postdomain.Post, raw json.RawMessage) error {
	if s.seo == nil || len(raw) == 0 {
		return nil
	}

	seo, err := postdomain.DecodeSEO(raw)
	if err != nil {
		return err
	}

	return s.seo.Save(ctx, post.UUID, seo, post.UpdatedAt)
}

// restorable returns snap without the references that no longer exist, keeping post's
// slug when another live post took the revision's slug.
func (s *PostService) restorable(
	ctx context.Context, post postdomain.Post, snap postdomain.Snapshot,
) (postdomain.Snapshot, RestoreSkips, error) {
	skipped := RestoreSkips{Categories: []uuid.UUID{}, Tags: []uuid.UUID{}, Media: []uuid.UUID{}}

	if snap.Slug != post.Slug {
		taken, err := s.repo.SlugTaken(ctx, snap.Slug, post.UUID)
		if err != nil {
			return postdomain.Snapshot{}, RestoreSkips{}, err
		}

		if taken {
			skipped.Slug = snap.Slug
			snap.Slug = post.Slug
		}
	}

	existing, err := s.revisions.Existing(ctx, snapshotRefs(snap))
	if err != nil {
		return postdomain.Snapshot{}, RestoreSkips{}, err
	}

	return dropMissing(snap, existing, &skipped), skipped, nil
}

// snapshotRefs lists the categories, tags, and media snap points at.
func snapshotRefs(snap postdomain.Snapshot) ports.References {
	refs := ports.References{}
	if snap.CategoryUUID != nil {
		refs.Categories = []uuid.UUID{*snap.CategoryUUID}
	}

	for _, tag := range snap.Tags {
		refs.Tags = append(refs.Tags, tag.ID)
	}

	for _, m := range snap.Media {
		refs.Media = append(refs.Media, m.MediaAssetID)
	}

	if snap.CoverImageMediaUUID != nil {
		refs.Media = append(refs.Media, *snap.CoverImageMediaUUID)
	}

	if seo, err := postdomain.DecodeSEO(snap.SEO); err == nil {
		for _, id := range []*uuid.UUID{seo.OGImageUUID, seo.TwitterImageUUID} {
			if id != nil {
				refs.Media = append(refs.Media, *id)
			}
		}
	}

	return refs
}

// dropMissing removes from snap the references absent from existing, noting each in skipped.
func dropMissing(snap postdomain.Snapshot, existing ports.References, skipped *RestoreSkips) postdomain.Snapshot {
	skipMedia := func(id uuid.UUID) {
		if !slices.Contains(skipped.Media, id) {
			skipped.Media = append(skipped.Media, id)
		}
	}

	if snap.CategoryUUID != nil && !slices.Contains(existing.Categories, *snap.CategoryUUID) {
		skipped.Categories = append(skipped.Categories, *snap.CategoryUUID)
		snap.CategoryUUID = nil
	}

	snap.Tags = slices.DeleteFunc(slices.Clone(snap.Tags), func(tag postdomain.RevisionTag) bool {
		gone := !slices.Contains(existing.Tags, tag.ID)
		if gone {
			skipped.Tags = append(skipped.Tags, tag.ID)
		}

		return gone
	})

	snap.Media = slices.DeleteFunc(slices.Clone(snap.Media), func(m postdomain.RevisionMedia) bool {
		gone := !slices.Contains(existing.Media, m.MediaAssetID)
		if gone {
			skipMedia(m.MediaAssetID)
		}

		return gone
	})

	if snap.CoverImageMediaUUID != nil && !slices.Contains(existing.Media, *snap.CoverImageMediaUUID) {
		skipMedia(*snap.CoverImageMediaUUID)
		snap.CoverImageMediaUUID = nil
	}

	snap.SEO = dropMissingSEO(snap.SEO, existing.Media, skipMedia)

	return snap
}

// dropMissingSEO clears the snapshot SEO's social images absent from media.
func dropMissingSEO(raw json.RawMessage, media []uuid.UUID, skip func(uuid.UUID)) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}

	seo, err := postdomain.DecodeSEO(raw)
	if err != nil {
		return raw
	}

	for _, id := range []**uuid.UUID{&seo.OGImageUUID, &seo.TwitterImageUUID} {
		if *id != nil && !slices.Contains(media, **id) {
			skip(**id)
			*id = nil
		}
	}

	return postdomain.EncodeSEO(seo)
}

// revise appends a revision of post to its history and records its events. Call it in
// the write's transaction, after the post row was written: that row lock orders
// concurrent writers, so the revision number read here is the latest.
func (s *PostService) revise(
	ctx context.Context, post postdomain.Post, typ postdomain.RevisionType, actorID *uuid.UUID, source *revisionSource,
) (postdomain.Revision, error) {
	if s.revisions == nil {
		return postdomain.Revision{}, nil
	}

	snap, err := s.snapshot(ctx, post)
	if err != nil {
		return postdomain.Revision{}, err
	}

	editor := postdomain.EditorFrom(ctx)
	if actorID == nil {
		actorID = editor.UserID
	}

	rev := postdomain.Revision{
		UUID:       s.ids.New(),
		PostUUID:   post.UUID,
		Number:     1,
		Type:       typ,
		Snapshot:   snap,
		Diff:       map[string]any{},
		AuthorUUID: actorID,
		RequestID:  editor.RequestID,
		CreatedAt:  s.clock.Now(),
	}

	prev, err := s.revisions.Latest(ctx, post.UUID)

	switch {
	case errors.Is(err, postdomain.ErrRevisionNotFound):
	case err != nil:
		return postdomain.Revision{}, err
	default:
		rev.Number = prev.Number + 1
		rev.ChangedFields, rev.Diff = postdomain.DiffSnapshots(prev.Snapshot, snap)
	}

	if source != nil {
		rev.RestoreFromUUID = &source.revision.UUID
		rev.RestoreFromNumber = &source.revision.Number
		rev.EditorNote = source.note
	}

	rev.Changelog = postdomain.Changelog(typ, rev.ChangedFields, rev.RestoreFromNumber)

	rev, err = s.revisions.Create(ctx, rev)
	if err != nil {
		return postdomain.Revision{}, err
	}

	if err := s.events.Record(ctx, revisionCreatedEvent(rev)); err != nil {
		return postdomain.Revision{}, err
	}

	if source != nil {
		return rev, s.events.Record(ctx, revisionRestoredEvent(rev))
	}

	return rev, nil
}

// snapshot captures post with its current tags and media.
func (s *PostService) snapshot(ctx context.Context, post postdomain.Post) (postdomain.Snapshot, error) {
	tags := []postdomain.RevisionTag{}

	if s.tags != nil {
		linked, err := s.tags.ListByPostID(ctx, post.UUID)
		if err != nil {
			return postdomain.Snapshot{}, err
		}

		for _, tag := range linked {
			tags = append(tags, postdomain.RevisionTag{ID: tag.UUID, Name: tag.Name})
		}
	}

	media := []postdomain.RevisionMedia{}

	if s.postMedia != nil {
		items, err := s.postMedia.ListByPostID(ctx, post.UUID)
		if err != nil {
			return postdomain.Snapshot{}, err
		}

		for _, item := range items {
			media = append(media, postdomain.RevisionMedia{MediaAssetID: item.MediaAssetUUID, Kind: item.Kind, SortOrder: item.SortOrder})
		}
	}

	snap := postdomain.SnapshotOf(post, tags, media)

	if s.seo != nil {
		seo, err := s.seo.Get(ctx, post.UUID)
		if err != nil {
			return postdomain.Snapshot{}, err
		}

		snap.SEO = postdomain.EncodeSEO(seo)
	}

	return snap, nil
}

// revisionType names the revision a transition produces.
func revisionType(transition postdomain.Transition) postdomain.RevisionType {
	switch transition {
	case postdomain.TransitionPublish:
		return postdomain.RevisionPublish
	case postdomain.TransitionUnpublish:
		return postdomain.RevisionUnpublish
	case postdomain.TransitionArchive:
		return postdomain.RevisionArchive
	}

	return postdomain.RevisionUpdate
}

// revisionCreatedPayload is PostRevisionCreated in docs/architecture/asyncapi.yaml.
type revisionCreatedPayload struct {
	PostID                uuid.UUID  `json:"post_id"`
	RevisionID            uuid.UUID  `json:"revision_id"`
	RevisionNumber        int        `json:"revision_number"`
	RevisionType          string     `json:"revision_type"`
	AuthorID              *uuid.UUID `json:"author_id"`
	RestoreFromRevisionID *uuid.UUID `json:"restore_from_revision_id"`
	ChangedFields         []string   `json:"changed_fields"`
}

// revisionRestoredPayload is PostRevisionRestored in docs/architecture/asyncapi.yaml.
type revisionRestoredPayload struct {
	PostID                 uuid.UUID  `json:"post_id"`
	RestoredFromRevisionID uuid.UUID  `json:"restored_from_revision_id"`
	NewRevisionID          uuid.UUID  `json:"new_revision_id"`
	ActorID                *uuid.UUID `json:"actor_id"`
}

func revisionCreatedEvent(rev postdomain.Revision) event.Event {
	fields := rev.ChangedFields
	if fields == nil {
		fields = []string{}
	}

	return event.New(event.PostRevisionCreated, event.AggregatePost, rev.PostUUID, rev.AuthorUUID, rev.CreatedAt, revisionCreatedPayload{
		PostID: rev.PostUUID, RevisionID: rev.UUID, RevisionNumber: rev.Number, RevisionType: string(rev.Type),
		AuthorID: rev.AuthorUUID, RestoreFromRevisionID: rev.RestoreFromUUID, ChangedFields: fields,
	})
}

func revisionRestoredEvent(rev postdomain.Revision) event.Event {
	return event.New(event.PostRevisionRestored, event.AggregatePost, rev.PostUUID, rev.AuthorUUID, rev.CreatedAt, revisionRestoredPayload{
		PostID: rev.PostUUID, RestoredFromRevisionID: *rev.RestoreFromUUID, NewRevisionID: rev.UUID, ActorID: rev.AuthorUUID,
	})
}
