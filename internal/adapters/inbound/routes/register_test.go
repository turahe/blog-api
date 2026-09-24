package routes_test

import (
	nethttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/adapters/inbound/routes"
)

func TestRegisterRunsAuthBeforeHandler(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)

	var called []string

	record := func(name string) gin.HandlerFunc {
		return func(c *gin.Context) {
			called = append(called, name)

			c.Next()
		}
	}

	router := gin.New()
	routes.Register(router, routes.Controllers{
		Stub: routes.NotImplemented,
		Users: routes.Users{
			MeGet: func(c *gin.Context) {
				called = append(called, "handler")
				matched, ok := routes.RouteOf(c)
				require.True(t, ok)
				require.Equal(t, "me.get", matched.OperationID)
				require.Equal(t, routes.GroupSelfService, matched.Group)
				require.Equal(t, routes.AuthRequired, matched.Auth)
				c.Status(nethttp.StatusNoContent)
			},
		},
	}, routes.AuthMiddleware{
		Required: gin.HandlersChain{record("auth")},
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), nethttp.MethodGet, "/api/v1/me", nil))

	require.Equal(t, nethttp.StatusNoContent, recorder.Code)
	require.Equal(t, []string{"auth", "handler"}, called)
}

func TestCommentRoutesUseDeclaredAuthModes(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)

	var chain []string

	record := func(name string) gin.HandlerFunc {
		return func(c *gin.Context) {
			chain = append(chain, name)

			c.Next()
		}
	}
	handler := func(c *gin.Context) {
		route, _ := routes.RouteOf(c)
		chain = append(chain, route.OperationID)

		c.Status(nethttp.StatusNoContent)
	}
	router := gin.New()
	routes.Register(router, routes.Controllers{
		Stub: routes.NotImplemented,
		Comments: routes.Comments{
			PostList: handler, PostCreate: handler, Get: handler, Flag: handler,
			MeList: handler, Patch: handler, Delete: handler, Upvote: handler,
		},
	}, routes.AuthMiddleware{
		Optional: gin.HandlersChain{record("optional")},
		Required: gin.HandlersChain{record("required")},
	})

	cases := []struct {
		method, path string
		want         []string
	}{
		{nethttp.MethodGet, "/api/v1/posts/p/comments", []string{"public.posts.comments.list"}},
		{nethttp.MethodPost, "/api/v1/posts/p/comments", []string{"optional", "public.posts.comments.create"}},
		{nethttp.MethodGet, "/api/v1/comments/c", []string{"public.comments.get"}},
		{nethttp.MethodPost, "/api/v1/comments/c/flag", []string{"optional", "public.comments.flag"}},
		{nethttp.MethodGet, "/api/v1/me/comments", []string{"required", "self.comments.list"}},
		{nethttp.MethodPatch, "/api/v1/comments/c", []string{"required", "self.comments.patch"}},
		{nethttp.MethodDelete, "/api/v1/comments/c", []string{"required", "self.comments.delete"}},
		{nethttp.MethodPost, "/api/v1/comments/c/upvote", []string{"required", "self.comments.upvote"}},
	}
	for _, tc := range cases {
		chain = nil
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), tc.method, tc.path, nil))
		require.Equal(t, nethttp.StatusNoContent, recorder.Code, tc.method+" "+tc.path)
		require.Equal(t, tc.want, chain, tc.method+" "+tc.path)
	}
}

func TestRegisterUsesStubWhenHandlerNil(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)

	router := gin.New()
	routes.Register(router, routes.Controllers{Stub: routes.NotImplemented}, routes.AuthMiddleware{})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), nethttp.MethodPost, "/api/v1/auth/oauth/google/callback", nil))
	require.Equal(t, nethttp.StatusNotImplemented, recorder.Code)
}
