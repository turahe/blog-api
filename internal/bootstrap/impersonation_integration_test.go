package bootstrap_test

import (
	"context"
	"log/slog"
	nethttp "net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	httpadapter "github.com/turahe/blog-api/internal/adapters/inbound/http"
	"github.com/turahe/blog-api/internal/adapters/outbound/persistence"
	outboundrbac "github.com/turahe/blog-api/internal/adapters/outbound/rbac"
	auditservice "github.com/turahe/blog-api/internal/core/audit/service"
	authservice "github.com/turahe/blog-api/internal/core/auth/service"
	impdomain "github.com/turahe/blog-api/internal/core/impersonation/domain"
	impservice "github.com/turahe/blog-api/internal/core/impersonation/service"
	rbacdomain "github.com/turahe/blog-api/internal/core/rbac/domain"
	userservice "github.com/turahe/blog-api/internal/core/user/service"
	"gorm.io/gorm"
)

type grantsAndEnforcer struct {
	*outboundrbac.Enforcer
	*outboundrbac.RoleStore
}

// impersonationStack is the auth stack whose user may impersonate a target holding a subset
// of their permissions.
type impersonationStack struct {
	*authStack

	target uuid.UUID
}

func newImpersonationStack(t *testing.T, extra ...func(tx *gorm.DB, deps *httpadapter.Dependencies)) *impersonationStack {
	t.Helper()

	var target uuid.UUID

	var clock *testClock

	s := newAuthStackWith(t, func(tx *gorm.DB, deps *httpadapter.Dependencies) {
		auth, ok := deps.Auth.(*authservice.AuthService)
		require.True(t, ok)

		enforcer, err := outboundrbac.NewEnforcer(tx)
		require.NoError(t, err)

		roles := outboundrbac.NewRoleStore(tx, enforcer)
		suffix := strings.ReplaceAll(uuid.NewString()[:8], "-", "")
		read := "itest.read." + suffix

		for _, key := range []string{impdomain.PermissionStart, read} {
			require.NoError(t, tx.Exec("INSERT INTO permissions (key) VALUES (?) ON CONFLICT DO NOTHING", key).Error)
		}

		_, err = roles.CreateRole(t.Context(), rbacdomain.Role{Name: "itest_support_" + suffix, Permissions: []string{impdomain.PermissionStart, read}})
		require.NoError(t, err)
		_, err = roles.CreateRole(t.Context(), rbacdomain.Role{Name: "itest_reader_" + suffix, Permissions: []string{read}})
		require.NoError(t, err)

		var actor uuid.UUID
		require.NoError(t, tx.Raw("SELECT uuid FROM users ORDER BY id DESC LIMIT 1").Row().Scan(&actor))
		require.NoError(t, roles.AssignRoles(t.Context(), actor, []string{"itest_support_" + suffix}))

		target = uuid.New()
		require.NoError(t, tx.Exec("INSERT INTO users (uuid, email, username, full_name) VALUES (?, ?, ?, ?)",
			target, "target"+suffix+"@example.test", "target"+suffix, "Target").Error)
		require.NoError(t, roles.AssignRoles(t.Context(), target, []string{"itest_reader_" + suffix}))

		clock = &testClock{t: time.Now().UTC()}
		users := persistence.NewUserRepository(tx)
		deps.Users = userservice.New(users)
		deps.RBAC = enforcer
		deps.Impersonation = impservice.New(persistence.NewImpersonationRepository(tx), users,
			grantsAndEnforcer{enforcer, roles}, auth, auth, uuidGen{}, clock,
			impservice.Config{TTL: time.Hour, AdminRole: rbacdomain.ProtectedRole})

		for _, extend := range extra {
			extend(tx, deps)
		}
	})
	s.clock = clock

	return &impersonationStack{authStack: s, target: target}
}

func (s *impersonationStack) start(t *testing.T, staff, password string) reply {
	t.Helper()

	return s.do(t, nethttp.MethodPost, "/api/v1/admin/impersonation/start", staff, map[string]any{
		"targetUserId": s.target.String(), "reason": "ticket #4521 cannot publish", "currentPassword": password,
	})
}

