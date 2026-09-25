package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/core/audit"
	"github.com/turahe/blog-api/internal/core/event"
	mediadomain "github.com/turahe/blog-api/internal/core/media/domain"
	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
	"github.com/turahe/blog-api/internal/core/post/ports"
	"github.com/turahe/blog-api/internal/core/readcache"
)

// SEOView is a post's stored SEO with its slug and advisory warnings.
type SEOView struct {
	PostUUID uuid.UUID
	Slug     string
	SEO      postdomain.SEO
	Warnings []postdomain.FieldViolation
}

// SEODraft is an unsaved SEO edit to preview: SEO changes plus optional unsaved post
// title and excerpt.
type SEODraft struct {
	Patch   postdomain.SEOPatch
	Title   *string
	Excerpt *string
}

// WithSEO enables per-post SEO: storage, site defaults for rendering, and image URLs
// for social cards. Revisions then include the SEO snapshot.
func (s *PostService) WithSEO(repo ports.SEORepository, defaults ports.SEODefaultsSource, images ports.ImageURLs) *PostService {
	s.seo = repo
	s.seoDefaults = defaults
	s.images = images

	return s
}

// GetSEO returns the post's SEO; callers without unrestricted access only reach their
// own posts.
func (s *PostService) GetSEO(ctx context.Context, postID, viewerID uuid.UUID, unrestricted bool) (SEOView, error) {
	if s.seo == nil {
		return SEOView{}, fmt.Errorf("%w: seo not configured", ErrValidation)
	}

	post, err := s.editablePost(ctx, postID, viewerID, unrestricted)
	if err != nil {
		return SEOView{}, err
	}

	seo, err := s.seo.Get(ctx, post.UUID)
	if err != nil {
		return SEOView{}, err
	}

	return SEOView{PostUUID: post.UUID, Slug: post.Slug, SEO: seo, Warnings: seo.Warnings()}, nil
}

// UpdateSEO applies patch to the post's SEO and, when patch.Slug differs, renames the
// post. slugAllowed is whether the caller holds post.slug.edit. Every invalid field is
// reported at once in a *postdomain.SEOValidationError.
func (s *PostService) UpdateSEO(
	ctx context.Context, postID, actorID uuid.UUID, unrestricted, slugAllowed bool, patch postdomain.SEOPatch,
) (SEOView, error) {
	if s.seo == nil {
		return SEOView{}, fmt.Errorf("%w: seo not configured", ErrValidation)
	}

	post, err := s.editablePost(ctx, postID, actorID, unrestricted)
	if err != nil {
		return SEOView{}, err
	}

	current, err := s.seo.Get(ctx, post.UUID)
	if err != nil {
		return SEOView{}, err
	}

	next := current.Apply(patch)

	newSlug, err := seoSlug(post, patch.Slug, slugAllowed)
	if err != nil {
		return SEOView{}, err
	}

	if err := s.validateSEO(ctx, post.UUID, current, next, newSlug); err != nil {
		return SEOView{}, err
	}

	fields := postdomain.SEOFieldChanges(current, next)
	slugChanged := newSlug != "" && newSlug != post.Slug

	if len(fields) == 0 && !slugChanged {
		return SEOView{PostUUID: post.UUID, Slug: post.Slug, SEO: current, Warnings: current.Warnings()}, nil
	}

	oldSlug := post.Slug

	post, err = s.commitSEO(ctx, post, next, newSlug, actorID, fields)
	if err != nil {
		return SEOView{}, err
	}

	audit.AddMetadata(ctx, "changed_fields", fields)

	if slugChanged {
		audit.AddChange(ctx, "slug", oldSlug, post.Slug)
	}

	s.invalidate(ctx)

	return SEOView{PostUUID: post.UUID, Slug: post.Slug, SEO: next, Warnings: next.Warnings()}, nil
}

// commitSEO stores the change and records its events in one transaction.
func (s *PostService) commitSEO(
	ctx context.Context, post postdomain.Post, seo postdomain.SEO, newSlug string, actorID uuid.UUID, fields []string,
) (postdomain.Post, error) {
	oldSlug := post.Slug

	err := s.events.InTx(ctx, func(ctx context.Context) error {
		var err error

		post, err = s.storeSEO(ctx, post, seo, newSlug, actorID)
		if err != nil {
			return err
		}

		if len(fields) > 0 {
			if err := s.events.Record(ctx, seoUpdatedEvent(post, actorID, fields, s.clock.Now())); err != nil {
				return err
			}
		}

		if post.Slug != oldSlug {
			return s.events.Record(ctx, slugChangedEvent(post, oldSlug, actorID, s.clock.Now()))
		}

		return nil
	})

	return post, err
}

