package persistence

import (
	"context"
	"errors"
	"fmt"
	"html"
	"regexp"
	"strings"

	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
	"github.com/turahe/blog-api/internal/shared/pagination"
	"gorm.io/gorm"
)

// postSearchCfg declares pagination bounds for the post search list.
// Sort: search rank DESC, published_at DESC NULLS LAST, id DESC provides
// deterministic ordering required for keyset (cursor) pagination. Rank is
// computed via ts_rank; the seek SQL uses the identical expression for
// row-value comparisons.
var postSearchCfg = pagination.CursorConfig{
	Kind: "posts_search",
	Sort: []pagination.SortField{
		{Name: "rank", Dir: pagination.Desc, Type: pagination.TypeFloat64},
		{Name: "published_at", Column: "posts.published_at", Dir: pagination.Desc, Nulls: pagination.NullsLast, Type: pagination.TypeTime},
		{Name: "id", Column: "posts.id", Dir: pagination.Desc, Type: pagination.TypeInt64},
	},
	TTL:            pagination.DefaultTTL,
	MaxPerPage:     pagination.DefaultMaxPerPage,
	DefaultPerPage: pagination.DefaultPerPage,
}

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

// rankExpr returns the ts_rank expression and bind vars used for both the
// ORDER BY and row-value seek comparison. Keeping the expression in one
// place guarantees the cursor comparison and sort order never diverge.
func rankExpr(language string) (string, []any) {
	expr := "ts_rank(posts.search_vector, websearch_to_tsquery(?::regconfig, ?), 1)"
	return expr, []any{language, language}
}

