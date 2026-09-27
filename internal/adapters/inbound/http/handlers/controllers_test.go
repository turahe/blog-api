package handlers

import (
	nethttp "net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/adapters/inbound/routes"
	categoryservice "github.com/turahe/blog-api/internal/core/category/service"
	commentservice "github.com/turahe/blog-api/internal/core/comment/service"
	postservice "github.com/turahe/blog-api/internal/core/post/service"
	tagservice "github.com/turahe/blog-api/internal/core/tag/service"
	userservice "github.com/turahe/blog-api/internal/core/user/service"
)

func fullDeps(roles []string) Deps {
	return Deps{
		Health:          stubHealth{},
		Auth:            &fakeAuth{},
		Users:           userservice.New(&fakeUserRepo{}),
		AdminUsers:      &fakeAdminUsers{},
		TwoFactor:       &fakeTwoFactor{},
		AdminLogin:      &fakeAdminLogin{},
		OAuth:           &fakeOAuth{},
		RoleAdmin:       &fakeRoles{},
		Activity:        &fakeActivity{},
		Notifications:   &fakeInbox{},
		Profiles:        &fakeProfiles{},
		Privacy:         &fakePrivacy{},
		PrivacyRequests: &fakePrivacyRequests{},
		EmailChange:     &fakeEmailChanger{},
		Registration:    &fakeRegistrar{},
		AvatarMaxBytes:  1 << 20,
		Roles:           fakeRoleLookup{names: roles},
		Posts:           postservice.New(nil, nil, nil),
		Categories:      categoryservice.New(nil, nil, nil),
		Tags:            tagservice.New(nil, nil, nil),
		Media:           &fakeMediaService{},
		Comments:        commentservice.New(nil, nil, nil, commentservice.Config{}),
		Consent:         &fakeConsent{},
		SettingsValues:  fakeSettingsValues{},
		Version:         "1.2.3",
	}
}

func TestNewControllers(t *testing.T) {
	t.Parallel()

	wired := func(c routes.Controllers) map[string]gin.HandlerFunc {
		return map[string]gin.HandlerFunc{
			"health.live":        c.Health.Live,
			"health.ready":       c.Health.Ready,
			"health.version":     c.Health.Version,
			"auth.login":         c.Auth.Login,
			"auth.refresh":       c.Auth.Refresh,
			"auth.logout":        c.Auth.Logout,
			"auth.reset":         c.Auth.PasswordReset,
			"auth.register":      c.Auth.Register,
			"auth.verify":        c.Auth.VerifyEmail,
			"auth.admin_login":   c.Auth.AdminLogin,
			"auth.oauth_start":   c.Auth.OAuthStart,
			"auth.oauth_cb":      c.Auth.OAuthCallback,
			"auth.2fa":           c.Auth.TwoFactorChallenge,
			"auth.me_2fa_codes":  c.Auth.MeTwoFactorBackupCodes,
			"users.me":           c.Users.MeGet,
			"users.admin_list":   c.Users.AdminUsersList,
			"users.admin_create": c.Users.AdminCreate,
			"users.profile":      c.Users.PublicProfile,
			"users.admin_patch":  c.Users.AdminProfilePatch,
			"users.avatar":       c.Users.MeAvatarUpload,
			"users.avatar_del":   c.Users.MeAvatarDelete,
			"users.privacy":      c.Users.MePrivacyGet,
			"users.privacy_put":  c.Users.MePrivacyUpdate,
			"users.email_req":    c.Users.MeEmailRequestChange,
			"users.email_ok":     c.Users.MeEmailConfirmChange,
			"activity.export":    c.Activity.MeExport,
			"activity.erase":     c.Activity.MeErase,
			"activity.me":        c.Activity.MeList,
			"roles.list":         c.Roles.List,
			"roles.revoke":       c.Roles.UserRoleRevoke,
			"consent.store":      c.Analytics.ConsentStore,
			"consent.gate":       c.Analytics.IngestGate,
			"notifications.list": c.Notifications.List,
			"cats.public":        c.Cats.PublicList,
			"cats.move":          c.Cats.AdminMove,
			"tags.public":        c.Tags.PublicList,
			"tags.delete":        c.Tags.AdminDelete,
			"media.presign":      c.Media.AdminCreate,
			"comments.create":    c.Comments.PostCreate,
			"comments.hard":      c.Comments.AdminHardDelete,
			"posts.public":       c.Posts.PublicList,
		}
	}

	tests := []struct {
		name  string
		deps  Deps
		wired bool
	}{
		{name: "present services are wired", deps: fullDeps([]string{roleAdmin}), wired: true},
		{name: "absent services stay nil", deps: Deps{}, wired: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			c := NewControllers(tc.deps)
			require.NotNil(t, c.Stub)
			require.NotNil(t, c.Guard)

			for name, handler := range wired(c) {
				require.Equal(t, tc.wired, handler != nil, name)
			}
		})
	}
}

