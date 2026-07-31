package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	categorydomain "github.com/turahe/blog-api/internal/core/category/domain"
	"github.com/turahe/blog-api/internal/core/category/ports"
)

var (
	ErrValidation = errors.New("validation error")
	ErrConflict   = categorydomain.ErrConflict
	slugPattern   = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
)

type IDGenerator interface {
	New() uuid.UUID
}

type Clock interface {
	Now() time.Time
}

type CategoryService struct {
	repo  ports.Repository
	ids   IDGenerator
	clock Clock
}

func New(repo ports.Repository, ids IDGenerator, clock Clock) *CategoryService {
	return &CategoryService{repo: repo, ids: ids, clock: clock}
}

func (s *CategoryService) List(ctx context.Context) ([]categorydomain.Category, error) {
	return s.repo.List(ctx)
}

func (s *CategoryService) GetBySlug(ctx context.Context, slug string) (categorydomain.Category, error) {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return categorydomain.Category{}, categorydomain.ErrNotFound
	}
	return s.repo.GetBySlug(ctx, slug)
}

func (s *CategoryService) Create(ctx context.Context, in categorydomain.CreateInput) (categorydomain.Category, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return categorydomain.Category{}, fmt.Errorf("%w: name required", ErrValidation)
	}
	if utf8.RuneCountInString(name) > 120 {
		return categorydomain.Category{}, fmt.Errorf("%w: name too long", ErrValidation)
	}

	slug, err := normalizeSlug(in.Slug, name)
	if err != nil {
		return categorydomain.Category{}, err
	}

	taken, err := s.repo.SlugTaken(ctx, slug, uuid.Nil)
	if err != nil {
		return categorydomain.Category{}, err
	}
	if taken {
		return categorydomain.Category{}, ErrConflict
	}

	if err := s.validateParent(ctx, uuid.Nil, in.ParentID); err != nil {
		return categorydomain.Category{}, err
	}

	desc := strings.TrimSpace(in.Description)
	if utf8.RuneCountInString(desc) > 2000 {
		return categorydomain.Category{}, fmt.Errorf("%w: description too long", ErrValidation)
	}

	max, err := s.repo.MaxSortOrder(ctx, in.ParentID)
	if err != nil {
		return categorydomain.Category{}, err
	}

	now := s.clock.Now()
	cat := categorydomain.Category{
		ID:          s.ids.New(),
		Name:        name,
		Slug:        slug,
		Description: desc,
		ParentID:    in.ParentID,
		ImageID:     in.ImageID,
		SortOrder:   max + 1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	return s.repo.Create(ctx, cat)
}

func (s *CategoryService) Update(ctx context.Context, id uuid.UUID, in categorydomain.UpdateInput) (categorydomain.Category, error) {
	if in.Name == nil && in.Slug == nil && in.Description == nil && !in.ParentID.Present && !in.ImageID.Present {
		return categorydomain.Category{}, fmt.Errorf("%w: no fields to update", ErrValidation)
	}

	cat, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return categorydomain.Category{}, err
	}

	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if name == "" {
			return categorydomain.Category{}, fmt.Errorf("%w: name required", ErrValidation)
		}
		if utf8.RuneCountInString(name) > 120 {
			return categorydomain.Category{}, fmt.Errorf("%w: name too long", ErrValidation)
		}
		cat.Name = name
	}

	if in.Slug != nil {
		slug, err := normalizeSlug(*in.Slug, "")
		if err != nil {
			return categorydomain.Category{}, err
		}
		if slug == "" {
			return categorydomain.Category{}, fmt.Errorf("%w: slug required", ErrValidation)
		}
		if slug != cat.Slug {
			taken, err := s.repo.SlugTaken(ctx, slug, cat.ID)
			if err != nil {
				return categorydomain.Category{}, err
			}
			if taken {
				return categorydomain.Category{}, ErrConflict
			}
		}
		cat.Slug = slug
	}

	if in.Description != nil {
		desc := strings.TrimSpace(*in.Description)
		if utf8.RuneCountInString(desc) > 2000 {
			return categorydomain.Category{}, fmt.Errorf("%w: description too long", ErrValidation)
		}
		cat.Description = desc
	}

	if in.ParentID.Present {
		if err := s.validateParent(ctx, cat.ID, in.ParentID.Value); err != nil {
			return categorydomain.Category{}, err
		}
		cat.ParentID = in.ParentID.Value
	}

	if in.ImageID.Present {
		cat.ImageID = in.ImageID.Value
	}

	cat.UpdatedAt = s.clock.Now()
	return s.repo.Update(ctx, cat)
}

