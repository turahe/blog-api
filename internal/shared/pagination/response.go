package pagination

import (
	nethttp "net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// PageLinks is the standard navigational URL block used on paginated
// responses. Mirrors the shape of the existing http/responses PageLinks so
// envelope writers can convert between them easily. Null when unavailable.
type PageLinks struct {
	First *string `json:"first,omitempty"`
	Last  *string `json:"last,omitempty"`
	Prev  *string `json:"prev,omitempty"`
	Next  *string `json:"next,omitempty"`
}

// MetaForResult builds cursor-mode pagination metadata for a PageResult.
// The envelope writer merges this onto PaginationMeta so callers get both
// legacy and cursor fields on a single response object.
//
//   - When result.Total is nil (includeTotal=false) we omit total, lastPage,
//     from, to, first and last.
//   - When OffsetMode: returns canonical PaginationMeta values using legacy
//     result.OffsetPage / OffsetPerPage fields.
type CursorMeta struct {
	Limit           int     `json:"limit,omitempty"`
	HasNextPage     *bool   `json:"hasNextPage,omitempty"`
	HasPreviousPage *bool   `json:"hasPreviousPage,omitempty"`
	NextCursor      *string `json:"nextCursor,omitempty"`
	PreviousCursor  *string `json:"previousCursor,omitempty"`
	RequestID       string  `json:"requestId,omitempty"`
}

// LegacyMeta returns a struct matching the current envelope's PaginationMeta
// fields (currentPage/lastPage/perPage/from/to/total). For cursor mode,
// Total-driven fields are only populated when result.Total != nil.
type LegacyMeta struct {
	CurrentPage int    `json:"currentPage"`
	From        *int   `json:"from,omitempty"`
	LastPage    int    `json:"lastPage,omitempty"`
	Path        string `json:"path"`
	PerPage     int    `json:"perPage"`
	To          *int   `json:"to,omitempty"`
	Total       *int64 `json:"total,omitempty"`
	RequestID   string `json:"requestId,omitempty"`
}

// BuildMeta splits a PageResult into (legacy, cursor) meta structs, both
// populated according to the rules above.
//
// For cursor mode with Total == nil, LegacyMeta still has CurrentPage (0)
// but From/To/LastPage/Total remain nil — envelope writer should skip
// emitting those fields (using omitempty tags above).
func BuildMeta[T any](c *gin.Context, r PageResult[T]) (LegacyMeta, CursorMeta) {
	path := requestPath(c)
	rid, _ := c.Get("request_id")
	requestID, _ := rid.(string)
	legacy := LegacyMeta{
		Path:      path,
		RequestID: requestID,
	}
	cursor := CursorMeta{
		RequestID: requestID,
		Limit:     r.Limit,
	}
	if r.Limit > 0 {
		legacy.PerPage = r.Limit
	}
	t := true
	f := false
	if r.HasNextPage {
		cursor.HasNextPage = &t
	} else {
		cursor.HasNextPage = &f
	}
	if r.HasPreviousPage {
		cursor.HasPreviousPage = &t
	} else {
		cursor.HasPreviousPage = &f
	}
	if r.NextCursor != "" {
		s := r.NextCursor
		cursor.NextCursor = &s
	}
	if r.PreviousCursor != "" {
		s := r.PreviousCursor
		cursor.PreviousCursor = &s
	}
	switch {
	case r.OffsetPage > 0:
		page := r.OffsetPage
		perPage := r.OffsetPerPage
		if perPage <= 0 {
			perPage = r.Limit
		}
		legacy.CurrentPage = page
		legacy.PerPage = perPage
		if r.Total != nil {
			total := *r.Total
			legacy.Total = &total
			lastPage := int((total + int64(perPage) - 1) / int64(perPage))
			if lastPage < 1 {
				lastPage = 1
			}
			legacy.LastPage = lastPage
			if total > 0 {
				from := (page-1)*perPage + 1
				to := page * perPage
				if int64(to) > total {
					to = int(total)
				}
				legacy.From = &from
				legacy.To = &to
			}
		} else {
			legacy.CurrentPage = page
		}
	case r.Total != nil:
		// Cursor-mode includeTotal=true: provide synthetic legacy fields so
		// old integrations that only read currentPage/lastPage still work.
		// currentPage: not well-defined; emit 0 (omitempty leaves it out if
		// we rely on the zero value — but the type is int so we can't omit
		// it; callers will simply see 0 which is safe because they know
		// from the presence of nextCursor that cursor mode is active).
		total := *r.Total
		perPage := r.Limit
		if perPage <= 0 {
			perPage = DefaultPerPage
		}
		legacy.Total = &total
		lastPage := int((total + int64(perPage) - 1) / int64(perPage))
		if lastPage < 1 {
			lastPage = 1
		}
		legacy.LastPage = lastPage
		legacy.CurrentPage = 0 // cursor mode — no page number; left as a sentinel
		if total > 0 {
			// approximate from/to based on hasPreviousPage (unknown exact row
			// index without additional queries); omit to keep API honest.
		}
	}
	return legacy, cursor
}

// BuildLinks returns first/last/prev/next URLs given the PageResult.
// Preserves every existing query parameter (filters, includeTotal) and
// replaces only the pagination key:
//   - Cursor mode: next URL uses ?after=<nextCursor>; prev uses ?before=<previousCursor>
//   - Offset mode: mutates ?page=<num>
//
// For includeTotal=false (Total==nil) both first and last are nil (FR4.2).
func BuildLinks[T any](c *gin.Context, r PageResult[T], legacy LegacyMeta) PageLinks {
	links := PageLinks{}
	pageCursorNotEmpty := r.NextCursor != "" || r.PreviousCursor != ""
	if r.Total != nil {
		if r.Mode() == ModeOffset && legacy.LastPage >= 1 {
			first := absoluteURL(c, withPage(1))
			last := absoluteURL(c, withPage(legacy.LastPage))
			links.First = &first
			links.Last = &last
		}
	}
	switch r.Mode() {
	case ModeOffset:
		if legacy.CurrentPage > 1 {
			s := absoluteURL(c, withPage(legacy.CurrentPage-1))
			links.Prev = &s
		}
		if legacy.LastPage >= 1 && legacy.CurrentPage < legacy.LastPage {
			s := absoluteURL(c, withPage(legacy.CurrentPage+1))
			links.Next = &s
		}
		return links
	case ModeCursor:
		if r.NextCursor != "" && r.HasNextPage {
			s := absoluteURL(c, func(v url.Values) {
				v.Set("after", r.NextCursor)
				v.Set("limit", strconv.Itoa(r.Limit))
				setIncludeTotal(v, r.Total != nil)
			})
			links.Next = &s
		}
		if r.PreviousCursor != "" && r.HasPreviousPage {
			s := absoluteURL(c, func(v url.Values) {
				v.Set("before", r.PreviousCursor)
				v.Set("limit", strconv.Itoa(r.Limit))
				setIncludeTotal(v, r.Total != nil)
			})
			links.Prev = &s
		}
	default:
	}
	_ = pageCursorNotEmpty
	return links
}

// Mode reports the pagination mode of the result. Determined by whether the
// OffsetPage field indicates offset mode vs. cursor mode (OffsetPage == 0).
func (r PageResult[T]) Mode() Mode {
	if r.OffsetPage > 0 {
		return ModeOffset
	}
	return ModeCursor
}

// absoluteURL builds the current request URL with query parameters mutated by
// the provided modifier (which replaces pagination-related keys, preserving
// filters). All existing pagination keys (after/before/page/limit/perPage/
// includeTotal) are cleared BEFORE mutate runs so old cursor values never
// leak into newly generated next/prev/first/last URLs.
func absoluteURL(c *gin.Context, mutate func(url.Values)) string {
	req := c.Request
	u := &url.URL{}
	proto := req.Header.Get("X-Forwarded-Proto")
	if proto == "" {
		if req.TLS != nil {
			proto = "https"
		} else {
			proto = "http"
		}
	}
	u.Scheme = proto
	host := req.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = req.Host
	}
	u.Host = host
	u.Path = req.URL.Path
	q := req.URL.Query()
	for _, k := range []string{"after", "before", "page", "limit", "perPage", "includeTotal"} {
		q.Del(k)
	}
	mutate(q)
	u.RawQuery = q.Encode()
	return u.String()
}

