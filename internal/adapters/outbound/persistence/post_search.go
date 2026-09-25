package persistence

import (
	"context"
	"errors"
	"fmt"
	"html"
	"regexp"
	"strings"

	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
	"gorm.io/gorm"
)

// Highlight delimiters are Unicode private-use characters so ts_headline output can be
// HTML-escaped before they become <mark> tags.
const (
	markStart = "\uE000"
	markStop  = "\uE001"

	titleHeadline   = "StartSel=" + markStart + ", StopSel=" + markStop + ", HighlightAll=true"
	snippetHeadline = "StartSel=" + markStart + ", StopSel=" + markStop +
		", MaxWords=35, MinWords=15, MaxFragments=2, FragmentDelimiter=\" … \""
)

var (
	searchLanguagePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)
	indexedLanguage       = regexp.MustCompile(`to_tsvector\('([^']+)'::regconfig`)

	// ErrUnknownSearchLanguage means PostgreSQL has no text search configuration by that name.
	ErrUnknownSearchLanguage = errors.New("unknown text search configuration")
)

// PostSearch implements postports.Searcher with PostgreSQL full-text search over
// posts.search_vector.
type PostSearch struct {
	db       *gorm.DB
	language string
}

// NewPostSearch searches with the text search configuration the index was built with;
// pass IndexedSearchLanguage's result so queries and the index always agree.
func NewPostSearch(db *gorm.DB, language string) *PostSearch {
	return &PostSearch{db: db, language: language}
}

type searchHitRow struct {
	PostModel

	SearchRank    float64 `gorm:"column:search_rank"`
	SearchTitle   string  `gorm:"column:search_title"`
	SearchSnippet string  `gorm:"column:search_snippet"`
}

// SearchPublished ranks published posts matching filter.Query with ts_rank (normalized by
// document length), then highlights the page's titles and content with ts_headline.
func (s *PostSearch) SearchPublished(ctx context.Context, filter postdomain.SearchFilter) (postdomain.SearchResult, error) {
	db := conn(ctx, s.db)
	query := "websearch_to_tsquery(?::regconfig, ?)"

	q := db.Model(&PostModel{}).
		Where("status = ? AND deleted_at IS NULL", string(postdomain.StatusPublished)).
		Where("search_vector @@ "+query, s.language, filter.Query)
	if filter.CategoryUUID != nil {
		q = q.Where("category_id = "+idOf("categories"), *filter.CategoryUUID)
	}

	if filter.TagUUID != nil {
		q = q.Where("id IN (SELECT post_id FROM post_tags WHERE tag_id = "+idOf("tags")+")", *filter.TagUUID)
	}

	result := postdomain.SearchResult{Page: filter.Page, PerPage: filter.PerPage}
	if err := q.Count(&result.Total).Error; err != nil {
		return postdomain.SearchResult{}, err
	}

	var page []struct {
		ID   int64
		Rank float64
	}

	if err := q.Select("id, ts_rank(search_vector, "+query+", 1) AS rank", s.language, filter.Query).
		Order("rank DESC, published_at DESC NULLS LAST, id DESC").
		Limit(filter.PerPage).Offset((filter.Page - 1) * filter.PerPage).
		Scan(&page).Error; err != nil {
		return postdomain.SearchResult{}, err
	}

	if len(page) == 0 {
		result.Items = []postdomain.SearchHit{}
		return result, nil
	}

	ids := make([]int64, len(page))
	for i, hit := range page {
		ids[i] = hit.ID
	}

	var rows []searchHitRow
	if err := db.Model(&PostModel{}).
		Select(postColumns+
			", ts_headline(?::regconfig, posts.title, "+query+", ?) AS search_title"+
			", ts_headline(?::regconfig, left(posts.content, 100000), "+query+", ?) AS search_snippet",
			s.language, s.language, filter.Query, titleHeadline,
			s.language, s.language, filter.Query, snippetHeadline).
		Where("posts.id IN ?", ids).
		Find(&rows).Error; err != nil {
		return postdomain.SearchResult{}, err
	}

	byID := make(map[int64]searchHitRow, len(rows))
	for _, row := range rows {
		byID[row.ID] = row
	}

	result.Items = make([]postdomain.SearchHit, 0, len(page))
	for _, hit := range page {
		row, ok := byID[hit.ID]
		if !ok {
			continue
		}

		result.Items = append(result.Items, postdomain.SearchHit{
			Post:    mapPost(row.PostModel),
			Rank:    hit.Rank,
			Title:   markHighlights(row.SearchTitle),
			Snippet: markHighlights(row.SearchSnippet),
		})
	}

	return result, nil
}

// markHighlights HTML-escapes ts_headline output and turns its delimiters into <mark> tags.
func markHighlights(s string) string {
	return strings.NewReplacer(markStart, "<mark>", markStop, "</mark>").Replace(html.EscapeString(s))
}

// IndexedSearchLanguage returns the text search configuration posts.search_vector was built
// with.
func IndexedSearchLanguage(ctx context.Context, db *gorm.DB) (string, error) {
	var expr string

	err := conn(ctx, db).Raw(`SELECT pg_get_expr(d.adbin, d.adrelid)
		FROM pg_attrdef d
		JOIN pg_attribute a ON a.attrelid = d.adrelid AND a.attnum = d.adnum
		WHERE d.adrelid = 'posts'::regclass AND a.attname = 'search_vector'`).Scan(&expr).Error
	if err != nil {
		return "", err
	}

	match := indexedLanguage.FindStringSubmatch(expr)
	if match == nil {
		return "", errors.New("posts.search_vector is missing; run the migrations")
	}

	return match[1], nil
}

// RebuildPostSearchIndex recreates posts.search_vector and its index for language in one
// transaction. It rewrites the posts table under an exclusive lock.
func RebuildPostSearchIndex(ctx context.Context, db *gorm.DB, language string) error {
	if !searchLanguagePattern.MatchString(language) {
		return fmt.Errorf("%w: %q", ErrUnknownSearchLanguage, language)
	}

	return conn(ctx, db).Transaction(func(tx *gorm.DB) error {
		var n int64
		if err := tx.Raw("SELECT count(*) FROM pg_ts_config WHERE cfgname = ?", language).Scan(&n).Error; err != nil {
			return err
		}

		if n == 0 {
			return fmt.Errorf("%w: %q", ErrUnknownSearchLanguage, language)
		}

		for _, statement := range []string{
			`DROP INDEX IF EXISTS posts_search_vector_idx`,
			`ALTER TABLE posts DROP COLUMN IF EXISTS search_vector`,
			`ALTER TABLE posts ADD COLUMN search_vector tsvector GENERATED ALWAYS AS (` + searchVectorExpr(language) + `) STORED`,
			`CREATE INDEX posts_search_vector_idx ON posts USING gin (search_vector)`,
		} {
			if err := tx.Exec(statement).Error; err != nil {
				return fmt.Errorf("rebuild search index: %w", err)
			}
		}

		return nil
	})
}

// searchVectorExpr must match migration 00028 for 'simple'. language is validated, so it is
// safe to inline.
func searchVectorExpr(language string) string {
	cfg := "'" + language + "'::regconfig"

	return "setweight(to_tsvector(" + cfg + ", coalesce(title, '')), 'A') || " +
		"setweight(to_tsvector(" + cfg + ", coalesce(excerpt, '')), 'B') || " +
		"setweight(to_tsvector(" + cfg + ", left(content, 100000)), 'C')"
}
