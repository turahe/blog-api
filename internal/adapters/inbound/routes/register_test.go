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
			AdminList: handler, AdminGet: handler, AdminStats: handler, AdminModerate: handler,
			AdminBulkModerate: handler, AdminHardDelete: handler,
		},
		Posts: routes.Posts{
			AdminPublish: handler, AdminUnpublish: handler, AdminArchive: handler,
			AdminDelete: handler, AdminRestore: handler,
		},
		Users: routes.Users{
			MeProfilePatch: handler, MeAvatarUpload: handler, MeAvatarDelete: handler,
			MeEmailRequestChange: handler, MeEmailConfirmChange: handler, PublicProfile: handler,
			AdminProfileGet: handler, AdminProfilePatch: handler,
		},
		Auth: routes.Auth{
			TwoFactorChallenge: handler, MeTwoFactorGet: handler, MeTwoFactorSetup: handler,
			MeTwoFactorConfirm: handler, MeTwoFactorDisable: handler, MeTwoFactorBackupCodes: handler,
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
		{nethttp.MethodGet, "/api/v1/admin/comments", []string{"required", "admin.comments.list"}},
		{nethttp.MethodGet, "/api/v1/admin/comments/stats", []string{"required", "admin.comments.stats"}},
		{nethttp.MethodGet, "/api/v1/admin/comments/c", []string{"required", "admin.comments.get"}},
		{nethttp.MethodPost, "/api/v1/admin/comments/c/moderate", []string{"required", "admin.comments.moderate"}},
		{nethttp.MethodPost, "/api/v1/admin/comments/bulk-moderate", []string{"required", "admin.comments.bulk_moderate"}},
		{nethttp.MethodDelete, "/api/v1/admin/comments/c", []string{"required", "admin.comments.delete"}},
		{nethttp.MethodPost, "/api/v1/admin/posts/p/publish", []string{"required", "admin.posts.publish"}},
		{nethttp.MethodPost, "/api/v1/admin/posts/p/unpublish", []string{"required", "admin.posts.unpublish"}},
		{nethttp.MethodPost, "/api/v1/admin/posts/p/archive", []string{"required", "admin.posts.archive"}},
		{nethttp.MethodPost, "/api/v1/admin/posts/p/restore", []string{"required", "admin.posts.restore"}},
		{nethttp.MethodDelete, "/api/v1/admin/posts/p", []string{"required", "admin.posts.delete"}},
		{nethttp.MethodPatch, "/api/v1/me/profile", []string{"required", "me.profile.patch"}},
		{nethttp.MethodPost, "/api/v1/me/avatar", []string{"required", "me.avatar.upload"}},
		{nethttp.MethodDelete, "/api/v1/me/avatar", []string{"required", "me.avatar.delete"}},
		{nethttp.MethodPost, "/api/v1/me/email/request-change", []string{"required", "me.email.request_change"}},
		{nethttp.MethodPost, "/api/v1/me/email/confirm-change", []string{"required", "me.email.confirm_change"}},
		{nethttp.MethodGet, "/api/v1/users/ada", []string{"optional", "public.users.profile"}},
		{nethttp.MethodGet, "/api/v1/admin/users/u/profile", []string{"required", "admin.users.profile.get"}},
		{nethttp.MethodPatch, "/api/v1/admin/users/u/profile", []string{"required", "admin.users.profile.patch"}},
		{nethttp.MethodPost, "/api/v1/auth/2fa/challenge", []string{"auth.2fa.challenge"}},
		{nethttp.MethodGet, "/api/v1/me/2fa", []string{"required", "me.2fa.get"}},
		{nethttp.MethodDelete, "/api/v1/me/2fa", []string{"required", "me.2fa.disable"}},
		{nethttp.MethodPost, "/api/v1/me/2fa/setup", []string{"required", "me.2fa.setup"}},
		{nethttp.MethodPost, "/api/v1/me/2fa/confirm", []string{"required", "me.2fa.confirm"}},
		{nethttp.MethodPost, "/api/v1/me/2fa/backup-codes", []string{"required", "me.2fa.backup_codes"}},
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
