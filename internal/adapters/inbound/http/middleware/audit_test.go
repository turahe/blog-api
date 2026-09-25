package middleware

import (
	"context"
	nethttp "net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/adapters/inbound/routes"
	"github.com/turahe/blog-api/internal/core/audit"
	auditdomain "github.com/turahe/blog-api/internal/core/audit/domain"
)

type captureWriter struct {
	mu      sync.Mutex
	entries []auditdomain.Entry
}

func (w *captureWriter) Record(_ context.Context, entry auditdomain.Entry) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.entries = append(w.entries, entry)
}

type auditCase struct {
	method, path, pattern string
	route                 routes.Route
	actor                 *uuid.UUID
	status                int
	handler               gin.HandlerFunc // optional extra work before responding
}

func runAudit(t *testing.T, tc auditCase) []auditdomain.Entry {
	t.Helper()

	gin.SetMode(gin.TestMode)

	writer := &captureWriter{}
	router := gin.New()
	router.Use(RequestID(), Audit(writer))

	tc.route.Method = tc.method
	router.Handle(tc.method, tc.pattern, routes.WithMeta(tc.route), func(c *gin.Context) {
		if tc.actor != nil {
			c.Set(ContextUserIDKey, *tc.actor)
		}

		if tc.handler != nil {
			tc.handler(c)
		}

		c.Status(tc.status)
	})

	req := httptest.NewRequestWithContext(t.Context(), tc.method, tc.path, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) Firefox/130.0")
	router.ServeHTTP(httptest.NewRecorder(), req)

	return writer.entries
}

func adminRoute(op string) routes.Route {
	return routes.Route{OperationID: op, Group: routes.GroupAdmin, Auth: routes.AuthRequired}
}

func TestAuditRecordsSuccessfulAdminMutation(t *testing.T) {
	t.Parallel()

	actor, post := uuid.New(), uuid.New()

	entries := runAudit(t, auditCase{
		method: nethttp.MethodPost, path: "/posts/" + post.String() + "/publish", pattern: "/posts/:param1/publish",
		route: adminRoute("admin.posts.publish"), actor: &actor, status: nethttp.StatusOK,
		handler: func(c *gin.Context) { audit.AddChange(c.Request.Context(), "status", "draft", "published") },
	})

	require.Len(t, entries, 1)
	entry := entries[0]
	require.Equal(t, "admin.posts.publish", entry.Action)
	require.Equal(t, "post_publish", entry.Category)
	require.Equal(t, auditdomain.ResultSuccess, entry.Result)
	require.Equal(t, &actor, entry.ActorID)
	require.Equal(t, "post", entry.ResourceType)
	require.Equal(t, &post, entry.ResourceID)
	require.Equal(t, auditdomain.Change{From: "draft", To: "published"}, entry.Changes["status"])
	require.NotEmpty(t, entry.RequestID)
	require.NotEmpty(t, entry.IP)
	require.Contains(t, entry.UserAgent, "Firefox")
	require.NotEqual(t, uuid.Nil, entry.UUID)
}

func TestAuditAdminActionOnUserTargetsTheUser(t *testing.T) {
	t.Parallel()

	actor, target := uuid.New(), uuid.New()

	entries := runAudit(t, auditCase{
		method: nethttp.MethodPost, path: "/users/" + target.String() + "/roles", pattern: "/users/:param1/roles",
		route: adminRoute("admin.users.roles.assign"), actor: &actor, status: nethttp.StatusOK,
	})

	require.Len(t, entries, 1)
	require.Equal(t, "role_change", entries[0].Category)
	require.Equal(t, auditdomain.ResourceUser, entries[0].ResourceType)
	require.Equal(t, &target, entries[0].ResourceID)
}

func TestAuditSkipsReadsFailuresAndNoisyOperations(t *testing.T) {
	t.Parallel()

	actor := uuid.New()

	cases := map[string]auditCase{
		"read": {
			method: nethttp.MethodGet, path: "/posts", pattern: "/posts",
			route: adminRoute("admin.posts.list"), actor: &actor, status: nethttp.StatusOK,
		},
		"rejected admin write": {
			method: nethttp.MethodPost, path: "/posts", pattern: "/posts",
			route: adminRoute("admin.posts.create"), actor: &actor, status: nethttp.StatusForbidden,
		},
		"refresh": {
			method: nethttp.MethodPost, path: "/auth/refresh", pattern: "/auth/refresh",
			route: routes.Route{OperationID: "auth.refresh", Group: routes.GroupAuth},
			actor: &actor, status: nethttp.StatusOK,
		},
		"anonymous public write": {
			method: nethttp.MethodPost, path: "/comments/x/flag", pattern: "/comments/:param1/flag",
			route:  routes.Route{OperationID: "public.comments.flag", Group: routes.GroupPublic},
			status: nethttp.StatusCreated,
		},
		"login awaiting second factor": {
			method: nethttp.MethodPost, path: "/auth/login", pattern: "/auth/login",
			route:  routes.Route{OperationID: "auth.login", Group: routes.GroupAuth},
			status: nethttp.StatusOK,
		},
		"stub": {
			method: nethttp.MethodPost, path: "/settings", pattern: "/settings",
			route: adminRoute("admin.settings.put"), actor: &actor, status: nethttp.StatusNotImplemented,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.Empty(t, runAudit(t, tc))
		})
	}
}

