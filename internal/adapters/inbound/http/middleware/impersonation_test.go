package middleware

import (
	"context"
	"encoding/json"
	"errors"
	nethttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/adapters/inbound/routes"
	"github.com/turahe/blog-api/internal/core/audit"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
	authports "github.com/turahe/blog-api/internal/core/auth/ports"
	impdomain "github.com/turahe/blog-api/internal/core/impersonation/domain"
)

// tokenAuth parses every token as claims.
type tokenAuth struct {
	authports.Service

	claims authdomain.AccessClaims
}

func (a tokenAuth) ParseAccessToken(string) (authdomain.AccessClaims, error) { return a.claims, nil }

type fakeVerifier struct {
	err   error
	calls int
}

func (v *fakeVerifier) Verify(context.Context, string, uuid.UUID, uuid.UUID) error {
	v.calls++
	return v.err
}

func impersonationClaims() authdomain.AccessClaims {
	actor := uuid.New()
	return authdomain.AccessClaims{Subject: uuid.New(), Actor: &actor, SessionID: uuid.NewString()}
}

func serveAuth(t *testing.T, auth gin.HandlerFunc, op string) (int, string, *gin.Context) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	var seen *gin.Context

	router := gin.New()
	router.GET("/x", routes.WithMeta(routes.Route{OperationID: op}), auth, ImpersonationGuard(), func(c *gin.Context) {
		seen = c.Copy()
		c.Status(nethttp.StatusNoContent)
	})

	req := httptest.NewRequestWithContext(t.Context(), nethttp.MethodGet, "/x", nil)
	req.Header.Set("Authorization", "Bearer token")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}

	_ = json.Unmarshal(w.Body.Bytes(), &body)

	return w.Code, body.Error.Code, seen
}

func TestBearerAuthVerifiesImpersonationSessions(t *testing.T) {
	t.Parallel()

	claims := impersonationClaims()

	t.Run("active session acts as the target", func(t *testing.T) {
		t.Parallel()

		verifier := &fakeVerifier{}
		status, _, c := serveAuth(t, BearerAuth(tokenAuth{claims: claims}, verifier), "me.get")
		require.Equal(t, nethttp.StatusNoContent, status)
		assert.Equal(t, 1, verifier.calls)

		userID, _ := CurrentUserID(c)
		assert.Equal(t, claims.Subject, userID)

		imp, ok := CurrentImpersonation(c)
		require.True(t, ok)
		assert.Equal(t, *claims.Actor, imp.ActorID)
		assert.Equal(t, claims.SessionID, imp.SessionID.String())
	})

	t.Run("ended session is 401", func(t *testing.T) {
		t.Parallel()

		status, code, _ := serveAuth(t, BearerAuth(tokenAuth{claims: claims}, &fakeVerifier{err: impdomain.ErrEnded}), "me.get")
		assert.Equal(t, nethttp.StatusUnauthorized, status)
		assert.Equal(t, ErrorCodeImpersonationEnded, code)
	})

	t.Run("no verifier refuses impersonation tokens", func(t *testing.T) {
		t.Parallel()

		status, code, _ := serveAuth(t, BearerAuth(tokenAuth{claims: claims}, nil), "me.get")
		assert.Equal(t, nethttp.StatusUnauthorized, status)
		assert.Equal(t, ErrorCodeImpersonationEnded, code)
	})

	t.Run("verifier failure is 500", func(t *testing.T) {
		t.Parallel()

		status, _, _ := serveAuth(t, BearerAuth(tokenAuth{claims: claims}, &fakeVerifier{err: errors.New("db down")}), "me.get")
		assert.Equal(t, nethttp.StatusInternalServerError, status)
	})

	t.Run("optional auth treats an ended session as anonymous", func(t *testing.T) {
		t.Parallel()

		status, _, c := serveAuth(t, OptionalBearerAuth(tokenAuth{claims: claims}, &fakeVerifier{err: impdomain.ErrEnded}), "public.posts.list")
		require.Equal(t, nethttp.StatusNoContent, status)

		_, ok := CurrentUserID(c)
		assert.False(t, ok)
	})

	t.Run("regular tokens skip the verifier", func(t *testing.T) {
		t.Parallel()

		verifier := &fakeVerifier{err: impdomain.ErrEnded}
		status, _, _ := serveAuth(t, BearerAuth(tokenAuth{claims: authdomain.AccessClaims{Subject: uuid.New()}}, verifier), "me.password.update")
		assert.Equal(t, nethttp.StatusNoContent, status)
		assert.Zero(t, verifier.calls)
	})
}

