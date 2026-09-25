// Package service implements post drafting, publishing, editing, and media/tag associations.
package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	mediadomain "github.com/turahe/blog-api/internal/core/media/domain"
	mediaports "github.com/turahe/blog-api/internal/core/media/ports"
	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
	"github.com/turahe/blog-api/internal/core/post/ports"
	"github.com/turahe/blog-api/internal/core/readcache"
	tagdomain "github.com/turahe/blog-api/internal/core/tag/domain"
)

// Post service errors.
var (
	ErrValidation = postdomain.ErrValidation
	ErrConflict   = postdomain.ErrConflict
	slugPattern   = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
)

// IDGenerator returns new UUIDs.
type IDGenerator interface {
	New() uuid.UUID
}

// Clock returns the current time.
type Clock interface {
	Now() time.Time
}

// PostService implements ports.Service.
type PostService struct {
	repo      ports.Repository
	tags      ports.TagLinker
	postMedia mediaports.PostMediaRepository
	media     mediaports.Repository
	ids       IDGenerator
	clock     Clock
	cache     readcache.Cache
	notifier  ports.PublishNotifier
}

// New returns a PostService without media or tag support; see WithMedia and WithTags.
func New(repo ports.Repository, ids IDGenerator, clock Clock) *PostService {
	return &PostService{repo: repo, ids: ids, clock: clock}
}

// WithMedia enables post media attachments.
func (s *PostService) WithMedia(postMedia mediaports.PostMediaRepository, media mediaports.Repository) *PostService {
	s.postMedia = postMedia
	s.media = media

	return s
}

// WithCache caches public reads in cache; every post write invalidates the posts family.
func (s *PostService) WithCache(cache readcache.Cache) *PostService {
	s.cache = cache
	return s
}

// WithNotifier sends a notice whenever a post is published.
func (s *PostService) WithNotifier(notifier ports.PublishNotifier) *PostService {
	s.notifier = notifier
	return s
}

// WithTags enables post tags.
func (s *PostService) WithTags(tags ports.TagLinker) *PostService {
	s.tags = tags
	return s
}

// ListPublished returns a page of published posts.
func (s *PostService) ListPublished(ctx context.Context, filter postdomain.ListFilter) (postdomain.ListResult, error) {
	if filter.Page < 1 {
		filter.Page = 1
	}

	if filter.PerPage < 1 || filter.PerPage > 100 {
		filter.PerPage = 20
	}

	key := readcache.Key("list", "page", filter.Page, "per_page", filter.PerPage,
		"category", filter.CategoryUUID, "tag", filter.TagUUID)

	return readcache.Through(ctx, s.cache, readcache.Posts, key, func() (postdomain.ListResult, error) {
		return s.repo.ListPublished(ctx, filter)
	})
}

// ListAdmin returns a page of posts for the admin list.
func (s *PostService) ListAdmin(ctx context.Context, filter postdomain.AdminListFilter) (postdomain.ListResult, error) {
	if filter.Page < 1 {
		filter.Page = 1
	}

	if filter.PerPage < 1 || filter.PerPage > 100 {
		filter.PerPage = 20
	}

	if filter.ScopeAuthorUUID != nil {
		filter.AuthorUUID = filter.ScopeAuthorUUID
	}

	filter.Status = strings.TrimSpace(strings.ToLower(filter.Status))
	filter.Query = strings.TrimSpace(filter.Query)

	return s.repo.ListAdmin(ctx, filter)
}

// GetPublishedBySlug returns the published post with the slug or ErrNotFound.
func (s *PostService) GetPublishedBySlug(ctx context.Context, slug string) (postdomain.Post, error) {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return postdomain.Post{}, postdomain.ErrNotFound
	}

	return readcache.Through(ctx, s.cache, readcache.Posts, readcache.Key("get", "slug", slug), func() (postdomain.Post, error) {
		return s.repo.GetPublishedBySlug(ctx, slug)
	})
}

// invalidate drops cached public post reads after a write.
func (s *PostService) invalidate(ctx context.Context) {
	readcache.Invalidate(ctx, s.cache, readcache.Posts)
}