func TestAuditRecordsRejectedLoginAgainstKnownAccount(t *testing.T) {
	t.Parallel()

	account := uuid.New()

	entries := runAudit(t, auditCase{
		method: nethttp.MethodPost, path: "/auth/login", pattern: "/auth/login",
		route:   routes.Route{OperationID: "auth.login", Group: routes.GroupAuth},
		status:  nethttp.StatusUnauthorized,
		handler: func(c *gin.Context) { audit.SetActor(c.Request.Context(), account) },
	})

	require.Len(t, entries, 1)
	require.Equal(t, auditdomain.ResultFailure, entries[0].Result)
	require.Equal(t, nethttp.StatusUnauthorized, entries[0].Status)
	require.Equal(t, "login", entries[0].Category)
	require.Equal(t, &account, entries[0].ActorID)
	require.Equal(t, auditdomain.ResourceUser, entries[0].ResourceType)
	require.Equal(t, &account, entries[0].ResourceID)
}

func TestAuditRecordsRejectedAdminLoginWithoutAccount(t *testing.T) {
	t.Parallel()

	entries := runAudit(t, auditCase{
		method: nethttp.MethodPost, path: "/admin/auth/login", pattern: "/admin/auth/login",
		route:  routes.Route{OperationID: "admin.auth.login", Group: routes.GroupAdmin},
		status: nethttp.StatusTooManyRequests,
	})

	require.Len(t, entries, 1)
	require.Equal(t, auditdomain.ResultFailure, entries[0].Result)
	require.Nil(t, entries[0].ActorID)
}

func TestAuditSuccessfulLoginUsesServiceActor(t *testing.T) {
	t.Parallel()

	account := uuid.New()

	entries := runAudit(t, auditCase{
		method: nethttp.MethodPost, path: "/auth/login", pattern: "/auth/login",
		route:   routes.Route{OperationID: "auth.login", Group: routes.GroupAuth},
		status:  nethttp.StatusOK,
		handler: func(c *gin.Context) { audit.SetActor(c.Request.Context(), account) },
	})

	require.Len(t, entries, 1)
	require.Equal(t, auditdomain.ResultSuccess, entries[0].Result)
	require.Equal(t, &account, entries[0].ActorID)
}

func TestAuditRecordsSignedInPublicWrite(t *testing.T) {
	t.Parallel()

	actor, post := uuid.New(), uuid.New()

	entries := runAudit(t, auditCase{
		method: nethttp.MethodPost, path: "/posts/" + post.String() + "/comments", pattern: "/posts/:param1/comments",
		route: routes.Route{OperationID: "public.posts.comments.create", Group: routes.GroupPublic},
		actor: &actor, status: nethttp.StatusCreated,
	})

	require.Len(t, entries, 1)
	require.Equal(t, "comment_create", entries[0].Category)
	require.Equal(t, &post, entries[0].ResourceID)
}

func TestAuditAdminOnlyActionHasNoCategory(t *testing.T) {
	t.Parallel()

	actor := uuid.New()

	entries := runAudit(t, auditCase{
		method: nethttp.MethodDelete, path: "/tags/not-a-uuid", pattern: "/tags/:param1",
		route: adminRoute("admin.tags.delete"), actor: &actor, status: nethttp.StatusNoContent,
	})

	require.Len(t, entries, 1)
	require.Empty(t, entries[0].Category)
	require.Equal(t, "tag", entries[0].ResourceType)
	require.Nil(t, entries[0].ResourceID)
}

func TestAuditWithoutWriterPassesThrough(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(Audit(nil))
	router.POST("/x", func(c *gin.Context) { c.Status(nethttp.StatusNoContent) })

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), nethttp.MethodPost, "/x", nil))
	require.Equal(t, nethttp.StatusNoContent, recorder.Code)
}
