package bootstrap_test

import (
	nethttp "net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	httpadapter "github.com/turahe/blog-api/internal/adapters/inbound/http"
	"github.com/turahe/blog-api/internal/adapters/outbound/persistence"
	outboundrbac "github.com/turahe/blog-api/internal/adapters/outbound/rbac"
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

func newImpersonationStack(t *testing.T) *impersonationStack {
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
	})
	s.clock = clock

	return &impersonationStack{authStack: s, target: target}
}

func (s *impersonationStack) start(t *testing.T, staff, password string) reply {
	t.Helper()

	return s.do(t, nethttp.MethodPost, "/api/v1/admin/impersonation/start", staff, map[string]any{
		"target_user_id": s.target.String(), "reason": "ticket #4521 cannot publish", "current_password": password,
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

	token, _ := r.data["access_token"].(string)
	require.NotEmpty(t, token)
	require.NotContains(t, r.data, "refresh_token")

	r = s.do(t, nethttp.MethodGet, "/api/v1/me", token, nil)
	require.Equal(t, nethttp.StatusOK, r.status, r.code)
	require.Equal(t, s.target.String(), r.data["id"], "the token acts as the target")

	r = s.do(t, nethttp.MethodPut, "/api/v1/me/password", token, map[string]any{"current_password": "x", "new_password": "y"})
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

func TestImpersonationTokenDiesAtSessionExpiry(t *testing.T) {
	t.Parallel()

	s := newImpersonationStack(t)
	staff, _ := s.login(t)

	r := s.start(t, staff, cyclePassword)
	require.Equal(t, nethttp.StatusCreated, r.status, r.code)

	token, _ := r.data["access_token"].(string)

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
