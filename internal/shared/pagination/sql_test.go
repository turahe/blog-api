package pagination_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/shared/pagination"
)

func TestBuildSeekForwardDESC(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	pr := pagination.PageRequest{Mode: pagination.ModeCursor, Forward: true, Limit: 20, Cursor: "X"}
	cursor := map[string]any{"published_at": now, "created_at": now.Add(1), "id": int64(42)}
	res, err := pagination.BuildSeek(cfgPost, pr, cursor)
	require.NoError(t, err)
	require.Contains(t, res.WhereClause, "(p.published_at, p.created_at, p.id) <")
	require.Len(t, res.BindVars, 3)
	require.Equal(t, now, res.BindVars[0])
	require.Equal(t, int64(42), res.BindVars[2])
	require.Equal(t, 21, res.LimitFetch)
	require.False(t, res.ReverseDisplay)
	require.Contains(t, res.OrderClause, "ORDER BY p.published_at DESC NULLS LAST, p.created_at DESC, p.id DESC")
}

func TestBuildSeekForwardASC(t *testing.T) {
	t.Parallel()
	cfg := pagination.CursorConfig{
		Kind: "comments_oldest",
		Sort: []pagination.SortField{
			{Name: "created_at", Dir: pagination.Asc, Type: pagination.TypeTime},
			{Name: "id", Dir: pagination.Asc, Type: pagination.TypeInt64},
		},
	}
	pr := pagination.PageRequest{Mode: pagination.ModeCursor, Forward: true, Limit: 5, Cursor: "X"}
	cursor := map[string]any{"created_at": time.Unix(1700000000, 0), "id": int64(7)}
	res, err := pagination.BuildSeek(cfg, pr, cursor)
	require.NoError(t, err)
	require.Contains(t, res.WhereClause, "(created_at, id) >")
	require.Contains(t, res.OrderClause, "ORDER BY created_at ASC, id ASC")
	require.Equal(t, 6, res.LimitFetch)
}

func TestBuildSeekBackward(t *testing.T) {
	t.Parallel()
	cfg := pagination.CursorConfig{
		Kind: "c_audit",
		Sort: []pagination.SortField{
			{Name: "occurred_at", Column: "a.occurred_at", Dir: pagination.Desc, Nulls: pagination.NullsLast, Type: pagination.TypeTime},
			{Name: "id", Column: "a.id", Dir: pagination.Desc, Type: pagination.TypeInt64},
		},
	}
	pr := pagination.PageRequest{Mode: pagination.ModeCursor, Forward: false, Limit: 50, Cursor: "Y"}
	cursor := map[string]any{"occurred_at": time.Unix(100, 0), "id": int64(12)}
	res, err := pagination.BuildSeek(cfg, pr, cursor)
	require.NoError(t, err)
	require.True(t, res.ReverseDisplay)
	require.Contains(t, res.OrderClause, "a.occurred_at ASC NULLS FIRST")
	require.Contains(t, res.OrderClause, "a.id ASC")
	require.Contains(t, res.WhereClause, "(a.occurred_at, a.id) >")
	require.Len(t, res.BindVars, 2)
}

func TestBuildSeekEmptyCursorFirstPage(t *testing.T) {
	t.Parallel()
	pr := pagination.PageRequest{Mode: pagination.ModeCursor, Forward: true, Limit: 20}
	res, err := pagination.BuildSeek(cfgPost, pr, nil)
	require.NoError(t, err)
	require.Empty(t, res.WhereClause)
	require.Empty(t, res.BindVars)
	require.Equal(t, 21, res.LimitFetch)
}

func TestBuildSeekOffsetModeNoSeek(t *testing.T) {
	t.Parallel()
	pr := pagination.PageRequest{Mode: pagination.ModeOffset, Limit: 15, Offset: 30}
	res, err := pagination.BuildSeek(cfgPost, pr, nil)
	require.NoError(t, err)
	require.Empty(t, res.WhereClause)
	require.Equal(t, 16, res.LimitFetch)
	require.False(t, res.ReverseDisplay)
	require.Equal(t, "ORDER BY p.published_at DESC NULLS LAST, p.created_at DESC, p.id DESC", res.OrderClause)
}

func TestBuildSeekEmptySort(t *testing.T) {
	t.Parallel()
	_, err := pagination.BuildSeek(pagination.CursorConfig{}, pagination.PageRequest{}, nil)
	require.Error(t, err)
}

func TestTruncatePageForwardFirstPageExact(t *testing.T) {
	t.Parallel()
	items := ints(20)
	page, hasNext, hasPrev := pagination.TruncatePage(items, 20, true, false)
	require.Equal(t, ints(20), page)
	require.False(t, hasNext)
	require.False(t, hasPrev)
}

func TestTruncatePageForwardPeekOne(t *testing.T) {
	t.Parallel()
	items := ints(21)
	page, hasNext, hasPrev := pagination.TruncatePage(items, 20, true, true)
	require.Len(t, page, 20)
	require.Equal(t, 1, page[0])
	require.Equal(t, 20, page[19])
	require.True(t, hasNext)
	require.True(t, hasPrev)
}

func TestTruncatePageBackwardPeekOne(t *testing.T) {
	t.Parallel()
	items := ints(21)
	page, hasNext, hasPrev := pagination.TruncatePage(items, 20, false, true)
	require.Len(t, page, 20)
	// 21 items reversed = [21..1]; back+excess drops index 0 (the peek row), so page = [20,19..1]
	require.Equal(t, 20, page[0])
	require.Equal(t, 1, page[19])
	require.True(t, hasNext) // hadCursor
	require.True(t, hasPrev) // excess = more rows behind this page (prev direction)
}

func TestTruncatePageBackwardBoundaryLastPage(t *testing.T) {
	t.Parallel()
	items := ints(3) // less than limit 5 — no excess; backward
	page, hasNext, hasPrev := pagination.TruncatePage(items, 5, false, true)
	require.Len(t, page, 3)
	require.Equal(t, []int{3, 2, 1}, page) // simple reverse, no truncation needed
	require.True(t, hasNext)               // hadCursor → still on a page with content after
	require.False(t, hasPrev)              // no excess = at start of set (end of backwards walk)
}

func TestExtractLimit(t *testing.T) {
	t.Parallel()
	require.Equal(t, 21, pagination.ExtractLimit(pagination.PageRequest{Mode: pagination.ModeCursor, Limit: 20}))
	require.Equal(t, 20, pagination.ExtractLimit(pagination.PageRequest{Mode: pagination.ModeOffset, Limit: 20}))
	require.Equal(t, 21, pagination.ExtractLimit(pagination.PageRequest{Mode: pagination.ModeCursor, Limit: 0}))
}

func ints(n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = i + 1
	}
	return out
}
