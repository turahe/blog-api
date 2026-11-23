package pagination_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/shared/pagination"
)

func newCtx2(t *testing.T, path string, q url.Values) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path+"?"+q.Encode(), nil)
	req.Host = "api.example.com"
	req.Header.Set("X-Forwarded-Proto", "https")
	ctx.Request = req

	return ctx
}

type Item struct {
	ID        int64
	Published time.Time
	Created   time.Time
}

func (i Item) SortValues(_ pagination.CursorConfig) map[string]any {
	return map[string]any{
		"published_at": i.Published,
		"created_at":   i.Created,
		"id":           i.ID,
	}
}

func jsonFloat(t *testing.T, v any) float64 {
	t.Helper()

	f, ok := v.(float64)
	require.True(t, ok)

	return f
}

func TestBuildMetaCursorMode(t *testing.T) {
	t.Parallel()

	total := int64(999)
	r := pagination.PageResult[Item]{
		Items:           make([]Item, 3),
		Total:           &total,
		HasNextPage:     true,
		HasPreviousPage: true,
		NextCursor:      "NX",
		PreviousCursor:  "PR",
		Limit:           10,
	}
	ctx := newCtx2(t, "/v1/posts", url.Values{"q": []string{"hello"}})
	ctx.Set("request_id", "R-1")
	legacy, cursor := pagination.BuildMeta[Item](ctx, r)
	// Marshal cursor meta only.
	b, err := json.Marshal(cursor)
	require.NoError(t, err)

	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	require.Equal(t, "NX", m["nextCursor"])
	require.Equal(t, "PR", m["previousCursor"])
	require.Equal(t, true, m["hasNextPage"])
	require.Equal(t, true, m["hasPreviousPage"])
	require.InDelta(t, 10, jsonFloat(t, m["limit"]), 0)
	require.Equal(t, "R-1", m["requestId"])

	_ = legacy
}

func TestBuildMetaOffsetMode(t *testing.T) {
	t.Parallel()

	total := int64(999)
	r := pagination.PageResult[Item]{
		Items:         make([]Item, 20),
		Total:         &total,
		Limit:         20,
		OffsetPage:    3,
		OffsetPerPage: 20,
	}
	ctx := newCtx2(t, "/v1/posts", url.Values{"tag": []string{"go"}})
	legacy, _ := pagination.BuildMeta[Item](ctx, r)
	b, err := json.Marshal(legacy)
	require.NoError(t, err)

	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	require.InDelta(t, 3, jsonFloat(t, m["currentPage"]), 0)
	require.InDelta(t, 50, jsonFloat(t, m["lastPage"]), 0) // 999 / 20 ceil = 50
	require.InDelta(t, 20, jsonFloat(t, m["perPage"]), 0)
	require.InDelta(t, 41, jsonFloat(t, m["from"]), 0) // (3-1)*20 + 1
	require.InDelta(t, 60, jsonFloat(t, m["to"]), 0)   // (3-1)*20 + 20
	require.InDelta(t, 999, jsonFloat(t, m["total"]), 0)
	require.Equal(t, "/v1/posts", m["path"])
}

func TestBuildLinksCursorModePreservesFilters(t *testing.T) {
	t.Parallel()

	r := pagination.PageResult[Item]{
		Items:           make([]Item, 5),
		HasNextPage:     true,
		HasPreviousPage: true,
		NextCursor:      "NC",
		PreviousCursor:  "PC",
		Limit:           20,
	}
	ctx := newCtx2(t, "/posts", url.Values{
		"author":       []string{"ada"},
		"tag":          []string{"x"},
		"after":        []string{"OLD_AFTER"},
		"before":       []string{"OLD_BEFORE"},
		"page":         []string{"7"},
		"includeTotal": []string{"false"},
	})
	legacy, _ := pagination.BuildMeta[Item](ctx, r)
	links := pagination.BuildLinks[Item](ctx, r, legacy)
	// next: hasNextPage -> next cursor, include filters (author, tag, includeTotal), strip after/before/page.
	require.NotNil(t, links.Next, "Next link must exist when hasNextPage=true")
	u, err := url.Parse(*links.Next)
	require.NoError(t, err)

	q := u.Query()
	require.Equal(t, "NC", q.Get("after"))
	require.Empty(t, q.Get("before"))
	require.Equal(t, "ada", q.Get("author"))
	require.Equal(t, "x", q.Get("tag"))
	require.Equal(t, "false", q.Get("includeTotal"))
	require.NotNil(t, links.Prev)
	u2, _ := url.Parse(*links.Prev)
	require.Equal(t, "PC", u2.Query().Get("before"))
	require.Nil(t, links.First)
	require.Nil(t, links.Last)
}

func TestBuildLinksOffsetModeLegacy(t *testing.T) {
	t.Parallel()

	total := int64(100)
	r := pagination.PageResult[Item]{
		Items:         make([]Item, 20),
		Total:         &total,
		Limit:         20,
		OffsetPage:    3,
		OffsetPerPage: 20,
		HasNextPage:   true,
	}
	ctx := newCtx2(t, "/posts", url.Values{"q": []string{"hello"}, "perPage": []string{"20"}})
	legacy, _ := pagination.BuildMeta[Item](ctx, r)
	links := pagination.BuildLinks[Item](ctx, r, legacy)
	require.NotNil(t, links.First)
	require.NotNil(t, links.Last)
	require.NotNil(t, links.Next)
	u, _ := url.Parse(*links.Next)
	require.Equal(t, "4", u.Query().Get("page"))
	require.Equal(t, "hello", u.Query().Get("q"))

	ul, _ := url.Parse(*links.Last)
	require.Equal(t, "5", ul.Query().Get("page"))
}

func TestBuildMetaNoTotalSkipsLegacyFields(t *testing.T) {
	t.Parallel()

	r := pagination.PageResult[Item]{
		Items:         make([]Item, 5),
		Total:         nil,
		Limit:         20,
		OffsetPage:    1,
		OffsetPerPage: 20,
	}
	ctx := newCtx2(t, "/x", url.Values{})
	legacy, _ := pagination.BuildMeta[Item](ctx, r)
	b, _ := json.Marshal(legacy)

	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	require.NotContains(t, m, "total")
	require.NotContains(t, m, "from")
	require.NotContains(t, m, "to")
	// lastPage and currentPage are int without pointer; they may be present as 0/1;
	// total absent is the key invariant.
}

func TestStripAllCursorsRemovesKeys(t *testing.T) {
	t.Parallel()

	out := pagination.StripAllCursors("after=a&before=b&page=1&q=x")
	parsed, _ := url.ParseQuery(strings.TrimPrefix(out, "?"))
	require.Empty(t, parsed.Get("after"))
	require.Empty(t, parsed.Get("before"))
	require.Empty(t, parsed.Get("page"))
	require.Equal(t, "x", parsed.Get("q"))
}
