package handlers

import (
	"context"
	"encoding/json"
	nethttp "net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
	postservice "github.com/turahe/blog-api/internal/core/post/service"
)

type stubSearcher struct {
	filter postdomain.SearchFilter
	result postdomain.SearchResult
}

func (s *stubSearcher) SearchPublished(_ context.Context, filter postdomain.SearchFilter) (postdomain.SearchResult, error) {
	s.filter = filter
	s.result.Page, s.result.PerPage = filter.Page, filter.PerPage

	return s.result, nil
}

func getPosts(t *testing.T, posts *postservice.PostService, query url.Values) (int, map[string]any) {
	t.Helper()

	engine := gin.New()
	engine.GET("/api/v1/posts", listPublishedPostsHandler(posts))

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), nethttp.MethodGet, "/api/v1/posts?"+query.Encode(), nil))

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))

	return rec.Code, body
}

func TestListPublishedPostsSearch(t *testing.T) {
	t.Parallel()

	category := uuid.New()
	post := postdomain.Post{UUID: uuid.New(), AuthorUUID: uuid.New(), Title: "Go <generics>", Status: postdomain.StatusPublished}
	searcher := &stubSearcher{result: postdomain.SearchResult{Total: 1, Items: []postdomain.SearchHit{{
		Post: post, Rank: 0.5, Title: "Go &lt;<mark>generics</mark>&gt;", Snippet: "about <mark>generics</mark>",
	}}}}
	posts := postservice.New(nil, nil, nil).WithSearch(searcher)

	code, body := getPosts(t, posts, url.Values{
		"q": {" generics "}, "page": {"2"}, "per_page": {"5"}, "category_id": {category.String()},
	})
	require.Equal(t, nethttp.StatusOK, code)
	assert.Equal(t, "generics", searcher.filter.Query)
	assert.Equal(t, 2, searcher.filter.Page)
	assert.Equal(t, 5, searcher.filter.PerPage)
	require.NotNil(t, searcher.filter.CategoryUUID)
	assert.Equal(t, category, *searcher.filter.CategoryUUID)

	items, ok := body["data"].([]any)
	require.True(t, ok, "data is a list: %v", body)
	require.Len(t, items, 1)

	item, ok := items[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, post.UUID.String(), item["id"])
	assert.Equal(t, "Go <generics>", item["title"])
	assert.Equal(t, map[string]any{
		"rank": 0.5, "title": "Go &lt;<mark>generics</mark>&gt;", "snippet": "about <mark>generics</mark>",
	}, item["search"])
}

func TestListPublishedPostsSearchErrors(t *testing.T) {
	t.Parallel()

	withSearch := postservice.New(nil, nil, nil).WithSearch(&stubSearcher{})

	code, body := getPosts(t, withSearch, url.Values{"q": {strings.Repeat("a", postdomain.MaxSearchQueryLength+1)}})
	assert.Equal(t, nethttp.StatusBadRequest, code)
	assert.Equal(t, "validation_error", errorCode(body))

	code, body = getPosts(t, postservice.New(nil, nil, nil), url.Values{"q": {"go"}})
	assert.Equal(t, nethttp.StatusServiceUnavailable, code)
	assert.Equal(t, "search.unavailable", errorCode(body))
}