func TestImpersonationLifecycle(t *testing.T) {
	t.Parallel()

	s := newImpersonationStack(t)
	staff, _ := s.login(t)

	r := s.start(t, staff, "wrong password")
	require.Equal(t, nethttp.StatusForbidden, r.status)
	require.Equal(t, "impersonation.step_up_required", r.code)

	r = s.start(t, staff, cyclePassword)
	require.Equal(t, nethttp.StatusCreated, r.status, r.code)

	token, _ := r.data["accessToken"].(string)
	require.NotEmpty(t, token)
	require.NotContains(t, r.data, "refresh_token")

	r = s.do(t, nethttp.MethodGet, "/api/v1/me", token, nil)
	require.Equal(t, nethttp.StatusOK, r.status, r.code)
	require.Equal(t, s.target.String(), r.data["id"], "the token acts as the target")

	r = s.do(t, nethttp.MethodPut, "/api/v1/me/password", token, map[string]any{"currentPassword": "x", "newPassword": "y"})
	require.Equal(t, nethttp.StatusForbidden, r.status)
	require.Equal(t, "impersonation.forbidden_action", r.code)

	r = s.start(t, token, cyclePassword)
	require.Equal(t, nethttp.StatusForbidden, r.status, "no chained impersonation")

	r = s.start(t, staff, cyclePassword)
	require.Equal(t, nethttp.StatusConflict, r.status)
	require.Equal(t, "impersonation.already_active", r.code)

	r = s.do(t, nethttp.MethodGet, "/api/v1/admin/impersonation/current", staff, nil)
	require.Equal(t, nethttp.StatusOK, r.status, r.code)
	require.Equal(t, true, r.data["active"])

	r = s.do(t, nethttp.MethodPost, "/api/v1/admin/impersonation/stop", token, nil)
	require.Equal(t, nethttp.StatusOK, r.status, r.code)
	require.Equal(t, "exited", r.data["state"])

	r = s.do(t, nethttp.MethodGet, "/api/v1/me", token, nil)
	require.Equal(t, nethttp.StatusUnauthorized, r.status)
	require.Equal(t, "auth.impersonation_ended", r.code)
}

type auditRow struct {
	Action       string
	Actor        *uuid.UUID
	Impersonator *uuid.UUID
	Resource     *uuid.UUID
	Session      *string
	Failure      *string
	Reason       *string
}

func TestImpersonationAuditTrail(t *testing.T) {
	t.Parallel()

	var recorder *auditservice.Recorder

	s := newImpersonationStack(t, func(tx *gorm.DB, deps *httpadapter.Dependencies) {
		recorder = auditservice.NewRecorder(persistence.NewAuditRepository(tx), slog.New(slog.DiscardHandler),
			auditservice.RecorderOptions{FlushInterval: time.Hour})
		deps.Audit = recorder
	})
	staff, _ := s.login(t)

	var staffID uuid.UUID
	require.NoError(t, s.tx.Raw("SELECT uuid FROM users WHERE email = ?", s.email).Row().Scan(&staffID))

	require.Equal(t, nethttp.StatusForbidden, s.start(t, staff, "wrong password").status)

	r := s.start(t, staff, cyclePassword)
	require.Equal(t, nethttp.StatusCreated, r.status, r.code)

	token, _ := r.data["accessToken"].(string)
	session, _ := r.data["session"].(map[string]any)
	sessionID, _ := session["impersonationSessionId"].(string)

	require.Equal(t, nethttp.StatusOK, s.do(t, nethttp.MethodGet, "/api/v1/me", token, nil).status)
	require.Equal(t, nethttp.StatusOK, s.do(t, nethttp.MethodGet, "/api/v1/me", staff, nil).status)

	r = s.do(t, nethttp.MethodPut, "/api/v1/me/password", token, map[string]any{"currentPassword": "x", "newPassword": "y"})
	require.Equal(t, "impersonation.forbidden_action", r.code)

	require.Equal(t, nethttp.StatusOK, s.do(t, nethttp.MethodPost, "/api/v1/admin/impersonation/stop", token, nil).status)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	require.NoError(t, recorder.Close(ctx))

	var rows []auditRow
	require.NoError(t, s.tx.Raw(`SELECT a.action, actor.uuid AS actor, imp.uuid AS impersonator, a.resource_id AS resource,
			a.metadata->>'impersonation_session_id' AS session, a.metadata->>'failure_reason' AS failure,
			a.metadata->>'reason' AS reason
		FROM audit_logs a
		LEFT JOIN users actor ON actor.id = a.actor_id
		LEFT JOIN users imp ON imp.id = a.impersonator_id
		WHERE a.actor_id IN (SELECT id FROM users WHERE uuid IN (?, ?))
		ORDER BY a.id`, staffID, s.target).Scan(&rows).Error)

	byOutcome := map[string]auditRow{}
	reads := 0

	for _, row := range rows {
		if row.Action == "me.get" {
			reads++
		}

		key := row.Action
		if row.Failure != nil {
			key += "/" + *row.Failure
		}

		byOutcome[key] = row
	}

	failed := byOutcome["admin.impersonation.start/step_up_required"]
	require.Equal(t, &staffID, failed.Actor, "a refused start is recorded against the staff member")
	require.Equal(t, &s.target, failed.Resource)

	started := byOutcome["admin.impersonation.start"]
	require.Equal(t, &staffID, started.Actor)
	require.Equal(t, &s.target, started.Resource)
	require.Equal(t, &sessionID, started.Session)
	require.Equal(t, "ticket #4521 cannot publish", *started.Reason)

	require.Equal(t, 1, reads, "the impersonated read is recorded; the staff member's own is not")

	read := byOutcome["me.get"]
	require.Equal(t, &s.target, read.Actor)
	require.Equal(t, &staffID, read.Impersonator)
	require.Equal(t, &sessionID, read.Session)

	refused := byOutcome["me.password.update/impersonation.forbidden_action"]
	require.Equal(t, &s.target, refused.Actor, "actions under impersonation act as the target")
	require.Equal(t, &staffID, refused.Impersonator)
	require.Equal(t, &sessionID, refused.Session)

	stopped := byOutcome["admin.impersonation.stop"]
	require.Equal(t, &staffID, stopped.Actor, "stopping is attributed to the staff member")
	require.Nil(t, stopped.Impersonator)
	require.Equal(t, &sessionID, stopped.Session)
}