// storeSEO renames the post when newSlug is set, saves seo, and records a revision.
func (s *PostService) storeSEO(
	ctx context.Context, post postdomain.Post, seo postdomain.SEO, newSlug string, actorID uuid.UUID,
) (postdomain.Post, error) {
	now := s.clock.Now()

	if newSlug != "" && newSlug != post.Slug {
		post.Slug = newSlug
		post.UpdatedAt = now
		post.Version++

		updated, err := s.repo.Update(ctx, post)
		if err != nil {
			return postdomain.Post{}, err
		}

		post = updated
	}

	if err := s.seo.Save(ctx, post.UUID, seo, now); err != nil {
		return postdomain.Post{}, err
	}

	if _, err := s.revise(ctx, post, postdomain.RevisionUpdate, &actorID, nil); err != nil {
		return postdomain.Post{}, err
	}

	return post, nil
}

// seoSlug returns the normalised requested slug, or "" when the slug is not changing.
func seoSlug(post postdomain.Post, raw *string, allowed bool) (string, error) {
	if raw == nil {
		return "", nil
	}

	slug := strings.ToLower(strings.TrimSpace(*raw))
	if slug == post.Slug {
		return "", nil
	}

	if !allowed {
		return "", postdomain.ErrSlugEditForbidden
	}

	return slug, nil
}

// validateSEO checks next's fields, its new images, and newSlug, returning every
// violation in one *postdomain.SEOValidationError.
func (s *PostService) validateSEO(ctx context.Context, postID uuid.UUID, current, next postdomain.SEO, newSlug string) error {
	defaults, err := s.defaults(ctx)
	if err != nil {
		return err
	}

	violations := next.Validate(defaults)

	images := []struct {
		field     string
		now, then *uuid.UUID
	}{
		{"og_image_id", next.OGImageUUID, current.OGImageUUID},
		{"twitter_image_id", next.TwitterImageUUID, current.TwitterImageUUID},
	}
	for _, img := range images {
		if img.now == nil || sameUUID(img.now, img.then) {
			continue
		}

		v, err := s.checkImage(ctx, img.field, *img.now)
		if err != nil {
			return err
		}

		if v != nil {
			violations = append(violations, *v)
		}
	}

	if newSlug != "" {
		if v := postdomain.CheckSEOSlug(newSlug); v != nil {
			violations = append(violations, *v)
		} else if taken, err := s.repo.SlugTaken(ctx, newSlug, postID); err != nil {
			return err
		} else if taken {
			violations = append(violations, postdomain.FieldViolation{
				Field: "slug", Code: postdomain.SEOCodeSlugTaken, Message: "another post already uses this slug",
			})
		}
	}

	if len(violations) > 0 {
		return &postdomain.SEOValidationError{Violations: violations}
	}

	return nil
}

func (s *PostService) checkImage(ctx context.Context, field string, id uuid.UUID) (*postdomain.FieldViolation, error) {
	if s.media == nil {
		return nil, nil
	}

	asset, err := s.media.GetByID(ctx, id)
	if errors.Is(err, mediadomain.ErrNotFound) {
		return &postdomain.FieldViolation{Field: field, Code: postdomain.SEOCodeNotFound, Message: "media not found"}, nil
	}

	if err != nil {
		return nil, err
	}

	if asset.DeletedAt != nil || asset.Status != mediadomain.StatusReady || !strings.HasPrefix(asset.ContentType, "image/") {
		return &postdomain.FieldViolation{Field: field, Code: postdomain.SEOCodeNotImage, Message: "media must be a ready image"}, nil
	}

	return nil, nil
}

// PreviewSEO renders how the post would look in search and social cards with draft
// applied, without saving anything.
func (s *PostService) PreviewSEO(
	ctx context.Context, postID, viewerID uuid.UUID, unrestricted bool, draft SEODraft,
) (postdomain.SEOPreview, error) {
	if s.seo == nil {
		return postdomain.SEOPreview{}, fmt.Errorf("%w: seo not configured", ErrValidation)
	}

	post, err := s.editablePost(ctx, postID, viewerID, unrestricted)
	if err != nil {
		return postdomain.SEOPreview{}, err
	}

	current, err := s.seo.Get(ctx, post.UUID)
	if err != nil {
		return postdomain.SEOPreview{}, err
	}

	next := current.Apply(draft.Patch)

	if draft.Title != nil {
		post.Title = *draft.Title
	}

	if draft.Excerpt != nil {
		post.Excerpt = *draft.Excerpt
	}

	if draft.Patch.Slug != nil {
		post.Slug = strings.ToLower(strings.TrimSpace(*draft.Patch.Slug))
	}

	defaults, err := s.defaults(ctx)
	if err != nil {
		return postdomain.SEOPreview{}, err
	}

	if violations := next.Validate(defaults); len(violations) > 0 {
		return postdomain.SEOPreview{}, &postdomain.SEOValidationError{Violations: violations}
	}

	meta, err := s.render(ctx, post, next, defaults)
	if err != nil {
		return postdomain.SEOPreview{}, err
	}

	return postdomain.PreviewOf(meta, next.Warnings()), nil
}

