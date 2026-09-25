package handlers

import (
	"context"
	"encoding/json"
	nethttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/middleware"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
	impdomain "github.com/turahe/blog-api/internal/core/impersonation/domain"
	impservice "github.com/turahe/blog-api/internal/core/impersonation/service"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
)

type fakeImpersonation struct {
	started   impservice.Started
	session   impdomain.Session
	active    bool
	err       error
	input     impservice.StartInput
	actorID   uuid.UUID
	sessionID *uuid.UUID
}

func (f *fakeImpersonation) Start(_ context.Context, in impservice.StartInput) (impservice.Started, error) {
	f.input = in
	return f.started, f.err
}

func (f *fakeImpersonation) Stop(_ context.Context, actorID uuid.UUID, sessionID *uuid.UUID) (impdomain.Session, error) {
	f.actorID, f.sessionID = actorID, sessionID
	return f.session, f.err
}

func (f *fakeImpersonation) Current(_ context.Context, actorID uuid.UUID, sessionID *uuid.UUID) (impdomain.Session, bool, error) {
	f.actorID, f.sessionID = actorID, sessionID
	return f.session, f.active, f.err
}

// runImpersonation runs handler as claims (an impersonation token when claims.Actor is set).
func runImpersonation(t *testing.T, handler gin.HandlerFunc, method, body string, claims authdomain.AccessClaims) (int, map[string]any) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequestWithContext(t.Context(), method, "/api/v1/admin/impersonation", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(middleware.ContextUserIDKey, claims.Subject)
	c.Set(middleware.ContextClaimsKey, claims)
	handler(c)

	var out map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out), w.Body.String())

	return w.Code, out
}

func objectOf(t *testing.T, v any) map[string]any {
	t.Helper()

	m, ok := v.(map[string]any)
	require.True(t, ok, "expected a JSON object, got %T", v)

	return m
}

func impersonationFixture() (impdomain.Session, userdomain.User) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	target := userdomain.User{UUID: uuid.New(), Username: "author", Email: "author@example.com"}
	session := impdomain.Session{
		UUID: uuid.New(), ActorUUID: testUserID, TargetUUID: target.UUID, State: impdomain.StateActive,
		Reason: "ticket #4521 cannot publish", StartedAt: now, ExpiresAt: now.Add(time.Hour),
	}

	return session, target
}

func TestStartImpersonationHandler(t *testing.T) {
	t.Parallel()

	session, target := impersonationFixture()
	valid := `{"target_user_id":"` + target.UUID.String() + `","reason":"ticket #4521 cannot publish","current_password":"pw","two_factor_code":"123456"}`
	staff := authdomain.AccessClaims{Subject: testUserID}

	t.Run("issues the token", func(t *testing.T) {
		t.Parallel()

		fake := &fakeImpersonation{started: impservice.Started{Session: session, Target: target, AccessToken: "imp-token"}}
		status, body := runImpersonation(t, adminStartImpersonationHandler(fake), nethttp.MethodPost, valid, staff)
		require.Equal(t, nethttp.StatusCreated, status)

		data := dataOf(body)
		assert.Equal(t, "imp-token", data["access_token"])
		assert.Equal(t, "Bearer", data["token_type"])
		assert.InDelta(t, 3600, data["expires_in"], 0)
		assert.NotContains(t, data, "refresh_token")
		assert.Equal(t, session.UUID.String(), objectOf(t, data["session"])["impersonation_session_id"])
		assert.Equal(t, target.UUID.String(), objectOf(t, data["target_user"])["id"])

		assert.Equal(t, testUserID, fake.input.ActorID)
		assert.Equal(t, target.UUID, fake.input.TargetID)
		assert.Equal(t, "pw", fake.input.Password)
		assert.Equal(t, "123456", fake.input.Code)
	})

	t.Run("validates the body", func(t *testing.T) {
		t.Parallel()

		for _, body := range []string{
			`{"target_user_id":"nope","reason":"ticket #4521 cannot publish","current_password":"pw"}`,
			`{"target_user_id":"` + target.UUID.String() + `","reason":"short","current_password":"pw"}`,
			`{"target_user_id":"` + target.UUID.String() + `","reason":"ticket #4521 cannot publish"}`,
		} {
			status, out := runImpersonation(t, adminStartImpersonationHandler(&fakeImpersonation{}), nethttp.MethodPost, body, staff)
			assert.Equal(t, nethttp.StatusBadRequest, status, body)
			assert.Equal(t, "validation_error", errorCode(out), body)
		}
	})

	for name, tc := range map[string]struct {
		err    error
		status int
		code   string
	}{
		"step-up":        {impdomain.ErrStepUpRequired, nethttp.StatusForbidden, "impersonation.step_up_required"},
		"ineligible":     {impdomain.ErrIneligible, nethttp.StatusUnprocessableEntity, "impersonation.target_ineligible"},
		"already active": {impdomain.ErrAlreadyActive, nethttp.StatusConflict, "impersonation.already_active"},
		"forbidden":      {impdomain.ErrForbidden, nethttp.StatusForbidden, "rbac.forbidden"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			status, out := runImpersonation(t, adminStartImpersonationHandler(&fakeImpersonation{err: tc.err}), nethttp.MethodPost, valid, staff)
			assert.Equal(t, tc.status, status)
			assert.Equal(t, tc.code, errorCode(out))
		})
	}
}