func TestNewControllersServesHealthAndGatesAdminRoutes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		handler func(routes.Controllers) gin.HandlerFunc
		role    string
		status  int
		code    string
	}{
		{name: "version is public", handler: func(c routes.Controllers) gin.HandlerFunc { return c.Health.Version }, role: roleAuthor, status: nethttp.StatusOK},
		{name: "author cannot list roles", handler: func(c routes.Controllers) gin.HandlerFunc { return c.Roles.List }, role: roleAuthor, status: nethttp.StatusForbidden, code: "forbidden"},
		{name: "admin lists roles", handler: func(c routes.Controllers) gin.HandlerFunc { return c.Roles.List }, role: roleAdmin, status: nethttp.StatusOK},
		{name: "author cannot read profiles", handler: func(c routes.Controllers) gin.HandlerFunc { return c.Users.AdminProfileGet }, role: roleAuthor, status: nethttp.StatusForbidden, code: "forbidden"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			handler := tc.handler(NewControllers(fullDeps([]string{tc.role})))
			w, body := runProfile(t, handler, profileRequest{method: nethttp.MethodGet, target: "/", param: testUserID.String(), user: &testUserID})
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Equal(t, tc.code, errorCode(body))
		})
	}
}

func TestNewControllersPermissionChecksInsideHandlers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		handler func(routes.Controllers) gin.HandlerFunc
		role    string
		body    string
		status  int
		code    string
	}{
		{
			name: "admin may assign roles on create", handler: func(c routes.Controllers) gin.HandlerFunc { return c.Users.AdminCreate },
			role: roleAdmin, body: newUserBody + `,"roles":["editor"]}`, status: nethttp.StatusCreated,
		},
		{
			name: "editor lists revisions of any post", handler: func(c routes.Controllers) gin.HandlerFunc { return c.Posts.RevisionsList },
			role: roleEditor, status: nethttp.StatusBadRequest, code: "validation_error",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			handler := tc.handler(NewControllers(fullDeps([]string{tc.role})))
			w, body := runProfile(t, handler, profileRequest{
				method: nethttp.MethodPost, target: "/", body: tc.body, contentType: jsonContent,
				param: uuid.NewString(), user: &testUserID,
			})
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Equal(t, tc.code, errorCode(body))
		})
	}
}

func TestNewControllersPublicProfilePrivilege(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		role       string
		privileged bool
	}{
		{name: "admin sees private fields", role: roleAdmin, privileged: true},
		{name: "author does not", role: roleAuthor},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			profiles := &fakeProfiles{}
			deps := fullDeps([]string{tc.role})
			deps.Profiles = profiles

			w, _ := runProfile(t, NewControllers(deps).Users.PublicProfile, profileRequest{
				method: nethttp.MethodGet, target: "/", param: "ada", user: &testUserID,
			})
			require.Equal(t, nethttp.StatusOK, w.Code, w.Body.String())
			require.Equal(t, tc.privileged, profiles.privileged)
		})
	}
}

func TestGateWithoutAuthorizationReturnsHandler(t *testing.T) {
	t.Parallel()

	w, body := runProfile(t, gate(Deps{}, "anything", adminRoles, reached), profileRequest{method: nethttp.MethodGet, target: "/"})
	require.Equal(t, nethttp.StatusOK, w.Code)
	require.Equal(t, true, body["reached"])
}

func TestChainStopsAtAbort(t *testing.T) {
	t.Parallel()

	calls := 0
	count := func(*gin.Context) { calls++ }
	abort := func(c *gin.Context) { c.AbortWithStatus(nethttp.StatusTeapot) }

	w := runRaw(t, chain(count, abort, count), profileRequest{method: nethttp.MethodGet, target: "/"})
	require.Equal(t, nethttp.StatusTeapot, w.Code)
	require.Equal(t, 1, calls)
}
