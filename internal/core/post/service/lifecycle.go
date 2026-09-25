package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/core/audit"
	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
)

// slugAttempts bounds retries when a concurrent write claims the chosen slug first.
const slugAttempts = 3

// Publish makes a draft, scheduled, or archived post public now.
func (s *PostService) Publish(ctx context.Context, id uuid.UUID) (postdomain.Post, error) {
	return s.transition(ctx, id, postdomain.TransitionPublish, nil)
}

// PublishBy is Publish on behalf of actorID, so the author is told when someone else publishes.
func (s *PostService) PublishBy(ctx context.Context, actorID, id uuid.UUID) (postdomain.Post, error) {
	return s.transition(ctx, id, postdomain.TransitionPublish, &actorID)
}

// Unpublish returns a published or scheduled post to draft and clears published_at.
func (s *PostService) Unpublish(ctx context.Context, id uuid.UUID) (postdomain.Post, error) {
	return s.transition(ctx, id, postdomain.TransitionUnpublish, nil)
}

// Archive hides a post from public reads while keeping its published_at history.
func (s *PostService) Archive(ctx context.Context, id uuid.UUID) (postdomain.Post, error) {
	return s.transition(ctx, id, postdomain.TransitionArchive, nil)
}

func (s *PostService) transition(ctx context.Context, id uuid.UUID, transition postdomain.Transition, actorID *uuid.UUID) (postdomain.Post, error) {
	post, err := s.livePost(ctx, id)
	if err != nil {
		return postdomain.Post{}, err
	}

	next, err := transition.Next(post.Status)
	if err != nil {
		return postdomain.Post{}, err
	}

	now := s.clock.Now()

	switch next {
	case postdomain.StatusPublished:
		post.PublishedAt = &now
	case postdomain.StatusDraft:
		post.PublishedAt = nil
	case postdomain.StatusScheduled, postdomain.StatusArchived:
	}

	previous := post.Status

	post.Status = next
	post.UpdatedAt = now
	post.Version++

	post, err = s.repo.Update(ctx, post)
	if err != nil {
		return postdomain.Post{}, err
	}

	audit.AddChange(ctx, "status", previous, next)
	s.invalidate(ctx)

	if next == postdomain.StatusPublished && s.notifier != nil {
		s.notifier.PostPublished(ctx, post, actorID)
	}

	return post, nil
}

// Delete soft-deletes a live post; it disappears from public and admin reads until restored.
func (s *PostService) Delete(ctx context.Context, id uuid.UUID) error {
	post, err := s.livePost(ctx, id)
	if err != nil {
		return err
	}

	now := s.clock.Now()
	post.DeletedAt = &now
	post.UpdatedAt = now
	post.Version++

	if err := s.repo.SoftDelete(ctx, post); err != nil {
		return err
	}

	s.invalidate(ctx)

	return nil
}

// Restore brings a soft-deleted post back as a draft. If a live post took its slug
// meanwhile, the restored post gets the next free numeric suffix.
func (s *PostService) Restore(ctx context.Context, id uuid.UUID) (postdomain.Post, error) {
	post, err := s.repo.GetDeletedByID(ctx, id)
	if err != nil {
		return postdomain.Post{}, err
	}

	now := s.clock.Now()
	post.DeletedAt = nil
	post.Status = postdomain.StatusDraft
	post.PublishedAt = nil
	post.UpdatedAt = now
	post.Version++

	post, err = s.withFreeSlug(ctx, post.Slug, func(free string) (postdomain.Post, error) {
		post.Slug = free
		return s.repo.Restore(ctx, post)
	})
	if err != nil {
		return postdomain.Post{}, err
	}

	s.invalidate(ctx)

	return post, nil
}

func (s *PostService) livePost(ctx context.Context, id uuid.UUID) (postdomain.Post, error) {
	post, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return postdomain.Post{}, err
	}

	if post.DeletedAt != nil {
		return postdomain.Post{}, postdomain.ErrNotFound
	}

	return post, nil
}

// withFreeSlug runs write with the first free slug derived from base, retrying with a
// fresh choice when a concurrent write claims it first.
func (s *PostService) withFreeSlug(ctx context.Context, base string, write func(slug string) (postdomain.Post, error)) (postdomain.Post, error) {
	var err error

	for range slugAttempts {
		var slug string

		slug, err = s.freeSlug(ctx, base)
		if err != nil {
			return postdomain.Post{}, err
		}

		var post postdomain.Post

		post, err = write(slug)
		if !isSlugConflict(err) {
			return post, err
		}
	}

	return postdomain.Post{}, err
}

// freeSlug returns base when no live post uses it, otherwise base-2, base-3, … — the
// lowest free suffix, so the same collisions always produce the same slug.
func (s *PostService) freeSlug(ctx context.Context, base string) (string, error) {
	existing, err := s.repo.SlugsWithPrefix(ctx, base)
	if err != nil {
		return "", err
	}

	taken := make(map[string]struct{}, len(existing))
	for _, slug := range existing {
		taken[slug] = struct{}{}
	}

	if _, ok := taken[base]; !ok {
		return base, nil
	}

	for n := 2; ; n++ {
		candidate := withSuffix(base, n)
		if _, ok := taken[candidate]; !ok {
			return candidate, nil
		}
	}
}

func withSuffix(base string, n int) string {
	suffix := "-" + strconv.Itoa(n)
	if len(base)+len(suffix) > postdomain.MaxSlugLength {
		base = strings.TrimRight(base[:postdomain.MaxSlugLength-len(suffix)], "-")
	}

	return base + suffix
}

func isSlugConflict(err error) bool {
	return errors.Is(err, postdomain.ErrConflict) &&
		!errors.Is(err, postdomain.ErrStaleVersion) &&
		!errors.Is(err, postdomain.ErrInvalidTransition)
}

// draftSlug validates an explicit slug, or derives one from the title when raw is blank.
func draftSlug(title, raw string) (string, error) {
	slug := strings.TrimSpace(strings.ToLower(raw))
	if slug == "" {
		slug = slugify(title)
		if len(slug) > postdomain.MaxSlugLength {
			slug = strings.TrimRight(slug[:postdomain.MaxSlugLength], "-")
		}
	}

	if slug == "" {
		return "", fmt.Errorf("%w: slug required when the title has no letters or digits", ErrValidation)
	}

	if len(slug) > postdomain.MaxSlugLength || !slugPattern.MatchString(slug) {
		return "", fmt.Errorf("%w: invalid slug", ErrValidation)
	}

	return slug, nil
}

func slugify(title string) string {
	s := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return r
		case r == ' ' || r == '_' || r == '-':
			return '-'
		default:
			return -1
		}
	}, strings.ToLower(strings.TrimSpace(title)))
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}

	return strings.Trim(s, "-")
}