func TestImpersonationGuardBlocksSensitiveOperations(t *testing.T) {
	t.Parallel()

	claims := impersonationClaims()

	for _, op := range []string{
		"me.password.update", "me.email.request_change", "me.2fa.setup", "me.2fa.disable", "me.privacy.update",
		"me.activity.export", "me.activity.erase", "auth.logout", "auth.refresh", "auth.oauth.start",
		"admin.impersonation.start", "me.newsletter.subscribe", "me.newsletter.unsubscribe",
		"analytics.consent.store", "analytics.consent.withdraw",
	} {
		status, code, _ := serveAuth(t, BearerAuth(tokenAuth{claims: claims}, &fakeVerifier{}), op)
		assert.Equal(t, nethttp.StatusForbidden, status, op)
		assert.Equal(t, ErrorCodeImpersonationForbidden, code, op)
	}

	for _, op := range []string{"me.get", "me.profile.patch", "admin.posts.update", "admin.impersonation.stop"} {
		status, _, _ := serveAuth(t, BearerAuth(tokenAuth{claims: claims}, &fakeVerifier{}), op)
		assert.Equal(t, nethttp.StatusNoContent, status, op)
	}
}

func TestImpersonationResponsesCarryTheSession(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	claims := impersonationClaims()

	serve := func(auth gin.HandlerFunc) *httptest.ResponseRecorder {
		router := gin.New()
		router.GET("/x", auth, func(c *gin.Context) { c.Status(nethttp.StatusNoContent) })

		req := httptest.NewRequestWithContext(t.Context(), nethttp.MethodGet, "/x", nil)
		req.Header.Set("Authorization", "Bearer token")

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		return w
	}

	assert.Equal(t, claims.SessionID, serve(BearerAuth(tokenAuth{claims: claims}, &fakeVerifier{})).Header().Get(HeaderImpersonationSession))
	assert.Equal(t, claims.SessionID, serve(OptionalBearerAuth(tokenAuth{claims: claims}, &fakeVerifier{})).Header().Get(HeaderImpersonationSession))

	regular := tokenAuth{claims: authdomain.AccessClaims{Subject: uuid.New()}}
	assert.Empty(t, serve(BearerAuth(regular, nil)).Header().Get(HeaderImpersonationSession))
}

func TestAuditRecordsRefusedImpersonationActions(t *testing.T) {
	t.Parallel()

	claims := impersonationClaims()
	refuse := func(c *gin.Context) {
		c.Set(ContextClaimsKey, claims)
		ImpersonationGuard()(c)
	}

	for _, tc := range []auditCase{
		{
			method: nethttp.MethodGet, path: "/me/2fa", pattern: "/me/2fa",
			route: routes.Route{OperationID: "me.2fa.get", Group: routes.GroupSelfService},
		},
		{
			method: nethttp.MethodPost, path: "/analytics/consent", pattern: "/analytics/consent",
			route: routes.Route{OperationID: "analytics.consent.store", Group: routes.GroupAnalytics},
		},
	} {
		tc.actor, tc.status, tc.handler = &claims.Subject, nethttp.StatusForbidden, refuse

		entries := runAudit(t, tc)
		require.Len(t, entries, 1, tc.route.OperationID)
		assert.Equal(t, "failure", entries[0].Result, tc.route.OperationID)
		assert.Equal(t, claims.Actor, entries[0].ImpersonatorID, tc.route.OperationID)
		assert.Equal(t, ErrorCodeImpersonationForbidden, entries[0].Metadata["failure_reason"], tc.route.OperationID)
	}
}

