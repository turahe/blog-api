package responses_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	"github.com/turahe/blog-api/internal/shared/pagination"
)

func testCtx(t *testing.T, query url.Values) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	raw := ""
	if len(query) > 0 {
		raw = "?" + query.Encode()
	}

	c.Request = httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/test"+raw, nil)
	c.Request.Host = "api.example.com"
	c.Request.Header.Set("X-Forwarded-Proto", "https")
	c.Set("request_id", "REQ-1")

	return c, w
}

type postDTO struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
}

func jsonFloat(t *testing.T, v any) float64 {
	t.Helper()

	f, ok := v.(float64)
	require.True(t, ok)

	return f
}

func jsonString(t *testing.T, v any) string {
	t.Helper()

	s, ok := v.(string)
	require.True(t, ok)

	return s
}

func TestLegacyEnvelopeGolden(t *testing.T) {
	t.Parallel()
	// Pre-refactor legacy envelope shape — produced by SuccessPaginated.
	data := []postDTO{{ID: 1, Title: "a"}, {ID: 2, Title: "b"}}
	ctx, w := testCtx(t, url.Values{"page": []string{"2"}, "tag": []string{"go"}})
	responses.SuccessPaginated(ctx, 200, data, 2, 20, 999)

	var env map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &env))
	meta, ok := env["meta"].(map[string]any)
	require.True(t, ok)
	links, ok := env["links"].(map[string]any)
	require.True(t, ok)

	// Legacy field assertions — these match the pre-refactor envelope exactly.
	require.InDelta(t, 2, jsonFloat(t, meta["currentPage"]), 0)
	require.InDelta(t, 999, jsonFloat(t, meta["total"]), 0)
	require.InDelta(t, 50, jsonFloat(t, meta["lastPage"]), 0) // ceil(999/20)
	require.InDelta(t, 20, jsonFloat(t, meta["perPage"]), 0)
	require.InDelta(t, 21, jsonFloat(t, meta["from"]), 0)
	require.InDelta(t, 40, jsonFloat(t, meta["to"]), 0)
	require.Equal(t, "https://api.example.com/api/v1/test", meta["path"])
	require.Equal(t, "REQ-1", meta["requestId"])
	// No cursor-only fields present.
	require.NotContains(t, meta, "nextCursor")
	require.NotContains(t, meta, "hasNextPage")
	require.NotContains(t, meta, "limit")

	// Links: first / last / prev / next all populated, filter (tag=go) preserved.
	require.NotNil(t, links["first"])
	require.NotNil(t, links["last"])
	require.NotNil(t, links["prev"])
	require.NotNil(t, links["next"])
	first, err := url.Parse(jsonString(t, links["first"]))
	require.NoError(t, err)
	require.Equal(t, "1", first.Query().Get("page"))
	require.Equal(t, "go", first.Query().Get("tag"))
}

func TestSuccessPaginatedResultCursorMode(t *testing.T) {
	t.Parallel()

	total := int64(5000)

	items := make([]postDTO, 20)
	for i := range items {
		items[i] = postDTO{ID: int64(i + 1), Title: "t"}
	}

	result := pagination.PageResult[postDTO]{
		Items:           items,
		Total:           &total,
		HasNextPage:     true,
		HasPreviousPage: true,
		NextCursor:      "CUR_NEXT",
		PreviousCursor:  "CUR_PREV",
		Limit:           20,
	}
	ctx, w := testCtx(t, url.Values{"tag": []string{"x"}, "author": []string{"ada"}})
	responses.SuccessPaginatedResult[postDTO](ctx, 200, responses.CursorPageOpts[postDTO]{Result: result})

	var env map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &env))
	meta, ok := env["meta"].(map[string]any)
	require.True(t, ok)
	links, ok := env["links"].(map[string]any)
	require.True(t, ok)

	// Cursor fields all present.
	require.Equal(t, "CUR_NEXT", meta["nextCursor"])
	require.Equal(t, "CUR_PREV", meta["previousCursor"])
	require.Equal(t, true, meta["hasNextPage"])
	require.Equal(t, true, meta["hasPreviousPage"])
	require.InDelta(t, 20, jsonFloat(t, meta["limit"]), 0)
	// Legacy fields preserved when total available.
	require.InDelta(t, 5000, jsonFloat(t, meta["total"]), 0)
	require.InDelta(t, 250, jsonFloat(t, meta["lastPage"]), 0) // ceil(5000/20)
	// next/prev links: after/before with cursor values + filters kept
	next, err := url.Parse(jsonString(t, links["next"]))
	require.NoError(t, err)
	require.Equal(t, "CUR_NEXT", next.Query().Get("after"))
	require.Equal(t, "x", next.Query().Get("tag"))
	require.Equal(t, "ada", next.Query().Get("author"))

	prev, err := url.Parse(jsonString(t, links["prev"]))
	require.NoError(t, err)
	require.Equal(t, "CUR_PREV", prev.Query().Get("before"))
}

func TestSuccessPaginatedResultCursorNoTotal(t *testing.T) {
	t.Parallel()

	items := []postDTO{{ID: 1}}
	result := pagination.PageResult[postDTO]{
		Items:       items,
		Total:       nil,
		HasNextPage: false,
		Limit:       10,
	}
	ctx, w := testCtx(t, url.Values{"includeTotal": []string{"false"}})
	responses.SuccessPaginatedResult[postDTO](ctx, 200, responses.CursorPageOpts[postDTO]{Result: result})

	var env map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &env))
	meta, ok := env["meta"].(map[string]any)
	require.True(t, ok)
	links, ok := env["links"].(map[string]any)
	require.True(t, ok)

	require.NotContains(t, meta, "total")
	require.NotContains(t, meta, "lastPage")
	require.Nil(t, links["first"])
	require.Nil(t, links["last"])
}

func TestSuccessPaginatedResultOffsetMode(t *testing.T) {
	t.Parallel()

	total := int64(42)
	items := []postDTO{{ID: 1}}
	result := pagination.PageResult[postDTO]{
		Items:         items,
		Total:         &total,
		Limit:         10,
		OffsetPage:    2,
		OffsetPerPage: 10,
		HasNextPage:   true,
	}
	ctx, w := testCtx(t, url.Values{"page": []string{"2"}, "q": []string{"x"}})
	responses.SuccessPaginatedResult[postDTO](ctx, 200, responses.CursorPageOpts[postDTO]{Result: result})

	var env map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &env))
	meta, ok := env["meta"].(map[string]any)
	require.True(t, ok)
	require.InDelta(t, 2, jsonFloat(t, meta["currentPage"]), 0)
	require.InDelta(t, 10, jsonFloat(t, meta["perPage"]), 0)
	require.InDelta(t, 42, jsonFloat(t, meta["total"]), 0)
	require.InDelta(t, 5, jsonFloat(t, meta["lastPage"]), 0)
	require.InDelta(t, 11, jsonFloat(t, meta["from"]), 0)
	require.InDelta(t, 20, jsonFloat(t, meta["to"]), 0)

	links, ok := env["links"].(map[string]any)
	require.True(t, ok)
	require.NotNil(t, links["first"])
	require.NotNil(t, links["last"])
	require.NotNil(t, links["next"])
	next, err := url.Parse(jsonString(t, links["next"]))
	require.NoError(t, err)
	require.Equal(t, "3", next.Query().Get("page"))
	require.Equal(t, "x", next.Query().Get("q"))
}