// SearchPublished ranks published posts matching filter.Query with ts_rank (normalized by
// document length), then highlights the page's titles and content with ts_headline.
func (s *PostSearch) SearchPublished(ctx context.Context, filter postdomain.SearchFilter) (postdomain.SearchResult, error) {
	pr := filter.PageRequest
	if pr.Mode == pagination.ModeOffset || pr.Page > 0 || pr.Page < 1 || pr.Limit < 1 {
		norm := pagination.ParseLegacy(postSearchCfg, pr.Page, pr.Limit)
		pr = norm
	}

	var cursorFields map[string]any
	if pr.Mode == pagination.ModeCursor && pr.Cursor != "" {
		decoded, _, err := pagination.DecodeCursor(postSearchCfg, pr.Cursor)
		if err != nil {
			return postdomain.SearchResult{}, err
		}
		if err := pagination.ValidateCursor(postSearchCfg, decoded); err != nil {
			return postdomain.SearchResult{}, err
		}
		cursorFields = decoded
	}

	db := conn(ctx, s.db)
	query := "websearch_to_tsquery(?::regconfig, ?)"
	rankExp, rankBinds := rankExpr(s.language)
	qBinds := []any{s.language, filter.Query}

	q := db.Model(&PostModel{}).
		Where("status = ? AND deleted_at IS NULL", string(postdomain.StatusPublished)).
		Where("search_vector @@ "+query, qBinds...)
	if filter.CategoryUUID != nil {
		q = q.Where("category_id = "+idOf("categories"), *filter.CategoryUUID)
	}

	if filter.TagUUID != nil {
		q = q.Where("id IN (SELECT post_id FROM post_tags WHERE tag_id = "+idOf("tags")+")", *filter.TagUUID)
	}

	var result postdomain.SearchResult
	result.Limit = pr.Limit
	if pr.Mode == pagination.ModeOffset {
		result.OffsetPage = pr.Page
		result.OffsetPerPage = pr.Limit
	}

	if pr.IncludeTotal {
		var total int64
		if err := q.Count(&total).Error; err != nil {
			return postdomain.SearchResult{}, err
		}
		result.Total = &total
	}

	forward := pr.Forward
	if pr.Mode == pagination.ModeOffset {
		forward = true
	}
	orderDir := func(dir pagination.Direction, fwd bool) string {
		d := dir
		if !fwd {
			if d == pagination.Asc {
				d = pagination.Desc
			} else {
				d = pagination.Asc
			}
		}
		if d == pagination.Desc {
			return "DESC"
		}
		return "ASC"
	}
	nullsDir := func(nulls pagination.NullsOrder, fwd bool) string {
		n := nulls
		if !fwd {
			if n == pagination.NullsFirst {
				n = pagination.NullsLast
			} else if n == pagination.NullsLast {
				n = pagination.NullsFirst
			}
		}
		switch n {
		case pagination.NullsFirst:
			return "NULLS FIRST"
		case pagination.NullsLast:
			return "NULLS LAST"
		case pagination.NullsDefault:
			return ""
		default:
			return ""
		}
	}

	reverseDisplay := !forward && pr.Mode == pagination.ModeCursor
	orderClause := fmt.Sprintf("ORDER BY %s %s, posts.published_at %s %s, posts.id %s",
		rankExp, orderDir(pagination.Desc, forward),
		orderDir(pagination.Desc, forward), nullsDir(pagination.NullsLast, forward),
		orderDir(pagination.Desc, forward))

	selectBinds := append([]any{}, rankBinds...)
	selectBinds = append(selectBinds, qBinds...)

	var whereSeekBinds []any
	if pr.Mode == pagination.ModeCursor && cursorFields != nil {
		op := "<"
		if !forward {
			op = ">"
		}
		rankVal := cursorFields["rank"]
		publishedVal := cursorFields["published_at"]
		idVal := cursorFields["id"]
		whereSeekBinds = []any{rankVal, publishedVal, idVal}
		seekBinds := append([]any{}, rankBinds...)
		seekBinds = append(seekBinds, whereSeekBinds...)
		q = q.Where(fmt.Sprintf("(%s, posts.published_at, posts.id) %s (?, ?, ?)", rankExp, op), seekBinds...)
	}

	fetchLimit := pr.Limit + 1
	rankSelectBinds := append([]any{}, rankBinds...)
	rankSelectBinds = append(rankSelectBinds, qBinds...)

	var page []struct {
		ID   int64
		Rank float64
	}

	rankQuery := q.Select(fmt.Sprintf("posts.id, %s AS rank", rankExp), rankSelectBinds...).
		Order(orderClause).
		Limit(fetchLimit)
	if pr.Mode == pagination.ModeOffset {
		rankQuery = rankQuery.Offset(pr.Offset)
	}
	if err := rankQuery.Scan(&page).Error; err != nil {
		return postdomain.SearchResult{}, err
	}

	if reverseDisplay {
		for i, j := 0, len(page)-1; i < j; i, j = i+1, j-1 {
			page[i], page[j] = page[j], page[i]
		}
	}

	truncatedItems, hasNext, hasPrev := pagination.TruncatePage(page, pr.Limit, forward, pr.Cursor != "")
	result.HasNextPage = hasNext
	result.HasPreviousPage = hasPrev

	if len(truncatedItems) == 0 {
		result.Items = []postdomain.SearchHit{}
		return result, nil
	}

	ids := make([]int64, len(truncatedItems))
	for i, hit := range truncatedItems {
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

	result.Items = make([]postdomain.SearchHit, 0, len(truncatedItems))
	for _, hit := range truncatedItems {
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

	if len(result.Items) > 0 {
		if result.HasNextPage {
			last := result.Items[len(result.Items)-1]
			fields := pagination.SortValues[postdomain.SearchHit](postSearchCfg, last,
				func(row postdomain.SearchHit, i int) any {
					switch i {
					case 0:
						return row.Rank
					case 1:
						return row.Post.PublishedAt
					case 2:
						return row.Post.ID
					}
					return nil
				})
			cur, err := pagination.EncodeCursor(postSearchCfg, fields)
			if err != nil {
				return postdomain.SearchResult{}, err
			}
			result.NextCursor = cur
		}
		if result.HasPreviousPage {
			first := result.Items[0]
			fields := pagination.SortValues[postdomain.SearchHit](postSearchCfg, first,
				func(row postdomain.SearchHit, i int) any {
					switch i {
					case 0:
						return row.Rank
					case 1:
						return row.Post.PublishedAt
					case 2:
						return row.Post.ID
					}
					return nil
				})
			cur, err := pagination.EncodeCursor(postSearchCfg, fields)
			if err != nil {
				return postdomain.SearchResult{}, err
			}
			result.PreviousCursor = cur
		}
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