func TestImpersonationEndsWhenStaffSignsOut(t *testing.T) {
	t.Parallel()

	s := newImpersonationStack(t)
	staff, refresh := s.login(t)

	r := s.start(t, staff, cyclePassword)
	require.Equal(t, nethttp.StatusCreated, r.status, r.code)

	token, _ := r.data["accessToken"].(string)
	require.Equal(t, nethttp.StatusOK, s.do(t, nethttp.MethodGet, "/api/v1/me", token, nil).status)

	r = s.do(t, nethttp.MethodPost, "/api/v1/auth/logout", staff, map[string]any{"refreshToken": refresh})
	require.Equal(t, nethttp.StatusOK, r.status, r.code)

	r = s.do(t, nethttp.MethodGet, "/api/v1/me", token, nil)
	require.Equal(t, nethttp.StatusUnauthorized, r.status)
	require.Equal(t, "auth.impersonation_ended", r.code)

	var state, reason string
	require.NoError(t, s.tx.Raw(`SELECT s.state, s.end_reason FROM impersonation_sessions s
		JOIN users u ON u.id = s.target_id WHERE u.uuid = ?`, s.target).Row().Scan(&state, &reason))
	require.Equal(t, "revoked", state)
	require.Equal(t, "parent_session_expired", reason)

	again, _ := s.login(t)
	r = s.start(t, again, cyclePassword)
	require.Equal(t, nethttp.StatusCreated, r.status, "a new sign-in can impersonate again: %s", r.code)
}

func TestImpersonationStartClosesSessionOfEndedSignIn(t *testing.T) {
	t.Parallel()

	s := newImpersonationStack(t)
	staff, refresh := s.login(t)

	r := s.start(t, staff, cyclePassword)
	require.Equal(t, nethttp.StatusCreated, r.status, r.code)

	first, _ := r.data["accessToken"].(string)

	r = s.do(t, nethttp.MethodPost, "/api/v1/auth/logout", staff, map[string]any{"refreshToken": refresh})
	require.Equal(t, nethttp.StatusOK, r.status, r.code)

	again, _ := s.login(t)
	r = s.start(t, again, cyclePassword)
	require.Equal(t, nethttp.StatusCreated, r.status, "the unused session of the old sign-in does not block: %s", r.code)

	r = s.do(t, nethttp.MethodGet, "/api/v1/me", first, nil)
	require.Equal(t, nethttp.StatusUnauthorized, r.status)
	require.Equal(t, "auth.impersonation_ended", r.code)
}

func TestImpersonationTokenDiesAtSessionExpiry(t *testing.T) {
	t.Parallel()

	s := newImpersonationStack(t)
	staff, _ := s.login(t)

	r := s.start(t, staff, cyclePassword)
	require.Equal(t, nethttp.StatusCreated, r.status, r.code)

	token, _ := r.data["accessToken"].(string)

	s.clock.advance(time.Hour)

	r = s.do(t, nethttp.MethodGet, "/api/v1/me", token, nil)
	require.Equal(t, nethttp.StatusUnauthorized, r.status)
	require.Equal(t, "auth.impersonation_ended", r.code)

	r = s.do(t, nethttp.MethodGet, "/api/v1/admin/impersonation/current", staff, nil)
	require.Equal(t, nethttp.StatusOK, r.status, r.code)
	require.Equal(t, false, r.data["active"])
	require.Nil(t, r.data["session"])

	var state string
	require.NoError(t, s.tx.Raw("SELECT state FROM impersonation_sessions s JOIN users u ON u.id = s.target_id WHERE u.uuid = ?",
		s.target).Row().Scan(&state))
	require.Equal(t, "expired", state)
}
