package service

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
	"github.com/turahe/blog-api/internal/core/post/ports"
	"github.com/turahe/blog-api/internal/core/readcache"
	"github.com/turahe/blog-api/internal/shared/pagination"
)

// searchListCfg is the CursorConfig used for normalising legacy offset
// params in the post search list. Must match the repo/handler configs.
// Sort: search rank DESC, published_at DESC NULLS LAST, id DESC provides
// deterministic ordering required for keyset (cursor) pagination.
var searchListCfg = pagination.CursorConfig{
	Kind: "posts_search",
	Sort: []pagination.SortField{
		{Name: "rank", Dir: pagination.Desc, Type: pagination.TypeFloat64},
		{Name: "published_at", Dir: pagination.Desc, Nulls: pagination.NullsLast, Type: pagination.TypeTime},
		{Name: "id", Dir: pagination.Desc, Type: pagination.TypeInt64},
	},
	TTL:            pagination.DefaultTTL,
	MaxPerPage:     pagination.DefaultMaxPerPage,
	DefaultPerPage: pagination.DefaultPerPage,
}

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

	if filter.Mode == pagination.ModeOffset || filter.Page > 0 || filter.Page < 1 || filter.Limit < 1 {
		norm := pagination.ParseLegacy(searchListCfg, filter.Page, filter.Limit)
		filter.PageRequest = norm
	}

	key := readcache.Key("search", "q", strings.ToLower(filter.Query), "cursor", filter.Cursor,
		"mode", filter.Mode, "page", filter.Page, "per_page", filter.Limit,
		"include_total", filter.IncludeTotal, "category", filter.CategoryUUID, "tag", filter.TagUUID)

	return readcache.Through(ctx, s.cache, readcache.Posts, key, func() (postdomain.SearchResult, error) {
		return s.search.SearchPublished(ctx, filter)
	})
}