func TestStopAndCurrentImpersonationHandlers(t *testing.T) {
	t.Parallel()

	session, target := impersonationFixture()
	actor := testUserID
	impToken := authdomain.AccessClaims{Subject: target.UUID, Actor: &actor, SessionID: session.UUID.String()}
	staff := authdomain.AccessClaims{Subject: actor}

	t.Run("stop with the impersonation token ends its session", func(t *testing.T) {
		t.Parallel()

		ended := session
		ended.State = impdomain.StateExited
		fake := &fakeImpersonation{session: ended}

		status, body := runImpersonation(t, adminStopImpersonationHandler(fake), nethttp.MethodPost, "", impToken)
		require.Equal(t, nethttp.StatusOK, status)
		assert.Equal(t, "exited", dataOf(body)["state"])
		assert.Equal(t, actor, fake.actorID, "the staff member stops, not the target")
		require.NotNil(t, fake.sessionID)
		assert.Equal(t, session.UUID, *fake.sessionID)
	})

	t.Run("stop with the staff token and no session is 404", func(t *testing.T) {
		t.Parallel()

		fake := &fakeImpersonation{err: impdomain.ErrNotFound}
		status, body := runImpersonation(t, adminStopImpersonationHandler(fake), nethttp.MethodPost, "", staff)
		require.Equal(t, nethttp.StatusNotFound, status)
		assert.Equal(t, "impersonation.not_found", errorCode(body))
		assert.Nil(t, fake.sessionID)
	})

	t.Run("current reports the active session", func(t *testing.T) {
		t.Parallel()

		fake := &fakeImpersonation{session: session, active: true}
		status, body := runImpersonation(t, adminCurrentImpersonationHandler(fake), nethttp.MethodGet, "", impToken)
		require.Equal(t, nethttp.StatusOK, status)
		assert.Equal(t, true, dataOf(body)["active"])
		assert.Equal(t, target.UUID.String(), objectOf(t, dataOf(body)["session"])["target_user_id"])
	})

	t.Run("current without a session is null", func(t *testing.T) {
		t.Parallel()

		status, body := runImpersonation(t, adminCurrentImpersonationHandler(&fakeImpersonation{}), nethttp.MethodGet, "", staff)
		require.Equal(t, nethttp.StatusOK, status)
		assert.Equal(t, false, dataOf(body)["active"])
		assert.Nil(t, dataOf(body)["session"])
	})
}
