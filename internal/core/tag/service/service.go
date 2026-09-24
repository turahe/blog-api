// Package service implements tag CRUD, merging, and post tag resolution.
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
	"github.com/turahe/blog-api/internal/core/readcache"
	tagdomain "github.com/turahe/blog-api/internal/core/tag/domain"
	"github.com/turahe/blog-api/internal/core/tag/ports"
)

// Tag service errors.
var (
	ErrValidation = errors.New("validation error")
	ErrConflict   = tagdomain.ErrConflict
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

// Service implements ports.Service and the post service's tag linker.
type Service struct {
	repo  ports.Repository
	ids   IDGenerator
	clock Clock
	cache readcache.Cache
}

// New returns a Service.
func New(repo ports.Repository, ids IDGenerator, clock Clock) *Service {
	return &Service{repo: repo, ids: ids, clock: clock}
}

// WithCache caches public reads in cache. Tag writes invalidate the tags family;
// merges and post re-tagging also invalidate posts, whose tag filter they change.
func (s *Service) WithCache(cache readcache.Cache) *Service {
	s.cache = cache
	return s
}

// List returns every tag.
func (s *Service) List(ctx context.Context) ([]tagdomain.Tag, error) {
	return readcache.Through(ctx, s.cache, readcache.Tags, readcache.Key("list"), func() ([]tagdomain.Tag, error) {
		return s.repo.List(ctx)
	})
}

// written invalidates families when the write succeeded and returns err.
func (s *Service) written(ctx context.Context, err error, families ...readcache.Family) error {
	if err == nil {
		readcache.Invalidate(ctx, s.cache, families...)
	}

	return err
}

// Create validates and stores a tag, deriving the slug from the name when blank.
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
		if slug == "" {
			return tagdomain.Tag{}, fmt.Errorf("%w: invalid slug", ErrValidation)
		}
	} else {
		slug = strings.ToLower(slug)
		if !slugPattern.MatchString(slug) {
			return tagdomain.Tag{}, fmt.Errorf("%w: invalid slug", ErrValidation)
		}
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
		UUID:      s.ids.New(),
		Name:      name,
		Slug:      slug,
		CreatedAt: now,
	}

	tag, err = s.repo.Create(ctx, tag)

	return tag, s.written(ctx, err, readcache.Tags)
}

// Update changes the tag's name and/or slug.
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
		if tag.Slug, err = s.updatedSlug(ctx, tag, *slug); err != nil {
			return tagdomain.Tag{}, err
		}
	}

	tag, err = s.repo.Update(ctx, tag)

	return tag, s.written(ctx, err, readcache.Tags)
}

// updatedSlug validates a new slug and checks it is free unless unchanged.
func (s *Service) updatedSlug(ctx context.Context, tag tagdomain.Tag, raw string) (string, error) {
	slug := strings.TrimSpace(strings.ToLower(raw))
	if slug == "" {
		return "", fmt.Errorf("%w: slug required", ErrValidation)
	}

	if !slugPattern.MatchString(slug) {
		return "", fmt.Errorf("%w: invalid slug", ErrValidation)
	}

	if slug == tag.Slug {
		return slug, nil
	}

	taken, err := s.repo.SlugTaken(ctx, slug, tag.UUID)
	if err != nil {
		return "", err
	}

	if taken {
		return "", ErrConflict
	}

	return slug, nil
}

// Merge moves sourceID's posts onto intoID and deletes sourceID.
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

	return s.written(ctx, s.repo.MergeInto(ctx, sourceID, intoID), readcache.Tags, readcache.Posts)
}

// Delete removes a tag that no post uses.
func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	n, err := s.repo.CountPosts(ctx, id)
	if err != nil {
		return err
	}

	if n > 0 {
		return tagdomain.ErrInUse
	}

	return s.written(ctx, s.repo.Delete(ctx, id), readcache.Tags)
}

// ResolveOrCreate returns tags for names, creating missing ones by slug.
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

// ReplacePostTags sets the post's tags to exactly tagIDs.
func (s *Service) ReplacePostTags(ctx context.Context, postID uuid.UUID, tagIDs []uuid.UUID) error {
	return s.written(ctx, s.repo.ReplacePostTags(ctx, postID, tagIDs), readcache.Posts)
}

// ListByPostID returns the post's tags.
func (s *Service) ListByPostID(ctx context.Context, postID uuid.UUID) ([]tagdomain.Tag, error) {
	return s.repo.ListByPostID(ctx, postID)
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