func TestAuditRecordsEveryImpersonatedRequest(t *testing.T) {
	t.Parallel()

	claims := impersonationClaims()
	impersonating := func(c *gin.Context) { c.Set(ContextClaimsKey, claims) }

	for _, tc := range []auditCase{
		{
			method: nethttp.MethodGet, path: "/me/activity", pattern: "/me/activity", status: nethttp.StatusOK,
			route: routes.Route{OperationID: "me.activity.list", Group: routes.GroupSelfService},
		},
		{
			method: nethttp.MethodGet, path: "/posts", pattern: "/posts", status: nethttp.StatusOK,
			route: routes.Route{OperationID: "public.posts.list", Group: routes.GroupPublic},
		},
		{
			method: nethttp.MethodPost, path: "/comments/x/upvote", pattern: "/comments/:param1/upvote", status: nethttp.StatusOK,
			route: routes.Route{OperationID: "self.comments.upvote", Group: routes.GroupSelfService},
		},
		{
			method: nethttp.MethodGet, path: "/admin/posts", pattern: "/admin/posts", status: nethttp.StatusNotFound,
			route: adminRoute("admin.posts.list"),
		},
	} {
		tc.actor, tc.handler = &claims.Subject, impersonating

		entries := runAudit(t, tc)
		require.Len(t, entries, 1, tc.route.OperationID)
		assert.Equal(t, tc.route.OperationID, entries[0].Action)
		assert.Equal(t, &claims.Subject, entries[0].ActorID, tc.route.OperationID)
		assert.Equal(t, claims.Actor, entries[0].ImpersonatorID, tc.route.OperationID)
		assert.Equal(t, claims.SessionID, entries[0].Metadata["impersonation_session_id"], tc.route.OperationID)
		assert.NotContains(t, entries[0].Metadata, "failure_reason", tc.route.OperationID)
	}

	entries := runAudit(t, auditCase{
		method: nethttp.MethodGet, path: "/me/activity", pattern: "/me/activity", status: nethttp.StatusOK,
		route: routes.Route{OperationID: "me.activity.list", Group: routes.GroupSelfService}, actor: &claims.Subject,
	})
	assert.Empty(t, entries, "the user's own reads stay unaudited")
}

func TestAuditRecordsFailedImpersonationStart(t *testing.T) {
	t.Parallel()

	staff := uuid.New()
	entries := runAudit(t, auditCase{
		method: nethttp.MethodPost, path: "/admin/impersonation/start", pattern: "/admin/impersonation/start",
		route: adminRoute("admin.impersonation.start"), actor: &staff, status: nethttp.StatusForbidden,
		handler: func(c *gin.Context) { audit.AddMetadata(c.Request.Context(), "failure_reason", "step_up_required") },
	})
	require.Len(t, entries, 1)
	assert.Equal(t, "failure", entries[0].Result)
	assert.Equal(t, "impersonation_start", entries[0].Category)
	assert.Equal(t, "step_up_required", entries[0].Metadata["failure_reason"])
}

func TestAuditRecordsImpersonator(t *testing.T) {
	t.Parallel()

	claims := impersonationClaims()
	impersonating := func(c *gin.Context) { c.Set(ContextClaimsKey, claims) }

	entries := runAudit(t, auditCase{
		method: nethttp.MethodPatch, path: "/me/profile", pattern: "/me/profile",
		route: routes.Route{OperationID: "me.profile.patch", Group: routes.GroupSelfService},
		actor: &claims.Subject, status: nethttp.StatusOK, handler: impersonating,
	})
	require.Len(t, entries, 1)
	assert.Equal(t, &claims.Subject, entries[0].ActorID)
	assert.Equal(t, claims.Actor, entries[0].ImpersonatorID)
	assert.Equal(t, claims.SessionID, entries[0].Metadata["impersonation_session_id"])

	entries = runAudit(t, auditCase{
		method: nethttp.MethodPost, path: "/admin/impersonation/stop", pattern: "/admin/impersonation/stop",
		route: adminRoute("admin.impersonation.stop"), actor: &claims.Subject, status: nethttp.StatusOK,
		handler: func(c *gin.Context) {
			impersonating(c)
			audit.SetActor(c.Request.Context(), *claims.Actor)
		},
	})
	require.Len(t, entries, 1)
	assert.Equal(t, claims.Actor, entries[0].ActorID, "stopping is attributed to the staff member")
	assert.Nil(t, entries[0].ImpersonatorID)
	assert.Equal(t, "impersonation_end", entries[0].Category)
}