// CreateDraft validates and stores a draft post, optionally with tags.
func (s *PostService) CreateDraft(
	ctx context.Context,
	authorID uuid.UUID,
	title, slug, excerpt, content string,
	categoryID *uuid.UUID,
	tags *[]string,
) (postdomain.Post, []tagdomain.Tag, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return postdomain.Post{}, nil, fmt.Errorf("%w: title required", ErrValidation)
	}

	slug, err := draftSlug(title, slug)
	if err != nil {
		return postdomain.Post{}, nil, err
	}

	var resolvedTags []tagdomain.Tag

	if tags != nil {
		if s.tags == nil {
			return postdomain.Post{}, nil, fmt.Errorf("%w: tag associations not configured", ErrValidation)
		}

		resolvedTags, err = s.tags.ResolveOrCreate(ctx, *tags)
		if err != nil {
			return postdomain.Post{}, nil, err
		}
	}

	now := s.clock.Now()
	post := postdomain.Post{
		UUID:          s.ids.New(),
		AuthorUUID:    authorID,
		CategoryUUID:  categoryID,
		Title:         title,
		Excerpt:       strings.TrimSpace(excerpt),
		Content:       content,
		Status:        postdomain.StatusDraft,
		CommentPolicy: postdomain.CommentPolicyOpen,
		Version:       1,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	post, err = s.withFreeSlug(ctx, slug, func(free string) (postdomain.Post, error) {
		post.Slug = free
		return s.repo.Create(ctx, post)
	})
	if err != nil {
		return postdomain.Post{}, nil, err
	}

	if tags != nil {
		tagIDs := make([]uuid.UUID, 0, len(resolvedTags))
		for _, tag := range resolvedTags {
			tagIDs = append(tagIDs, tag.UUID)
		}

		if err := s.tags.ReplacePostTags(ctx, post.UUID, tagIDs); err != nil {
			return postdomain.Post{}, nil, err
		}

		return post, resolvedTags, nil
	}

	if s.tags == nil {
		return post, nil, nil
	}

	current, err := s.tags.ListByPostID(ctx, post.UUID)
	if err != nil {
		return postdomain.Post{}, nil, err
	}

	return post, current, nil
}

// Update applies in to the post; unrestricted callers may edit posts they do not author.
func (s *PostService) Update(
	ctx context.Context,
	id, actorID uuid.UUID,
	unrestricted bool,
	in postdomain.UpdateInput,
) (postdomain.Post, []tagdomain.Tag, error) {
	if in.Title == nil && in.Slug == nil && in.Excerpt == nil && in.Content == nil &&
		!in.CategoryUUID.Present && in.Tags == nil && in.CommentPolicy == nil {
		return postdomain.Post{}, nil, fmt.Errorf("%w: no fields to update", ErrValidation)
	}

	post, err := s.editablePost(ctx, id, actorID, unrestricted)
	if err != nil {
		return postdomain.Post{}, nil, err
	}

	if err := s.applyUpdate(ctx, &post, in); err != nil {
		return postdomain.Post{}, nil, err
	}

	resolvedTags, err := s.resolveTags(ctx, in.Tags)
	if err != nil {
		return postdomain.Post{}, nil, err
	}

	post.UpdatedAt = s.clock.Now()
	post.Version++

	post, err = s.repo.Update(ctx, post)
	if err != nil {
		return postdomain.Post{}, nil, err
	}

	defer s.invalidate(ctx)

	tags, err := s.syncTags(ctx, post.UUID, in.Tags != nil, resolvedTags)
	if err != nil {
		return postdomain.Post{}, nil, err
	}

	return post, tags, nil
}

// editablePost loads a live post the actor may edit; non-owners without
// unrestricted access get ErrNotFound so drafts do not leak.
func (s *PostService) editablePost(ctx context.Context, id, actorID uuid.UUID, unrestricted bool) (postdomain.Post, error) {
	post, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return postdomain.Post{}, err
	}

	if post.DeletedAt != nil {
		return postdomain.Post{}, postdomain.ErrNotFound
	}

	if !unrestricted && post.AuthorUUID != actorID {
		return postdomain.Post{}, postdomain.ErrNotFound
	}

	return post, nil
}

func (s *PostService) applyUpdate(ctx context.Context, post *postdomain.Post, in postdomain.UpdateInput) error {
	if in.Title != nil {
		title := strings.TrimSpace(*in.Title)
		if title == "" {
			return fmt.Errorf("%w: title required", ErrValidation)
		}

		post.Title = title
	}

	if in.Slug != nil {
		slug, err := s.updatedSlug(ctx, *post, *in.Slug)
		if err != nil {
			return err
		}

		post.Slug = slug
	}

	if in.Excerpt != nil {
		post.Excerpt = strings.TrimSpace(*in.Excerpt)
	}

	if in.Content != nil {
		post.Content = *in.Content
	}

	if in.CategoryUUID.Present {
		post.CategoryUUID = in.CategoryUUID.Value
	}

	if in.CommentPolicy != nil {
		if !in.CommentPolicy.Valid() {
			return fmt.Errorf("%w: comment_policy must be open, authenticated, read_only, or disabled", ErrValidation)
		}

		post.CommentPolicy = *in.CommentPolicy
	}

	return nil
}

// updatedSlug validates a new slug and checks it is free unless unchanged.
func (s *PostService) updatedSlug(ctx context.Context, post postdomain.Post, raw string) (string, error) {
	slug := strings.TrimSpace(strings.ToLower(raw))
	if slug == "" {
		return "", fmt.Errorf("%w: slug required", ErrValidation)
	}

	if !slugPattern.MatchString(slug) {
		return "", fmt.Errorf("%w: invalid slug", ErrValidation)
	}

	if slug == post.Slug {
		return slug, nil
	}

	taken, err := s.repo.SlugTaken(ctx, slug, post.UUID)
	if err != nil {
		return "", err
	}

	if taken {
		return "", ErrConflict
	}

	return slug, nil
}

