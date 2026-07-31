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
	tagdomain "github.com/turahe/blog-api/internal/core/tag/domain"
)

var (
	ErrValidation = errors.New("validation error")
	ErrConflict   = postdomain.ErrConflict
	slugPattern   = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
)

type IDGenerator interface {
	New() uuid.UUID
}

type Clock interface {
	Now() time.Time
}

type PostService struct {
	repo      ports.Repository
	tags      ports.TagLinker
	postMedia mediaports.PostMediaRepository
	media     mediaports.Repository
	ids       IDGenerator
	clock     Clock
}

func New(repo ports.Repository, ids IDGenerator, clock Clock) *PostService {
	return &PostService{repo: repo, ids: ids, clock: clock}
}

func (s *PostService) WithMedia(postMedia mediaports.PostMediaRepository, media mediaports.Repository) *PostService {
	s.postMedia = postMedia
	s.media = media
	return s
}

func (s *PostService) WithTags(tags ports.TagLinker) *PostService {
	s.tags = tags
	return s
}

func (s *PostService) ListPublished(ctx context.Context, filter postdomain.ListFilter) (postdomain.ListResult, error) {
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.PerPage < 1 || filter.PerPage > 100 {
		filter.PerPage = 20
	}
	return s.repo.ListPublished(ctx, filter)
}

func (s *PostService) ListAdmin(ctx context.Context, filter postdomain.AdminListFilter) (postdomain.ListResult, error) {
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.PerPage < 1 || filter.PerPage > 100 {
		filter.PerPage = 20
	}
	if filter.ScopeAuthorID != nil {
		filter.AuthorID = filter.ScopeAuthorID
	}
	filter.Status = strings.TrimSpace(strings.ToLower(filter.Status))
	filter.Query = strings.TrimSpace(filter.Query)
	return s.repo.ListAdmin(ctx, filter)
}

func (s *PostService) GetPublishedBySlug(ctx context.Context, slug string) (postdomain.Post, error) {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return postdomain.Post{}, postdomain.ErrNotFound
	}
	return s.repo.GetPublishedBySlug(ctx, slug)
}

func (s *PostService) CreateDraft(
	ctx context.Context,
	authorID uuid.UUID,
	title, slug, excerpt, content string,
	categoryID *uuid.UUID,
	tags *[]string,
) (postdomain.Post, []tagdomain.Tag, error) {
	title = strings.TrimSpace(title)
	slug = strings.TrimSpace(strings.ToLower(slug))
	if title == "" || slug == "" {
		return postdomain.Post{}, nil, fmt.Errorf("%w: title and slug required", ErrValidation)
	}
	if !slugPattern.MatchString(slug) {
		return postdomain.Post{}, nil, fmt.Errorf("%w: invalid slug", ErrValidation)
	}
	var resolvedTags []tagdomain.Tag
	var err error
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
		ID:         s.ids.New(),
		AuthorID:   authorID,
		CategoryID: categoryID,
		Title:      title,
		Slug:       slug,
		Excerpt:    strings.TrimSpace(excerpt),
		Content:    content,
		Status:     postdomain.StatusDraft,
		Version:    1,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	post, err = s.repo.Create(ctx, post)
	if err != nil {
		return postdomain.Post{}, nil, err
	}
	if tags != nil {
		tagIDs := make([]uuid.UUID, 0, len(resolvedTags))
		for _, tag := range resolvedTags {
			tagIDs = append(tagIDs, tag.ID)
		}
		if err := s.tags.ReplacePostTags(ctx, post.ID, tagIDs); err != nil {
			return postdomain.Post{}, nil, err
		}
		return post, resolvedTags, nil
	}
	if s.tags == nil {
		return post, nil, nil
	}
	current, err := s.tags.ListByPostID(ctx, post.ID)
	if err != nil {
		return postdomain.Post{}, nil, err
	}
	return post, current, nil
}

func (s *PostService) Publish(ctx context.Context, id uuid.UUID) (postdomain.Post, error) {
	post, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return postdomain.Post{}, err
	}
	if post.DeletedAt != nil {
		return postdomain.Post{}, postdomain.ErrNotFound
	}
	now := s.clock.Now()
	post.Status = postdomain.StatusPublished
	post.PublishedAt = &now
	post.UpdatedAt = now
	post.Version++
	return s.repo.Update(ctx, post)
}

func (s *PostService) Unpublish(ctx context.Context, id uuid.UUID) (postdomain.Post, error) {
	post, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return postdomain.Post{}, err
	}
	if post.DeletedAt != nil {
		return postdomain.Post{}, postdomain.ErrNotFound
	}
	if post.Status != postdomain.StatusPublished {
		return postdomain.Post{}, fmt.Errorf("%w: only published posts can be unpublished", ErrValidation)
	}
	now := s.clock.Now()
	post.Status = postdomain.StatusDraft
	post.UpdatedAt = now
	post.Version++
	return s.repo.Update(ctx, post)
}

func (s *PostService) Archive(ctx context.Context, id uuid.UUID) (postdomain.Post, error) {
	post, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return postdomain.Post{}, err
	}
	if post.DeletedAt != nil {
		return postdomain.Post{}, postdomain.ErrNotFound
	}
	if post.Status == postdomain.StatusArchived {
		return post, nil
	}
	now := s.clock.Now()
	post.Status = postdomain.StatusArchived
	post.UpdatedAt = now
	post.Version++
	return s.repo.Update(ctx, post)
}

