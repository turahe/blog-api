package pagination

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Direction describes whether a sort column is ordered ascending or descending.
type Direction uint8

const (
	// Asc corresponds to SQL ASC; lower values appear first.
	Asc Direction = iota
	// Desc corresponds to SQL DESC; higher values appear first.
	Desc
)

// NullsOrder describes how NULLs in a sort column should be placed relative
// to non-null values. Only Postgres NULLS FIRST/LAST are supported.
type NullsOrder uint8

const (
	// NullsDefault uses the database default (ASC → NULLS LAST on Postgres).
	NullsDefault NullsOrder = iota
	// NullsFirst maps to Postgres NULLS FIRST.
	NullsFirst
	// NullsLast maps to Postgres NULLS LAST.
	NullsLast
)

// ColumnType describes the runtime Go type of a sort field. ValidateCursor
// uses this to type-check decoded cursor fields.
type ColumnType uint8

const (
	// TypeInt64 covers bigint primary keys, counters, revision numbers.
	TypeInt64 ColumnType = iota
	// TypeString covers text/UUID/slug columns decoded as strings.
	TypeString
	// TypeUUID covers google/uuid.UUID columns.
	TypeUUID
	// TypeTime covers time.Time (created_at, published_at, occurred_at, …).
	TypeTime
	// TypeFloat64 covers double-precision float columns (search rank, scores).
	TypeFloat64
)

// SortField is one column in an endpoint's ORDER BY composite. Order in the
// slice matters: the first field is the primary sort. The last field MUST be
// a unique column (typically the bigint ID) so ties cannot occur.
type SortField struct {
	// Name is the logical (JSON) field name used inside cursor payloads.
	// Also the default DB column name unless Column is overridden.
	Name string
	// Column, when set, overrides the database column expression used in
	// BuildSeek. Useful for qualified names like "p.published_at".
	Column string
	Dir    Direction
	Nulls  NullsOrder
	Type   ColumnType
}

// CursorConfig declares the shape of one paginated endpoint. Passed to
// ParseRequest, EncodeCursor, DecodeCursor, and the seek-SQL helpers.
type CursorConfig struct {
	// Kind scopes cursors to a single endpoint family. Set to a stable,
	// unique string like "posts_public" or "comments_newest".
	Kind string
	// Sort is the ORDER BY composite, primary column first, unique tiebreak
	// last (typically id). Length must be >= 1.
	Sort []SortField
	// TTL is the maximum age of a cursor before it is rejected.
	// Zero means "use DefaultTTL (24h)".
	TTL time.Duration
	// MaxPerPage is the upper clamp for limit/perPage. Values above this
	// are capped. Defaults to 100 when zero.
	MaxPerPage int
	// DefaultPerPage is the limit used when the request omits one.
	// Defaults to 20 when zero.
	DefaultPerPage int
	// OffsetModeOnly disables cursor mode entirely (used for rank-ordered
	// full-text search if the endpoint opts out of keyset). Requests with
	// after/before return ErrCursorUnsupported.
	OffsetModeOnly bool
}

// DefaultTTL is the cursor expiry used when CursorConfig.TTL is zero.
const DefaultTTL = 24 * time.Hour

// DefaultMaxPerPage is the clamp when CursorConfig.MaxPerPage is zero.
const DefaultMaxPerPage = 100

// DefaultPerPage is the fallback limit when CursorConfig.DefaultPerPage is zero.
const DefaultPerPage = 20

// Mode selects which pagination algorithm ParseRequest selected.
type Mode uint8

const (
	// ModeCursor indicates an after or before cursor was supplied (or first
	// page cursor mode: no cursor at all).
	ModeCursor Mode = iota
	// ModeOffset indicates the legacy page/perPage request path.
	ModeOffset
)

// PageRequest is the canonical pagination descriptor returned by ParseRequest
// and consumed by every repository method. Repository methods should embed
// PageRequest inside their domain filter struct (tagged `json:"-"`) rather
// than adding new parameters.
type PageRequest struct {
	Mode         Mode
	Forward      bool
	Limit        int
	Cursor       string
	Offset       int
	Page         int
	IncludeTotal bool
}

// PageResult is the unified shape returned by every paginated repository
// method after migration. Callers typically map this onto their existing
// domain ListResult struct via a small adapter or by embedding.
//
// Total is nil when IncludeTotal was false, allowing repositories to skip
// the (expensive) COUNT step.
type PageResult[T any] struct {
	Items           []T
	Total           *int64
	UnreadTotal     *int64
	HasNextPage     bool
	HasPreviousPage bool
	NextCursor      string
	PreviousCursor  string
	Limit           int
	// OffsetPage / OffsetPerPage are populated only for ModeOffset requests;
	// lets the legacy envelope writer compute currentPage / lastPage / from / to.
	OffsetPage    int
	OffsetPerPage int
}

// SortValues extracts the sort field values from a row using a user-provided
// accessor. Helper for repository code that wants to encode first/last-row
// cursors without hand-writing maps for every endpoint.
//
// fn(row, fieldIndex) must return the runtime value for the i-th sort field
// declared in cfg.Sort. Returned types must match cfg.Sort[i].Type exactly:
// TypeInt64→int64, TypeString→string, TypeUUID→uuid.UUID, TypeTime→time.Time.
func SortValues[T any](cfg CursorConfig, row T, fn func(T, int) any) map[string]any {
	out := make(map[string]any, len(cfg.Sort))
	for i, f := range cfg.Sort {
		v := fn(row, i)
		out[f.Name] = normalizeSortValue(f.Type, v)
	}
	return out
}

// normalizeSortValue accepts common loose types (e.g. int for TypeInt64) and
// produces the canonical runtime type stored in cursor payloads so that
// ValidateCursor's type check is deterministic.
func normalizeSortValue(t ColumnType, v any) any {
	switch t {
	case TypeInt64:
		switch n := v.(type) {
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
		case float64:
			if n == float64(int64(n)) {
				return int64(n)
			}
		case json.Number:
			if i, err := n.Int64(); err == nil {
				return i
			}
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
		case json.Number:
			if f, err := n.Float64(); err == nil {
				return f
			}
		}
	case TypeString:
		if s, ok := v.(string); ok {
			return s
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
	case TypeTime:
		if tm, ok := v.(time.Time); ok {
			return tm.UTC()
		}
		if s, ok := v.(string); ok {
			if tm, err := time.Parse(time.RFC3339Nano, s); err == nil {
				return tm.UTC()
			}
			if tm, err := time.Parse(time.RFC3339, s); err == nil {
				return tm.UTC()
			}
		}
	}
	return v
}