// SEOMeta returns the rendered meta of the published post with slug; other posts are
// ErrNotFound. Results are cached with the other public post reads.
func (s *PostService) SEOMeta(ctx context.Context, slug string) (postdomain.SEOMeta, error) {
	if s.seo == nil {
		return postdomain.SEOMeta{}, fmt.Errorf("%w: seo not configured", ErrValidation)
	}

	slug = strings.TrimSpace(slug)
	if slug == "" {
		return postdomain.SEOMeta{}, postdomain.ErrNotFound
	}

	return readcache.Through(ctx, s.cache, readcache.Posts, readcache.Key("seo", "slug", slug), func() (postdomain.SEOMeta, error) {
		post, err := s.repo.GetPublishedBySlug(ctx, slug)
		if err != nil {
			return postdomain.SEOMeta{}, err
		}

		seo, err := s.seo.Get(ctx, post.UUID)
		if err != nil {
			return postdomain.SEOMeta{}, err
		}

		defaults, err := s.defaults(ctx)
		if err != nil {
			return postdomain.SEOMeta{}, err
		}

		return s.render(ctx, post, seo, defaults)
	})
}

func (s *PostService) render(
	ctx context.Context, post postdomain.Post, seo postdomain.SEO, defaults postdomain.SEODefaults,
) (postdomain.SEOMeta, error) {
	in := postdomain.SEOInput{Post: post, SEO: seo, Defaults: defaults}

	if s.tags != nil {
		tags, err := s.tags.ListByPostID(ctx, post.UUID)
		if err != nil {
			return postdomain.SEOMeta{}, err
		}

		for _, tag := range tags {
			in.Tags = append(in.Tags, tag.Name)
		}
	}

	var err error

	if in.OGImageURL, err = s.imageURL(ctx, seo.OGImageUUID); err != nil {
		return postdomain.SEOMeta{}, err
	}

	if in.TwitterImageURL, err = s.imageURL(ctx, seo.TwitterImageUUID); err != nil {
		return postdomain.SEOMeta{}, err
	}

	if in.CoverImageURL, err = s.imageURL(ctx, post.CoverImageMediaUUID); err != nil {
		return postdomain.SEOMeta{}, err
	}

	return postdomain.RenderSEO(in), nil
}

func (s *PostService) imageURL(ctx context.Context, id *uuid.UUID) (string, error) {
	if id == nil || s.images == nil {
		return "", nil
	}

	return s.images.ImageURL(ctx, *id)
}

func (s *PostService) defaults(ctx context.Context) (postdomain.SEODefaults, error) {
	if s.seoDefaults == nil {
		return postdomain.SEODefaults{TitleTemplate: "{title}", TwitterCard: postdomain.TwitterSummaryLarge}, nil
	}

	return s.seoDefaults.SEODefaults(ctx)
}

func sameUUID(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == b
	}

	return *a == *b
}

// seoUpdatedPayload is PostSEOUpdated in docs/architecture/asyncapi.yaml.
type seoUpdatedPayload struct {
	PostID        uuid.UUID `json:"post_id"`
	ActorID       uuid.UUID `json:"actor_id"`
	ChangedFields []string  `json:"changed_fields"`
}

// slugChangedPayload is PostSlugChanged in docs/architecture/asyncapi.yaml.
type slugChangedPayload struct {
	PostID  uuid.UUID `json:"post_id"`
	OldSlug string    `json:"old_slug"`
	NewSlug string    `json:"new_slug"`
	ActorID uuid.UUID `json:"actor_id"`
}

func seoUpdatedEvent(post postdomain.Post, actorID uuid.UUID, fields []string, at time.Time) event.Event {
	return event.New(event.PostSEOUpdated, event.AggregatePost, post.UUID, &actorID, at,
		seoUpdatedPayload{PostID: post.UUID, ActorID: actorID, ChangedFields: fields})
}

func slugChangedEvent(post postdomain.Post, oldSlug string, actorID uuid.UUID, at time.Time) event.Event {
	return event.New(event.PostSlugChanged, event.AggregatePost, post.UUID, &actorID, at,
		slugChangedPayload{PostID: post.UUID, OldSlug: oldSlug, NewSlug: post.Slug, ActorID: actorID})
}
