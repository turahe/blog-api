package service

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
	"github.com/turahe/blog-api/internal/core/post/ports"
	"github.com/turahe/blog-api/internal/core/readcache"
)

// WithSearch enables full-text search over published posts.
func (s *PostService) WithSearch(searcher ports.Searcher) *PostService {
	s.search = searcher
	return s
}

// Search returns published posts matching filter.Query, best match first. Whitespace in the
// query is collapsed; an empty or overlong query returns ErrValidation.
func (s *PostService) Search(ctx context.Context, filter postdomain.SearchFilter) (postdomain.SearchResult, error) {
	if s.search == nil {
		return postdomain.SearchResult{}, postdomain.ErrSearchUnavailable
	}

	filter.Query = strings.Join(strings.Fields(filter.Query), " ")
	if filter.Query == "" {
		return postdomain.SearchResult{}, fmt.Errorf("%w: q must not be empty", ErrValidation)
	}

	if utf8.RuneCountInString(filter.Query) > postdomain.MaxSearchQueryLength {
		return postdomain.SearchResult{}, fmt.Errorf("%w: q must be at most %d characters", ErrValidation, postdomain.MaxSearchQueryLength)
	}

	if filter.Page < 1 {
		filter.Page = 1
	}

	if filter.PerPage < 1 || filter.PerPage > 100 {
		filter.PerPage = 20
	}

	key := readcache.Key("search", "q", strings.ToLower(filter.Query), "page", filter.Page,
		"per_page", filter.PerPage, "category", filter.CategoryUUID, "tag", filter.TagUUID)

	return readcache.Through(ctx, s.cache, readcache.Posts, key, func() (postdomain.SearchResult, error) {
		return s.search.SearchPublished(ctx, filter)
	})
}
