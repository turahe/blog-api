package pagination

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
)

// SeekResult is the return shape of BuildSeek. Repositories apply
// WhereClause (prepared string with ? placeholders) + BindVars + OrderClause
// + Limit on their GORM query or raw SQL.
type SeekResult struct {
	// WhereClause is a SQL WHERE fragment (no leading WHERE) containing the
	// row-value comparison using ? placeholders. Empty string on first-page
	// (empty cursor) cursor requests.
	WhereClause string
	// BindVars matches the ? placeholders in WhereClause in declaration order.
	BindVars []any
	// OrderClause is the full ORDER BY clause for the requested direction.
	// For backward seek (before=Y) the directions of each sort column are
	// reversed so the LIMIT+1 fetch walks the set backwards; the caller
	// must reverse the result slice to restore display order.
	OrderClause string
	// LimitFetch is Limit+1 (the +1 row lets us detect hasNextPage /
	// hasPreviousPage without a separate COUNT probe).
	LimitFetch int
	// ReverseDisplay is true for backward seeks (before=Y): the caller
	// must reverse the Items slice before emitting to clients.
	ReverseDisplay bool
}

// BuildSeek turns a PageRequest + CursorConfig into concrete SQL fragments
// the repository can apply. cfg.Sort drives ORDER BY, comparator direction,
// and bind-var order.
//
// For cursor-bearing requests the WHERE fragment uses Postgres row-value
// comparisons: `(a, b, id) < (?, ?, ?)` for DESC, or `(a, b, id) > (?, ?, ?)`
// for ASC. Row-value syntax is equivalent to the long-hand AND/OR expansion
// and Postgres optimises it identically.
//
// For ModeOffset requests, a no-op SeekResult is returned with
// OrderClause set so the caller can still apply LIMIT/OFFSET on top.
func BuildSeek(cfg CursorConfig, pr PageRequest, cursorFields map[string]any) (SeekResult, error) {
	if len(cfg.Sort) == 0 {
		return SeekResult{}, fmt.Errorf("pagination: BuildSeek called with empty Sort list")
	}
	// For offset mode the display order is the natural endpoint order (do
	// not flip via pr.Forward — that flag is meaningful only for cursor
	// forward/backward seeking).
	forward := pr.Forward
	if pr.Mode == ModeOffset {
		forward = true
	}
	order := orderFor(cfg.Sort, forward)
	limit := pr.Limit
	if limit < 1 {
		limit = DefaultPerPage
	}
	fetchLimit := limit + 1
	res := SeekResult{
		OrderClause: order,
		LimitFetch:  fetchLimit,
	}
	switch pr.Mode {
	case ModeOffset:
		return res, nil
	case ModeCursor:
		// empty cursor = first page: no WHERE seek
		if pr.Cursor == "" {
			return res, nil
		}
		if !pr.Forward {
			res.ReverseDisplay = true
		}
		if len(cursorFields) == 0 {
			return res, nil
		}
		// Build (col1, col2, ..., colN) <op> (?, ?, ..., ?)
		cols := make([]string, 0, len(cfg.Sort))
		bind := make([]any, 0, len(cfg.Sort))
		for _, f := range cfg.Sort {
			col := f.Column
			if col == "" {
				col = f.Name
			}
			cols = append(cols, col)
			bind = append(bind, coerceBind(f, cursorFields[f.Name]))
		}
		op := comparatorFor(cfg.Sort, pr.Forward)
		res.WhereClause = fmt.Sprintf("(%s) %s (%s)",
			strings.Join(cols, ", "),
			op,
			strings.TrimSuffix(strings.Repeat("?, ", len(cols)), ", "))
		res.BindVars = bind
		return res, nil
	}
	return res, nil
}

// comparatorFor returns the row-value operator applied to the *display* sort
// order. For display ORDER BY col DESC we fetch "rows strictly before the
// cursor", i.e. (col, id) < (cursor). For ASC we use >. When we run a
// *backward seek* (before=Y, pr.Forward=false) we flip the direction of the
// seek comparison (since we want rows strictly AFTER the cursor in display
// order, i.e. the preceding page). Callers see only a single bool
// `ReverseDisplay` that flips memory order after fetch.
func comparatorFor(sortFields []SortField, forward bool) string {
	// The first sort field determines the over-all "sense" (rows are
	// considered "increasing" in ASC, "decreasing" in DESC). Row-value
	// comparison semantics are applied once to the tuple, matching the
	// per-column display order of the sort list uniformly:
	//   For forward seek on a DESC-descending list — (tuple) < (cursor tuple)
	//   For forward seek on an ASC-ascending list  — (tuple) > (cursor tuple)
	//   For backward seek — invert.
	first := sortFields[0]
	sense := first.Dir
	if !forward {
		if sense == Asc {
			sense = Desc
		} else {
			sense = Asc
		}
	}
	if sense == Desc {
		return "<"
	}
	return ">"
}

