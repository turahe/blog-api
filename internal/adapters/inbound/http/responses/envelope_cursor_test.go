package responses_test

import (
	"encoding/json"
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
	c.Request = httptest.NewRequest("GET", "/api/v1/test"+raw, nil)
	c.Request.Host = "api.example.com"
	c.Request.Header.Set("X-Forwarded-Proto", "https")
	c.Set("request_id", "REQ-1")
	return c, w
}

type postDTO struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
}

func normaliseEnvelope(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var env map[string]any
	require.NoError(t, json.Unmarshal(raw, &env))
	meta, _ := env["meta"].(map[string]any)
	if meta != nil {
		// Remove requestId for legacy comparison to be stable (requestId
		// comes from context and is handled separately).
		delete(meta, "requestId")
	}
	return env
}

func TestLegacyEnvelopeGolden(t *testing.T) {
	t.Parallel()
	// Pre-refactor legacy envelope shape — produced by SuccessPaginated.
	data := []postDTO{{ID: 1, Title: "a"}, {ID: 2, Title: "b"}}
	ctx, w := testCtx(t, url.Values{"page": []string{"2"}, "tag": []string{"go"}})
	responses.SuccessPaginated(ctx, 200, data, 2, 20, 999)

	var env map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &env))
	meta := env["meta"].(map[string]any)
	links := env["links"].(map[string]any)

	// Legacy field assertions — these match the pre-refactor envelope exactly.
	require.Equal(t, float64(2), meta["currentPage"])
	require.Equal(t, float64(999), meta["total"])
	require.Equal(t, float64(50), meta["lastPage"]) // ceil(999/20)
	require.Equal(t, float64(20), meta["perPage"])
	require.Equal(t, float64(21), meta["from"])
	require.Equal(t, float64(40), meta["to"])
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
	first, _ := url.Parse(links["first"].(string))
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
	meta := env["meta"].(map[string]any)
	links := env["links"].(map[string]any)

	// Cursor fields all present.
	require.Equal(t, "CUR_NEXT", meta["nextCursor"])
	require.Equal(t, "CUR_PREV", meta["previousCursor"])
	require.Equal(t, true, meta["hasNextPage"])
	require.Equal(t, true, meta["hasPreviousPage"])
	require.Equal(t, float64(20), meta["limit"])
	// Legacy fields preserved when total available.
	require.Equal(t, float64(5000), meta["total"])
	require.Equal(t, float64(250), meta["lastPage"]) // ceil(5000/20)
	// next/prev links: after/before with cursor values + filters kept
	next, _ := url.Parse(links["next"].(string))
	require.Equal(t, "CUR_NEXT", next.Query().Get("after"))
	require.Equal(t, "x", next.Query().Get("tag"))
	require.Equal(t, "ada", next.Query().Get("author"))
	prev, _ := url.Parse(links["prev"].(string))
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
	meta := env["meta"].(map[string]any)
	links := env["links"].(map[string]any)

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
	meta := env["meta"].(map[string]any)
	require.Equal(t, float64(2), meta["currentPage"])
	require.Equal(t, float64(10), meta["perPage"])
	require.Equal(t, float64(42), meta["total"])
	require.Equal(t, float64(5), meta["lastPage"])
	require.Equal(t, float64(11), meta["from"])
	require.Equal(t, float64(20), meta["to"])
	links := env["links"].(map[string]any)
	require.NotNil(t, links["first"])
	require.NotNil(t, links["last"])
	require.NotNil(t, links["next"])
	next, _ := url.Parse(links["next"].(string))
	require.Equal(t, "3", next.Query().Get("page"))
	require.Equal(t, "x", next.Query().Get("q"))
}
