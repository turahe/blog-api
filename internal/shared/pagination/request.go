package pagination

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// ParseRequest extracts pagination parameters from the incoming Gin context
// and returns a canonical PageRequest. Precedence:
//
//  1. If `after` is present → ModeCursor, Forward=true, Cursor=after.
//  2. Else if `before` is present → ModeCursor, Forward=false, Cursor=before.
//  3. Else if `page` is present → ModeOffset, Offset=(page-1)*Limit.
//  4. Otherwise → ModeCursor, Forward=true, Cursor="" (first page).
//
// If the caller passes both an `after`/`before` cursor AND a legacy `page`
// param, cursor mode wins and `page` is ignored (documented behaviour, not
// ambiguous).
//
// `limit` and `perPage` are aliases; `limit` wins if both are set. Default
// cfg.DefaultPerPage (or 20 if zero). Clamped to [1, cfg.MaxPerPage (or 100)].
//
// `includeTotal` (bool, default true) controls whether repositories run the
// COUNT(*) step.
//
// Offset is intentionally left at zero in the returned PageRequest; the
// service or repository layer is responsible for computing
// Offset=(Page-1)*Limit when ModeOffset is active and issuing the query.
func ParseRequest(c *gin.Context, cfg CursorConfig, opts ...Option) (PageRequest, error) {
	opt, err := applyOptions(opts)
	_ = opt // reserved for future request correlation; no-op current usage
	if err != nil {
		return PageRequest{}, err
	}
	maxPer := cfg.MaxPerPage
	if maxPer <= 0 {
		maxPer = DefaultMaxPerPage
	}
	defPer := cfg.DefaultPerPage
	if defPer <= 0 {
		defPer = DefaultPerPage
	}

	after := strings.TrimSpace(c.Query("after"))
	before := strings.TrimSpace(c.Query("before"))
	limitRaw := strings.TrimSpace(c.Query("limit"))
	if limitRaw == "" {
		limitRaw = strings.TrimSpace(c.Query("perPage"))
	}
	limit := defPer
	if limitRaw != "" {
		n, err := strconv.Atoi(limitRaw)
		if err == nil {
			limit = n
		}
	}
	if limit < 1 {
		limit = defPer
	}
	if limit > maxPer {
		limit = maxPer
	}

	includeTotal := true
	if it := strings.TrimSpace(c.Query("includeTotal")); it != "" {
		switch strings.ToLower(it) {
		case "0", "false", "no", "off":
			includeTotal = false
		}
	}

	pr := PageRequest{Limit: limit, IncludeTotal: includeTotal}

	switch {
	case after != "":
		if cfg.OffsetModeOnly {
			return pr, ErrCursorUnsupported
		}
		pr.Mode = ModeCursor
		pr.Forward = true
		pr.Cursor = after
		return pr, nil
	case before != "":
		if cfg.OffsetModeOnly {
			return pr, ErrCursorUnsupported
		}
		pr.Mode = ModeCursor
		pr.Forward = false
		pr.Cursor = before
		return pr, nil
	}

	// Fall through: check legacy page param.
	pageRaw := strings.TrimSpace(c.Query("page"))
	if pageRaw != "" {
		page, _ := strconv.Atoi(pageRaw)
		if page < 1 {
			page = 1
		}
		pr.Mode = ModeOffset
		pr.Page = page
		pr.Forward = true
		return pr, nil
	}

	pr.Mode = ModeCursor
	pr.Forward = true
	return pr, nil
}

// ParseLegacy builds a PageRequest with ModeOffset directly from page+perPage
// integers. Used by services that already have legacy numeric values
// pre-parsed from older call sites. Clamps to [1,maxPerPage] with defaults.
func ParseLegacy(cfg CursorConfig, page, perPage int) PageRequest {
	maxPer := cfg.MaxPerPage
	if maxPer <= 0 {
		maxPer = DefaultMaxPerPage
	}
	defPer := cfg.DefaultPerPage
	if defPer <= 0 {
		defPer = DefaultPerPage
	}
	if perPage < 1 {
		perPage = defPer
	}
	if perPage > maxPer {
		perPage = maxPer
	}
	if page < 1 {
		page = 1
	}
	return PageRequest{
		Mode:         ModeOffset,
		Forward:      true,
		Limit:        perPage,
		Offset:       (page - 1) * perPage,
		Page:         page,
		IncludeTotal: true,
	}
}