// requestPath returns just the path portion of the URL for Meta.Path.
func requestPath(c *gin.Context) string {
	return c.Request.URL.Path
}

func withPage(n int) func(url.Values) {
	return func(v url.Values) {
		v.Set("page", strconv.Itoa(n))
	}
}

func setIncludeTotal(v url.Values, include bool) {
	if include {
		v.Set("includeTotal", "true")
		return
	}
	v.Set("includeTotal", "false")
}

// PtrTo is a tiny generic helper for setting *bool / *string / *int fields
// without repeated local vars in envelope writers.
func PtrTo[T any](v T) *T { return &v }

// StripAllCursors removes after / before / limit / includeTotal / page /
// perPage from a query string. Used by tests and envelope URL normalisers.
func StripAllCursors(raw string) string {
	q, _ := url.ParseQuery(raw)
	for _, k := range []string{"after", "before", "limit", "perPage", "page", "includeTotal"} {
		q.Del(k)
	}
	encoded := q.Encode()
	if encoded == "" {
		return ""
	}
	return "?" + encoded
}

// IntPtr is a typed helper for setting LegacyMeta.From / To pointers.
func IntPtr(n int) *int { return &n }

// Re-export StatusOK to allow handlers to import pagination without also
// importing net/http for numeric constants.
const (
	StatusOK = nethttp.StatusOK
)

// Ensure strings import is present when tiny helpers above grow.
var _ = strings.TrimSpace
