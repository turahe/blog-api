package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	categorydomain "github.com/turahe/blog-api/internal/core/category/domain"
	"github.com/turahe/blog-api/internal/core/category/ports"
)

var (
	ErrValidation = errors.New("validation error")
	slugPattern   = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
)

type CreateInput = ports.CreateInput
type UpdateInput = ports.UpdateInput

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

func (s *CategoryService) Create(ctx context.Context, in ports.CreateInput) (categorydomain.Category, error) {
	name, slug, desc, err := s.validateCreateFields(ctx, in)
	if err != nil {
		return categorydomain.Category{}, err
	}
	if err := s.validateParentAndBefore(ctx, in.ParentID, in.BeforeID); err != nil {
		return categorydomain.Category{}, err
	}

	var out categorydomain.Category
	err = s.repo.WithinTx(ctx, func(ctx context.Context, r ports.Repository) error {
		all, err := r.List(ctx)
		if err != nil {
			return err
		}
		now := s.clock.Now()
		cat := categorydomain.Category{
			UUID:        s.ids.New(),
			Name:        name,
			Slug:        slug,
			Description: desc,
			ParentUUID:  in.ParentID,
			ImageUUID:   in.ImageID,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if _, err := r.Create(ctx, cat); err != nil {
			return err
		}
		all = append(all, cat)
		all, err = applyBefore(all, cat.UUID, in.ParentID, in.BeforeID)
		if err != nil {
			return err
		}
		all = rebuildBounds(all)
		if err := r.ReplaceTreeBounds(ctx, all); err != nil {
			return err
		}
		out = findByID(all, cat.UUID)
		return nil
	})
	return out, err
}

func (s *CategoryService) Update(ctx context.Context, id uuid.UUID, in ports.UpdateInput) (categorydomain.Category, error) {
	if in.Name == nil && in.Slug == nil && in.Description == nil && !in.ImageIDProvided {
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
		if utf8.RuneCountInString(name) > 128 {
			return categorydomain.Category{}, fmt.Errorf("%w: name too long", ErrValidation)
		}
		cat.Name = name
	}

	if in.Slug != nil {
		slugVal := strings.TrimSpace(strings.ToLower(*in.Slug))
		if slugVal == "" {
			return categorydomain.Category{}, fmt.Errorf("%w: slug required", ErrValidation)
		}
		if !slugPattern.MatchString(slugVal) {
			return categorydomain.Category{}, fmt.Errorf("%w: invalid slug", ErrValidation)
		}
		if slugVal != cat.Slug {
			taken, err := s.repo.SlugTaken(ctx, slugVal, cat.UUID)
			if err != nil {
				return categorydomain.Category{}, err
			}
			if taken {
				return categorydomain.Category{}, categorydomain.ErrConflict
			}
		}
		cat.Slug = slugVal
	}

	if in.Description != nil {
		cat.Description = *in.Description
	}

	if in.ImageIDProvided {
		cat.ImageUUID = in.ImageID
	}

	cat.UpdatedAt = s.clock.Now()
	return s.repo.Update(ctx, cat)
}

func (s *CategoryService) Delete(ctx context.Context, id uuid.UUID) error {
	children, err := s.repo.CountChildren(ctx, id)
	if err != nil {
		return err
	}
	if children > 0 {
		return categorydomain.ErrInUse
	}
	posts, err := s.repo.CountPosts(ctx, id)
	if err != nil {
		return err
	}
	if posts > 0 {
		return categorydomain.ErrInUse
	}

	return s.repo.WithinTx(ctx, func(ctx context.Context, r ports.Repository) error {
		if err := r.Delete(ctx, id); err != nil {
			return err
		}
		all, err := r.List(ctx)
		if err != nil {
			return err
		}
		all = rebuildBounds(all)
		return r.ReplaceTreeBounds(ctx, all)
	})
}

func (s *CategoryService) Move(ctx context.Context, id uuid.UUID, parentID *uuid.UUID, beforeID *uuid.UUID) (categorydomain.Category, error) {
	if parentID != nil && *parentID == id {
		return categorydomain.Category{}, fmt.Errorf("%w: cannot move category under itself", ErrValidation)
	}
	if err := s.validateParentAndBefore(ctx, parentID, beforeID); err != nil {
		return categorydomain.Category{}, err
	}

	var out categorydomain.Category
	err := s.repo.WithinTx(ctx, func(ctx context.Context, r ports.Repository) error {
		all, err := r.List(ctx)
		if err != nil {
			return err
		}
		node, ok := findByIDOptional(all, id)
		if !ok {
			return categorydomain.ErrNotFound
		}
		if parentID != nil {
			if isDescendant(all, id, *parentID) {
				return fmt.Errorf("%w: cannot move category under its descendant", ErrValidation)
			}
		}
		node.ParentUUID = parentID
		node.UpdatedAt = s.clock.Now()
		all = replace(all, node)
		all, err = applyBefore(all, id, parentID, beforeID)
		if err != nil {
			return err
		}
		if _, err := r.Update(ctx, node); err != nil {
			return err
		}
		all = rebuildBounds(all)
		if err := r.ReplaceTreeBounds(ctx, all); err != nil {
			return err
		}
		out = findByID(all, id)
		return nil
	})
	return out, err
}

func (s *CategoryService) RebuildAll(ctx context.Context) error {
	return s.repo.WithinTx(ctx, func(ctx context.Context, r ports.Repository) error {
		all, err := r.List(ctx)
		if err != nil {
			return err
		}
		all = rebuildBounds(all)
		return r.ReplaceTreeBounds(ctx, all)
	})
}

func (s *CategoryService) validateCreateFields(ctx context.Context, in ports.CreateInput) (name, slug, desc string, err error) {
	name = strings.TrimSpace(in.Name)
	if name == "" {
		return "", "", "", fmt.Errorf("%w: name required", ErrValidation)
	}
	if utf8.RuneCountInString(name) > 128 {
		return "", "", "", fmt.Errorf("%w: name too long", ErrValidation)
	}

	slug = strings.TrimSpace(in.Slug)
	if slug == "" {
		slug = slugify(name)
		if slug == "" {
			return "", "", "", fmt.Errorf("%w: invalid slug", ErrValidation)
		}
	} else {
		slug = strings.ToLower(slug)
		if !slugPattern.MatchString(slug) {
			return "", "", "", fmt.Errorf("%w: invalid slug", ErrValidation)
		}
	}

	taken, err := s.repo.SlugTaken(ctx, slug, uuid.Nil)
	if err != nil {
		return "", "", "", err
	}
	if taken {
		return "", "", "", categorydomain.ErrConflict
	}

	if in.Description != nil {
		desc = *in.Description
	}
	return name, slug, desc, nil
}

func (s *CategoryService) validateParentAndBefore(ctx context.Context, parentID, beforeID *uuid.UUID) error {
	if parentID != nil {
		if _, err := s.repo.GetByID(ctx, *parentID); err != nil {
			if errors.Is(err, categorydomain.ErrNotFound) {
				return fmt.Errorf("%w: parent not found", ErrValidation)
			}
			return err
		}
	}
	if beforeID != nil {
		before, err := s.repo.GetByID(ctx, *beforeID)
		if err != nil {
			if errors.Is(err, categorydomain.ErrNotFound) {
				return fmt.Errorf("%w: before_id not found", ErrValidation)
			}
			return err
		}
		if !sameParent(before.ParentUUID, parentID) {
			return fmt.Errorf("%w: before_id must share parent", ErrValidation)
		}
	}
	return nil
}

func rebuildBounds(cats []categorydomain.Category) []categorydomain.Category {
	if len(cats) == 0 {
		return nil
	}

	children := make(map[uuid.UUID][]categorydomain.Category)
	var roots []categorydomain.Category
	for _, cat := range cats {
		if cat.ParentUUID == nil {
			roots = append(roots, cat)
		} else {
			pid := *cat.ParentUUID
			children[pid] = append(children[pid], cat)
		}
	}

	sortSiblings := func(s []categorydomain.Category) {
		sort.Slice(s, func(i, j int) bool {
			if s[i].SortOrder != s[j].SortOrder {
				return s[i].SortOrder < s[j].SortOrder
			}
			if s[i].Name != s[j].Name {
				return s[i].Name < s[j].Name
			}
			return s[i].UUID.String() < s[j].UUID.String()
		})
	}

	sortSiblings(roots)

	var out []categorydomain.Category
	counter := 1

	var walk func(cat categorydomain.Category, depth int)
	walk = func(cat categorydomain.Category, depth int) {
		cat.Depth = depth
		cat.Lft = counter
		counter++

		kids := children[cat.UUID]
		sortSiblings(kids)
		for i := range kids {
			kids[i].SortOrder = i
			walk(kids[i], depth+1)
		}

		cat.Rgt = counter
		counter++
		out = append(out, cat)
	}

	for i := range roots {
		roots[i].SortOrder = i
		walk(roots[i], 0)
	}
	return out
}

func applyBefore(cats []categorydomain.Category, id uuid.UUID, parentID, beforeID *uuid.UUID) ([]categorydomain.Category, error) {
	if beforeID != nil {
		before, ok := findByIDOptional(cats, *beforeID)
		if !ok {
			return nil, fmt.Errorf("%w: before_id not found", ErrValidation)
		}
		if !sameParent(before.ParentUUID, parentID) {
			return nil, fmt.Errorf("%w: before_id must share parent", ErrValidation)
		}
	}

	var siblings []categorydomain.Category
	for _, cat := range cats {
		if cat.UUID == id {
			cat.ParentUUID = parentID
			siblings = append(siblings, cat)
			continue
		}
		if sameParent(cat.ParentUUID, parentID) {
			siblings = append(siblings, cat)
		}
	}

	sort.Slice(siblings, func(i, j int) bool {
		if siblings[i].SortOrder != siblings[j].SortOrder {
			return siblings[i].SortOrder < siblings[j].SortOrder
		}
		if siblings[i].Name != siblings[j].Name {
			return siblings[i].Name < siblings[j].Name
		}
		return siblings[i].UUID.String() < siblings[j].UUID.String()
	})

	ordered := make([]uuid.UUID, 0, len(siblings))
	for _, sib := range siblings {
		if sib.UUID == id {
			continue
		}
		ordered = append(ordered, sib.UUID)
	}

	if beforeID == nil {
		ordered = append(ordered, id)
	} else {
		idx := -1
		for i, sid := range ordered {
			if sid == *beforeID {
				idx = i
				break
			}
		}
		if idx < 0 {
			return nil, fmt.Errorf("%w: before_id not found among siblings", ErrValidation)
		}
		ordered = append(ordered[:idx], append([]uuid.UUID{id}, ordered[idx:]...)...)
	}

	for i, sid := range ordered {
		cat := findByID(cats, sid)
		cat.SortOrder = i
		cat.ParentUUID = parentID
		cats = replace(cats, cat)
	}
	return cats, nil
}

func isDescendant(cats []categorydomain.Category, ancestorID, candidateID uuid.UUID) bool {
	current, ok := findByIDOptional(cats, candidateID)
	if !ok {
		return false
	}
	for current.ParentUUID != nil {
		if *current.ParentUUID == ancestorID {
			return true
		}
		current, ok = findByIDOptional(cats, *current.ParentUUID)
		if !ok {
			return false
		}
	}
	return false
}

func sameParent(a, b *uuid.UUID) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func findByID(cats []categorydomain.Category, id uuid.UUID) categorydomain.Category {
	cat, ok := findByIDOptional(cats, id)
	if !ok {
		return categorydomain.Category{}
	}
	return cat
}

func findByIDOptional(cats []categorydomain.Category, id uuid.UUID) (categorydomain.Category, bool) {
	for _, cat := range cats {
		if cat.UUID == id {
			return cat, true
		}
	}
	return categorydomain.Category{}, false
}

func replace(cats []categorydomain.Category, updated categorydomain.Category) []categorydomain.Category {
	for i, cat := range cats {
		if cat.UUID == updated.UUID {
			cats[i] = updated
			return cats
		}
	}
	return append(cats, updated)
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