// orderFor builds the ORDER BY clause. For backward (before=Y) seeks we
// reverse every column direction so the database walks the preceding page
// in reverse, then we flip the result slice in memory. Column names come
// from SortField.Column, falling back to Name.
func orderFor(sortFields []SortField, forward bool) string {
	parts := make([]string, 0, len(sortFields))
	for _, f := range sortFields {
		col := f.Column
		if col == "" {
			col = f.Name
		}
		dir := f.Dir
		nulls := f.Nulls
		if !forward {
			if dir == Asc {
				dir = Desc
			} else {
				dir = Asc
			}
			if nulls == NullsFirst {
				nulls = NullsLast
			} else if nulls == NullsLast {
				nulls = NullsFirst
			}
		}
		part := col
		if dir == Desc {
			part += " DESC"
		} else {
			part += " ASC"
		}
		switch nulls {
		case NullsFirst:
			part += " NULLS FIRST"
		case NullsLast:
			part += " NULLS LAST"
		case NullsDefault:
		}
		parts = append(parts, part)
	}
	return "ORDER BY " + strings.Join(parts, ", ")
}

// coerceBind returns the DB-compatible bind variable for a declared
// SortField / raw cursor value. JSON decoding turns ints into float64; this
// helper restores int64.
func coerceBind(f SortField, v any) any {
	switch f.Type {
	case TypeInt64:
		switch n := v.(type) {
		case float64:
			return int64(n)
		case int:
			return int64(n)
		case int32:
			return int64(n)
		case int64:
			return n
		case uint:
			return int64(n)
		case uint32:
			return int64(n)
		case uint64:
			return int64(n)
		}
	case TypeFloat64:
		switch n := v.(type) {
		case float32:
			return float64(n)
		case float64:
			return n
		case int:
			return float64(n)
		case int32:
			return float64(n)
		case int64:
			return float64(n)
		case uint:
			return float64(n)
		case uint32:
			return float64(n)
		case uint64:
			return float64(n)
		}
	case TypeTime:
		if tm, ok := v.(time.Time); ok {
			return tm.UTC()
		}
		if s, ok := v.(string); ok {
			if tm, err := time.Parse(time.RFC3339Nano, s); err == nil {
				return tm.UTC()
			}
		}
	case TypeUUID:
		if u, ok := v.(uuid.UUID); ok {
			return u
		}
		if s, ok := v.(string); ok {
			if u, err := uuid.Parse(s); err == nil {
				return u
			}
		}
	case TypeString:
		return v
	default:
	}
	return v
}

// TruncatePage takes items fetched with LIMIT limit+1 and returns the page
// (truncated to limit) plus hasNextPage / hasPreviousPage flags. The
// ReverseDisplay flag (returned by BuildSeek for before=Y) flips item order
// BEFORE truncation so the page comes out in the display direction.
//
// Semantics:
//   - Forward cursor + 1st page + len(items)==limit+1 → hasNextPage=true, hasPreviousPage=false
//   - Forward cursor + len(items)==limit+1      → hasNextPage=true, hasPreviousPage=true (we came from `after`)
//   - Backward cursor + len(items)==limit+1     → hasPreviousPage=true, hasNextPage=true
//   - Last page (len < limit+1)                 → hasNextPage for forward = false; hasPreviousPage for backward = false
func TruncatePage[T any](items []T, limit int, forward, hadCursor bool) (page []T, hasNextPage, hasPreviousPage bool) {
	if limit < 1 {
		limit = DefaultPerPage
	}

	excess := len(items) > limit

	if !forward {
		slices.Reverse(items)
	}

	if excess {
		if forward {
			// Truncate to limit, drop the +1 peek row at the end.
			page = slices.Clone(items[:limit])
			hasNextPage = true
			hasPreviousPage = hadCursor
		} else {
			// Backward: the +1 peek row is at position 0 after reversal; drop it.
			page = slices.Clone(items[1 : limit+1])
			hasPreviousPage = true
			hasNextPage = hadCursor
		}

		return page, hasNextPage, hasPreviousPage
	}

	// No excess — we are at the boundary in the direction of travel.
	page = slices.Clone(items)

	if forward {
		hasNextPage = false
		hasPreviousPage = hadCursor
	} else {
		hasPreviousPage = false
		hasNextPage = hadCursor
	}

	return page, hasNextPage, hasPreviousPage
}

// ExtractLimit returns the "limit" that should be applied to the SQL query
// given a PageRequest. For offset mode this is simply pr.Limit; for cursor
// mode this is limit+1. Kept so repositories don't repeat the arithmetic.
func ExtractLimit(pr PageRequest) int {
	limit := pr.Limit
	if limit < 1 {
		limit = DefaultPerPage
	}

	if pr.Mode == ModeOffset {
		return limit
	}

	return limit + 1
}
