package handlers

import (
	"context"
	"errors"
	nethttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/middleware"
)

// stubEnforcer answers every permission check with allow and err.
type stubEnforcer struct {
	allow bool
	err   error
}

func (e stubEnforcer) Enforce(context.Context, uuid.UUID, string) (bool, error) {
	return e.allow, e.err
}

// reached answers 200 so a guard that lets the request through is observable.
func reached(c *gin.Context) { c.JSON(nethttp.StatusOK, gin.H{"reached": true}) }

func errorMessage(body map[string]any) string {
	errObj, _ := body["error"].(map[string]any)
	message, _ := errObj["message"].(string)

	return message
}

func TestRequirePermission(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		enforcer permissionEnforcer
		anon     bool
		status   int
		code     string
		message  string
	}{
		{name: "anonymous", enforcer: stubEnforcer{allow: true}, anon: true, status: nethttp.StatusUnauthorized, code: "unauthorized", message: "Authentication required"},
		{name: "no enforcer", enforcer: nil, status: nethttp.StatusForbidden, code: "forbidden", message: "Authorization unavailable"},
		{name: "enforcer error", enforcer: stubEnforcer{err: errors.New("casbin down")}, status: nethttp.StatusInternalServerError, code: "internal_error", message: "Authorization check failed"},
		{name: "denied", enforcer: stubEnforcer{}, status: nethttp.StatusForbidden, code: "rbac.forbidden", message: "Insufficient permissions"},
		{name: "allowed", enforcer: stubEnforcer{allow: true}, status: nethttp.StatusOK},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := profileRequest{method: nethttp.MethodGet, target: "/"}
			if !tc.anon {
				req.user = &testUserID
			}

			w, body := runProfile(t, chain(requirePermission(tc.enforcer, "post.read"), reached), req)
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Equal(t, tc.code, errorCode(body))
			require.Equal(t, tc.message, errorMessage(body))
			require.Equal(t, tc.status == nethttp.StatusOK, body["reached"] == true)
		})
	}
}

func TestRequireRoles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		lookup  fakeRoleLookup
		anon    bool
		status  int
		code    string
		message string
	}{
		{name: "anonymous", lookup: fakeRoleLookup{names: []string{roleAdmin}}, anon: true, status: nethttp.StatusUnauthorized, code: "unauthorized", message: "Authentication required"},
		{name: "lookup error", lookup: fakeRoleLookup{err: errors.New("db down")}, status: nethttp.StatusInternalServerError, code: "internal_error", message: "Failed to resolve roles"},
		{name: "allowed role", lookup: fakeRoleLookup{names: []string{"subscriber", roleEditor}}, status: nethttp.StatusOK},
		{name: "no allowed role", lookup: fakeRoleLookup{names: []string{roleAuthor}}, status: nethttp.StatusForbidden, code: "forbidden", message: "Insufficient permissions"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := profileRequest{method: nethttp.MethodGet, target: "/"}
			if !tc.anon {
				req.user = &testUserID
			}

			w, body := runProfile(t, chain(requireRoles(tc.lookup, roleAdmin, roleEditor), reached), req)
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Equal(t, tc.code, errorCode(body))
			require.Equal(t, tc.message, errorMessage(body))
			require.Equal(t, tc.status == nethttp.StatusOK, body["reached"] == true)
		})
	}
}

func TestHolds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		deps Deps
		anon bool
		want bool
	}{
		{name: "anonymous", deps: Deps{RBAC: stubEnforcer{allow: true}}, anon: true},
		{name: "enforcer allows", deps: Deps{RBAC: stubEnforcer{allow: true}}, want: true},
		{name: "enforcer denies", deps: Deps{RBAC: stubEnforcer{}}},
		{name: "enforcer error denies", deps: Deps{RBAC: stubEnforcer{allow: true, err: errors.New("casbin down")}}},
		{name: "no enforcer and no role lookup", deps: Deps{}},
		{name: "role lookup error denies", deps: Deps{Roles: fakeRoleLookup{names: []string{roleAdmin}, err: errors.New("db down")}}},
		{name: "fallback role held", deps: Deps{Roles: fakeRoleLookup{names: []string{roleEditor}}}, want: true},
		{name: "fallback role missing", deps: Deps{Roles: fakeRoleLookup{names: []string{roleAuthor}}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequestWithContext(t.Context(), nethttp.MethodGet, "/", nil)

			if !tc.anon {
				c.Set(middleware.ContextUserIDKey, testUserID)
			}

			require.Equal(t, tc.want, holds(c, tc.deps, "post.revisions.view_all", editorRoles))
		})
	}
}
