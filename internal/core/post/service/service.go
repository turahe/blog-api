package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
	"github.com/turahe/blog-api/internal/core/post/ports"
)

var (
	ErrValidation = errors.New("validation error")
	slugPattern   = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
)

type IDGenerator interface {
	New() uuid.UUID
}

type Clock interface {
	Now() time.Time
}

type PostService struct {
	repo  ports.Repository
	ids   IDGenerator
	clock Clock
}

func New(repo ports.Repository, ids IDGenerator, clock Clock) *PostService {
	return &PostService{repo: repo, ids: ids, clock: clock}
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
) (postdomain.Post, error) {
	title = strings.TrimSpace(title)
	slug = strings.TrimSpace(strings.ToLower(slug))
	if title == "" || slug == "" {
		return postdomain.Post{}, fmt.Errorf("%w: title and slug required", ErrValidation)
	}
	if !slugPattern.MatchString(slug) {
		return postdomain.Post{}, fmt.Errorf("%w: invalid slug", ErrValidation)
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
	return s.repo.Create(ctx, post)
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