func (s *PostService) Update(
	ctx context.Context,
	id, actorID uuid.UUID,
	unrestricted bool,
	in postdomain.UpdateInput,
) (postdomain.Post, []tagdomain.Tag, error) {
	if in.Title == nil && in.Slug == nil && in.Excerpt == nil && in.Content == nil && !in.CategoryID.Present && in.Tags == nil {
		return postdomain.Post{}, nil, fmt.Errorf("%w: no fields to update", ErrValidation)
	}

	post, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return postdomain.Post{}, nil, err
	}
	if post.DeletedAt != nil {
		return postdomain.Post{}, nil, postdomain.ErrNotFound
	}
	if !unrestricted && post.AuthorID != actorID {
		return postdomain.Post{}, nil, postdomain.ErrNotFound
	}

	if in.Title != nil {
		title := strings.TrimSpace(*in.Title)
		if title == "" {
			return postdomain.Post{}, nil, fmt.Errorf("%w: title required", ErrValidation)
		}
		post.Title = title
	}

	if in.Slug != nil {
		slug := strings.TrimSpace(strings.ToLower(*in.Slug))
		if slug == "" {
			return postdomain.Post{}, nil, fmt.Errorf("%w: slug required", ErrValidation)
		}
		if !slugPattern.MatchString(slug) {
			return postdomain.Post{}, nil, fmt.Errorf("%w: invalid slug", ErrValidation)
		}
		if slug != post.Slug {
			taken, err := s.repo.SlugTaken(ctx, slug, post.ID)
			if err != nil {
				return postdomain.Post{}, nil, err
			}
			if taken {
				return postdomain.Post{}, nil, ErrConflict
			}
		}
		post.Slug = slug
	}

	if in.Excerpt != nil {
		post.Excerpt = strings.TrimSpace(*in.Excerpt)
	}
	if in.Content != nil {
		post.Content = *in.Content
	}
	if in.CategoryID.Present {
		post.CategoryID = in.CategoryID.Value
	}

	var resolvedTags []tagdomain.Tag
	if in.Tags != nil {
		if s.tags == nil {
			return postdomain.Post{}, nil, fmt.Errorf("%w: tag associations not configured", ErrValidation)
		}
		resolvedTags, err = s.tags.ResolveOrCreate(ctx, *in.Tags)
		if err != nil {
			return postdomain.Post{}, nil, err
		}
	}

	post.UpdatedAt = s.clock.Now()
	post.Version++
	post, err = s.repo.Update(ctx, post)
	if err != nil {
		return postdomain.Post{}, nil, err
	}
	if in.Tags != nil {
		tagIDs := make([]uuid.UUID, 0, len(resolvedTags))
		for _, tag := range resolvedTags {
			tagIDs = append(tagIDs, tag.ID)
		}
		if err := s.tags.ReplacePostTags(ctx, post.ID, tagIDs); err != nil {
			return postdomain.Post{}, nil, err
		}
		return post, resolvedTags, nil
	}
	if s.tags == nil {
		return post, nil, nil
	}
	current, err := s.tags.ListByPostID(ctx, post.ID)
	if err != nil {
		return postdomain.Post{}, nil, err
	}
	return post, current, nil
}

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
		kind := strings.TrimSpace(strings.ToLower(item.Kind))
		switch kind {
		case mediadomain.KindCover, mediadomain.KindInlineImage, mediadomain.KindAttachment:
		default:
			return nil, fmt.Errorf("%w: invalid media kind %q", ErrValidation, item.Kind)
		}
		if item.MediaAssetID == uuid.Nil {
			return nil, fmt.Errorf("%w: media_asset_id required", ErrValidation)
		}
		key := item.MediaAssetID.String() + ":" + kind
		if _, ok := seen[key]; ok {
			return nil, fmt.Errorf("%w: duplicate media item", ErrValidation)
		}
		seen[key] = struct{}{}

		asset, err := s.media.GetByID(ctx, item.MediaAssetID)
		if err != nil {
			if errors.Is(err, mediadomain.ErrNotFound) {
				return nil, fmt.Errorf("%w: media %s not found", ErrValidation, item.MediaAssetID)
			}
			return nil, err
		}
		if asset.Status != mediadomain.StatusReady {
			return nil, fmt.Errorf("%w: media %s is not ready", ErrValidation, item.MediaAssetID)
		}

		sortOrder := item.SortOrder
		if sortOrder == 0 && i > 0 {
			sortOrder = i
		}
		assetCopy := asset
		normalized = append(normalized, mediadomain.PostMediaItem{
			MediaAssetID: item.MediaAssetID,
			Kind:         kind,
			SortOrder:    sortOrder,
			Media:        &assetCopy,
		})
		if kind == mediadomain.KindCover {
			coverCount++
			id := item.MediaAssetID
			coverID = &id
		}
	}
	if coverCount > 1 {
		return nil, fmt.Errorf("%w: at most one cover media item allowed", ErrValidation)
	}

	if err := s.postMedia.ReplaceAll(ctx, postID, normalized); err != nil {
		return nil, err
	}
	if err := s.repo.SetCoverImage(ctx, postID, coverID, s.clock.Now()); err != nil {
		return nil, err
	}
	return normalized, nil
}

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