func (s *CategoryService) Delete(ctx context.Context, id uuid.UUID) error {
	if _, err := s.repo.GetByID(ctx, id); err != nil {
		return err
	}
	n, err := s.repo.CountChildren(ctx, id)
	if err != nil {
		return err
	}
	if n > 0 {
		return categorydomain.ErrHasChildren
	}
	return s.repo.Delete(ctx, id)
}

func (s *CategoryService) Reorder(ctx context.Context, parentID *uuid.UUID, orderedIDs []uuid.UUID) error {
	if len(orderedIDs) == 0 {
		return fmt.Errorf("%w: ordered_ids required", ErrValidation)
	}
	if parentID != nil {
		if _, err := s.repo.GetByID(ctx, *parentID); err != nil {
			return err
		}
	}

	siblings, err := s.repo.ListSiblingIDs(ctx, parentID)
	if err != nil {
		return err
	}
	if len(siblings) != len(orderedIDs) {
		return fmt.Errorf("%w: ordered_ids must match sibling set", ErrValidation)
	}
	want := make(map[uuid.UUID]struct{}, len(siblings))
	for _, id := range siblings {
		want[id] = struct{}{}
	}
	seen := make(map[uuid.UUID]struct{}, len(orderedIDs))
	for _, id := range orderedIDs {
		if _, ok := want[id]; !ok {
			return fmt.Errorf("%w: ordered_ids must match sibling set", ErrValidation)
		}
		if _, dup := seen[id]; dup {
			return fmt.Errorf("%w: duplicate ordered id", ErrValidation)
		}
		seen[id] = struct{}{}
	}
	return s.repo.Reorder(ctx, parentID, orderedIDs)
}

func (s *CategoryService) validateParent(ctx context.Context, selfID uuid.UUID, parentID *uuid.UUID) error {
	if parentID == nil {
		return nil
	}
	if selfID != uuid.Nil && *parentID == selfID {
		return fmt.Errorf("%w: parent_id cannot be self", ErrValidation)
	}
	parent, err := s.repo.GetByID(ctx, *parentID)
	if err != nil {
		return err
	}
	if selfID == uuid.Nil {
		return nil
	}
	// Walk ancestors to prevent cycles.
	cursor := parent.ParentID
	for cursor != nil {
		if *cursor == selfID {
			return fmt.Errorf("%w: parent_id would create a cycle", ErrValidation)
		}
		ancestor, err := s.repo.GetByID(ctx, *cursor)
		if err != nil {
			return err
		}
		cursor = ancestor.ParentID
	}
	return nil
}

func normalizeSlug(explicit, nameFallback string) (string, error) {
	slug := strings.TrimSpace(explicit)
	if slug == "" {
		slug = slugify(nameFallback)
		if slug == "" {
			return "", fmt.Errorf("%w: invalid slug", ErrValidation)
		}
		return slug, nil
	}
	slug = strings.ToLower(slug)
	if !slugPattern.MatchString(slug) {
		return "", fmt.Errorf("%w: invalid slug", ErrValidation)
	}
	return slug, nil
}

func slugify(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return r
		case r == ' ' || r == '_' || r == '-':
			return '-'
		default:
			return -1
		}
	}, s)
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	return strings.Trim(s, "-")
}
