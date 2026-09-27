package handlers

import (
	"context"
	"errors"
	nethttp "net/http"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
	postservice "github.com/turahe/blog-api/internal/core/post/service"
)

type failingSearcher struct{ err error }

func (f failingSearcher) SearchPublished(context.Context, postdomain.SearchFilter) (postdomain.SearchResult, error) {
	return postdomain.SearchResult{}, f.err
}

func TestListPublishedPostsHandler(t *testing.T) {
	t.Parallel()

	tagID := uuid.New()
	post := postdomain.Post{UUID: uuid.New(), Title: "Hello", Slug: "hello", Status: postdomain.StatusPublished}

	tests := []struct {
		name    string
		query   url.Values
		repoErr error
		status  int
		code    string
		message string
	}{
		{name: "lists newest posts", query: url.Values{"page": {"2"}, "perPage": {"5"}, "tagId": {tagID.String()}}, status: nethttp.StatusOK},
		{name: "invalid category", query: url.Values{"categoryId": {"nope"}}, status: nethttp.StatusBadRequest, code: "validation_error", message: "Invalid categoryId"},
		{name: "invalid tag", query: url.Values{"tagId": {"nope"}}, status: nethttp.StatusBadRequest, code: "validation_error", message: "Invalid tagId"},
		{name: "repository failure", repoErr: errors.New("db down"), status: nethttp.StatusInternalServerError, code: "internal_error", message: "Failed to list posts"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			repo := &fakePostRepo{err: tc.repoErr, published: postdomain.ListResult{Items: []postdomain.Post{post}, Total: 11, Page: 2, PerPage: 5}}
			code, body := getPosts(t, postservice.New(repo, nil, nil), tc.query)
			require.Equal(t, tc.status, code)
			require.Equal(t, tc.code, errorCode(body))
			require.Equal(t, tc.message, errorMessage(body))

			if tc.status != nethttp.StatusOK {
				return
			}

			require.Equal(t, 2, repo.listFilter.Page)
			require.Equal(t, 5, repo.listFilter.PerPage)
			require.Equal(t, &tagID, repo.listFilter.TagUUID)

			data, ok := body["data"].([]any)
			require.True(t, ok)
			require.Len(t, data, 1)
			require.Equal(t, post.UUID.String(), as[map[string]any](t, data[0])["id"])
			require.InDelta(t, 11, as[map[string]any](t, body["meta"])["total"], 0)
		})
	}
}

func TestSearchPublishedPostsFailure(t *testing.T) {
	t.Parallel()

	posts := postservice.New(nil, nil, nil).WithSearch(failingSearcher{err: errors.New("index down")})

	code, body := getPosts(t, posts, url.Values{"q": {"go"}})
	require.Equal(t, nethttp.StatusInternalServerError, code)
	require.Equal(t, "internal_error", errorCode(body))
	require.Equal(t, "Failed to search posts", errorMessage(body))
}

func TestGetPublishedPostHandler(t *testing.T) {
	t.Parallel()

	post := postdomain.Post{UUID: uuid.New(), Title: "Hello", Slug: "hello", Status: postdomain.StatusPublished}
	tests := []struct {
		name     string
		param    string
		repoErr  error
		status   int
		code     string
		message  string
		wantSlug string
	}{
		{name: "found", param: " hello ", status: nethttp.StatusOK, wantSlug: "hello"},
		{name: "blank slug", param: "  ", status: nethttp.StatusNotFound, code: "not_found", message: "Post not found"},
		{name: "not published", param: "draft", repoErr: postdomain.ErrNotFound, status: nethttp.StatusNotFound, code: "not_found", message: "Post not found", wantSlug: "draft"},
		{name: "repository failure", param: "hello", repoErr: errors.New("db down"), status: nethttp.StatusInternalServerError, code: "internal_error", message: "Failed to load post", wantSlug: "hello"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			repo := &fakePostRepo{post: post, err: tc.repoErr}
			w, body := runProfile(t, getPublishedPostHandler(postservice.New(repo, nil, nil)), profileRequest{
				method: nethttp.MethodGet, target: "/api/v1/posts/x", param: tc.param,
			})
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Equal(t, tc.code, errorCode(body))
			require.Equal(t, tc.message, errorMessage(body))
			require.Equal(t, tc.wantSlug, repo.slug)

			if tc.status == nethttp.StatusOK {
				require.Equal(t, post.UUID.String(), dataOf(body)["id"])
			}
		})
	}
}