// resolveTags resolves or creates the requested tag names; nil means tags are not being changed.
func (s *PostService) resolveTags(ctx context.Context, names *[]string) ([]tagdomain.Tag, error) {
	if names == nil {
		return nil, nil
	}

	if s.tags == nil {
		return nil, fmt.Errorf("%w: tag associations not configured", ErrValidation)
	}

	return s.tags.ResolveOrCreate(ctx, *names)
}

// syncTags replaces the post's tags when replace is set, otherwise returns the current tags.
func (s *PostService) syncTags(ctx context.Context, postID uuid.UUID, replace bool, resolved []tagdomain.Tag) ([]tagdomain.Tag, error) {
	if replace {
		tagIDs := make([]uuid.UUID, 0, len(resolved))
		for _, tag := range resolved {
			tagIDs = append(tagIDs, tag.UUID)
		}

		if err := s.tags.ReplacePostTags(ctx, postID, tagIDs); err != nil {
			return nil, err
		}

		return resolved, nil
	}

	if s.tags == nil {
		return nil, nil
	}

	return s.tags.ListByPostID(ctx, postID)
}

// ReplaceMedia replaces the post's media attachments and syncs its cover image.
func (s *PostService) ReplaceMedia(
	ctx context.Context,
	postID uuid.UUID,
	items []mediadomain.PostMediaItem,
	enforceCoverConsistency bool,
) ([]mediadomain.PostMediaItem, error) {
	if s.postMedia == nil || s.media == nil {
		return nil, fmt.Errorf("%w: media associations not configured", ErrValidation)
	}

	post, err := s.repo.GetByID(ctx, postID)
	if err != nil {
		return nil, err
	}

	if post.DeletedAt != nil {
		return nil, postdomain.ErrNotFound
	}

	_ = enforceCoverConsistency // cover FK is always synced from kind=cover items

	normalized := make([]mediadomain.PostMediaItem, 0, len(items))
	coverCount := 0

	var coverID *uuid.UUID

	seen := map[string]struct{}{}

	for i, item := range items {
		next, err := s.normalizeMediaItem(ctx, i, item)
		if err != nil {
			return nil, err
		}

		key := next.MediaAssetUUID.String() + ":" + next.Kind
		if _, ok := seen[key]; ok {
			return nil, fmt.Errorf("%w: duplicate media item", ErrValidation)
		}

		seen[key] = struct{}{}

		if next.Kind == mediadomain.KindCover {
			coverCount++
			id := next.MediaAssetUUID
			coverID = &id
		}

		normalized = append(normalized, next)
	}

	if coverCount > 1 {
		return nil, fmt.Errorf("%w: at most one cover media item allowed", ErrValidation)
	}

	if err := s.postMedia.ReplaceAll(ctx, postID, normalized); err != nil {
		return nil, err
	}

	defer s.invalidate(ctx)

	if err := s.repo.SetCoverImage(ctx, postID, coverID, s.clock.Now()); err != nil {
		return nil, err
	}

	return normalized, nil
}

// normalizeMediaItem validates one requested item at position i and attaches its ready asset.
func (s *PostService) normalizeMediaItem(ctx context.Context, i int, item mediadomain.PostMediaItem) (mediadomain.PostMediaItem, error) {
	kind := strings.TrimSpace(strings.ToLower(item.Kind))
	switch kind {
	case mediadomain.KindCover, mediadomain.KindInlineImage, mediadomain.KindAttachment:
	default:
		return mediadomain.PostMediaItem{}, fmt.Errorf("%w: invalid media kind %q", ErrValidation, item.Kind)
	}

	if item.MediaAssetUUID == uuid.Nil {
		return mediadomain.PostMediaItem{}, fmt.Errorf("%w: media_asset_id required", ErrValidation)
	}

	asset, err := s.media.GetByID(ctx, item.MediaAssetUUID)
	if errors.Is(err, mediadomain.ErrNotFound) {
		return mediadomain.PostMediaItem{}, fmt.Errorf("%w: media %s not found", ErrValidation, item.MediaAssetUUID)
	}

	if err != nil {
		return mediadomain.PostMediaItem{}, err
	}

	if asset.Status != mediadomain.StatusReady {
		return mediadomain.PostMediaItem{}, fmt.Errorf("%w: media %s is not ready", ErrValidation, item.MediaAssetUUID)
	}

	sortOrder := item.SortOrder
	if sortOrder == 0 && i > 0 {
		sortOrder = i
	}

	return mediadomain.PostMediaItem{
		MediaAssetUUID: item.MediaAssetUUID,
		Kind:           kind,
		SortOrder:      sortOrder,
		Media:          &asset,
	}, nil
}

// ParseOptionalUUID parses raw as a UUID, returning nil for blank input.
func ParseOptionalUUID(raw string) (*uuid.UUID, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	id, err := uuid.Parse(raw)
	if err != nil {
		return nil, err
	}

	return &id, nil
}
