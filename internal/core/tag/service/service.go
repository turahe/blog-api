package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	tagdomain "github.com/turahe/blog-api/internal/core/tag/domain"
	"github.com/turahe/blog-api/internal/core/tag/ports"
)

var (
	ErrValidation = errors.New("validation error")
	ErrConflict   = tagdomain.ErrConflict
)

type IDGenerator interface {
	New() uuid.UUID
}

type Clock interface {
	Now() time.Time
}

type Service struct {
	repo  ports.Repository
	ids   IDGenerator
	clock Clock
}

func New(repo ports.Repository, ids IDGenerator, clock Clock) *Service {
	return &Service{repo: repo, ids: ids, clock: clock}
}

func (s *Service) List(ctx context.Context) ([]tagdomain.Tag, error) {
	return s.repo.List(ctx)
}

func (s *Service) Create(ctx context.Context, name, slug string) (tagdomain.Tag, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return tagdomain.Tag{}, fmt.Errorf("%w: name required", ErrValidation)
	}
	if utf8.RuneCountInString(name) > 64 {
		return tagdomain.Tag{}, fmt.Errorf("%w: name too long", ErrValidation)
	}

	slug = strings.TrimSpace(slug)
	if slug == "" {
		slug = slugify(name)
	} else {
		slug = strings.ToLower(slug)
	}
	if slug == "" {
		return tagdomain.Tag{}, fmt.Errorf("%w: invalid slug", ErrValidation)
	}

	taken, err := s.repo.SlugTaken(ctx, slug, uuid.Nil)
	if err != nil {
		return tagdomain.Tag{}, err
	}
	if taken {
		return tagdomain.Tag{}, ErrConflict
	}

	now := s.clock.Now()
	tag := tagdomain.Tag{
		ID:        s.ids.New(),
		Name:      name,
		Slug:      slug,
		CreatedAt: now,
	}
	return s.repo.Create(ctx, tag)
}

func (s *Service) Update(ctx context.Context, id uuid.UUID, name, slug *string) (tagdomain.Tag, error) {
	if name == nil && slug == nil {
		return tagdomain.Tag{}, fmt.Errorf("%w: no fields to update", ErrValidation)
	}

	tag, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return tagdomain.Tag{}, err
	}

	if name != nil {
		n := strings.TrimSpace(*name)
		if n == "" {
			return tagdomain.Tag{}, fmt.Errorf("%w: name required", ErrValidation)
		}
		if utf8.RuneCountInString(n) > 64 {
			return tagdomain.Tag{}, fmt.Errorf("%w: name too long", ErrValidation)
		}
		tag.Name = n
	}

	if slug != nil {
		slugVal := strings.TrimSpace(strings.ToLower(*slug))
		if slugVal == "" {
			return tagdomain.Tag{}, fmt.Errorf("%w: slug required", ErrValidation)
		}
		if slugVal != tag.Slug {
			taken, err := s.repo.SlugTaken(ctx, slugVal, tag.ID)
			if err != nil {
				return tagdomain.Tag{}, err
			}
			if taken {
				return tagdomain.Tag{}, ErrConflict
			}
		}
		tag.Slug = slugVal
	}

	return s.repo.Update(ctx, tag)
}

func (s *Service) Merge(ctx context.Context, sourceID, intoID uuid.UUID) error {
	if sourceID == intoID {
		return fmt.Errorf("%w: source and target must differ", ErrValidation)
	}
	if _, err := s.repo.GetByID(ctx, sourceID); err != nil {
		return err
	}
	if _, err := s.repo.GetByID(ctx, intoID); err != nil {
		return err
	}
	return s.repo.MergeInto(ctx, sourceID, intoID)
}

func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	n, err := s.repo.CountPosts(ctx, id)
	if err != nil {
		return err
	}
	if n > 0 {
		return tagdomain.ErrInUse
	}
	return s.repo.Delete(ctx, id)
}

func (s *Service) ResolveOrCreate(ctx context.Context, names []string) ([]tagdomain.Tag, error) {
	seen := map[string]struct{}{}
	out := make([]tagdomain.Tag, 0, len(names))
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		if utf8.RuneCountInString(name) > 64 {
			return nil, fmt.Errorf("%w: name too long", ErrValidation)
		}
		slug := slugify(name)
		if slug == "" {
			return nil, fmt.Errorf("%w: invalid tag name", ErrValidation)
		}
		if _, ok := seen[slug]; ok {
			continue
		}
		seen[slug] = struct{}{}
		if existing, err := s.repo.GetBySlug(ctx, slug); err == nil {
			out = append(out, existing)
			continue
		} else if !errors.Is(err, tagdomain.ErrNotFound) {
			return nil, err
		}
		created, err := s.Create(ctx, name, slug)
		if err != nil {
			return nil, err
		}
		out = append(out, created)
	}
	return out, nil
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
