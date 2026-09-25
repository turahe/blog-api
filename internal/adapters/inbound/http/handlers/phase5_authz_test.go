package handlers

import (
	nethttp "net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/adapters/inbound/routes"
	impdomain "github.com/turahe/blog-api/internal/core/impersonation/domain"
	nldomain "github.com/turahe/blog-api/internal/core/newsletter/domain"
	postservice "github.com/turahe/blog-api/internal/core/post/service"
)

// gatedOp is an admin operation guarded by one RBAC permission, with the fallback roles that
// pass it when no enforcer is wired.
type gatedOp struct {
	handler    func(routes.Controllers) gin.HandlerFunc
	permission string
	roles      []string
}

func phase5Deps() Deps {
	return Deps{
		Posts:         postservice.New(nil, nil, nil),
		Settings:      &fakeSettings{},
		Media:         &fakeMediaService{},
		Newsletter:    &fakeNewsletter{},
		Impersonation: &fakeImpersonation{},
	}
}

func phase5AdminOps() map[string]gatedOp {
	return map[string]gatedOp{
		"admin.settings.get":     {func(c routes.Controllers) gin.HandlerFunc { return c.Settings.Get }, permSettingsRead, adminRoles},
		"admin.settings.put":     {func(c routes.Controllers) gin.HandlerFunc { return c.Settings.Put }, permSettingsUpdate, adminRoles},
		"admin.settings.history": {func(c routes.Controllers) gin.HandlerFunc { return c.Settings.History }, permSettingsHistory, adminRoles},

		"admin.posts.revisions.list":    {func(c routes.Controllers) gin.HandlerFunc { return c.Posts.RevisionsList }, permPostRevisionsView, authorRoles},
		"admin.posts.revisions.get":     {func(c routes.Controllers) gin.HandlerFunc { return c.Posts.RevisionGet }, permPostRevisionsView, authorRoles},
		"admin.posts.revisions.restore": {func(c routes.Controllers) gin.HandlerFunc { return c.Posts.RevisionRestore }, permPostRevisionsRestore, authorRoles},
		"admin.posts.seo.get":           {func(c routes.Controllers) gin.HandlerFunc { return c.Posts.SEOGet }, permPostSEOView, authorRoles},
		"admin.posts.seo.update":        {func(c routes.Controllers) gin.HandlerFunc { return c.Posts.SEOUpdate }, permPostSEOEdit, authorRoles},
		"admin.posts.seo.preview":       {func(c routes.Controllers) gin.HandlerFunc { return c.Posts.SEOPreview }, permPostSEOView, authorRoles},

		"admin.media.usage": {func(c routes.Controllers) gin.HandlerFunc { return c.Media.AdminUsage }, permMediaUsageRead, editorRoles},

		"admin.impersonation.start": {func(c routes.Controllers) gin.HandlerFunc { return c.Impersonation.Start }, impdomain.PermissionStart, adminRoles},

		"admin.newsletter.subscribers.list":    {func(c routes.Controllers) gin.HandlerFunc { return c.Newsletter.AdminSubscribersList }, nldomain.PermSubscribersRead, editorRoles},
		"admin.newsletter.subscribers.get":     {func(c routes.Controllers) gin.HandlerFunc { return c.Newsletter.AdminSubscriberGet }, nldomain.PermSubscribersRead, editorRoles},
		"admin.newsletter.subscribers.delete":  {func(c routes.Controllers) gin.HandlerFunc { return c.Newsletter.AdminSubscriberDelete }, nldomain.PermSubscribersErase, adminRoles},
		"admin.newsletter.issues.list":         {func(c routes.Controllers) gin.HandlerFunc { return c.Newsletter.AdminIssuesList }, nldomain.PermIssuesRead, editorRoles},
		"admin.newsletter.issues.send":         {func(c routes.Controllers) gin.HandlerFunc { return c.Newsletter.AdminIssueCreate }, nldomain.PermIssuesEdit, editorRoles},
		"admin.newsletter.issues.get":          {func(c routes.Controllers) gin.HandlerFunc { return c.Newsletter.AdminIssueGet }, nldomain.PermIssuesRead, editorRoles},
		"admin.newsletter.issues.patch":        {func(c routes.Controllers) gin.HandlerFunc { return c.Newsletter.AdminIssuePatch }, nldomain.PermIssuesEdit, editorRoles},
		"admin.newsletter.provider_config.get": {func(c routes.Controllers) gin.HandlerFunc { return c.Newsletter.AdminConfigGet }, nldomain.PermConfigRead, adminRoles},
		"admin.newsletter.provider_config.put": {func(c routes.Controllers) gin.HandlerFunc { return c.Newsletter.AdminConfigPut }, nldomain.PermConfigUpdate, adminRoles},
	}
}

func TestPhase5AdminOperationsCheckTheirPermission(t *testing.T) {
	t.Parallel()

	for op, tc := range phase5AdminOps() {
		enforcer := &recordingEnforcer{}
		deps := phase5Deps()
		deps.RBAC = enforcer
		handler := tc.handler(NewControllers(deps))

		w, body := runProfile(t, handler, profileRequest{method: nethttp.MethodPost, target: "/", body: `{}`, user: &testUserID})
		require.Equal(t, nethttp.StatusForbidden, w.Code, op)
		require.Equal(t, "rbac.forbidden", errorCode(body), op)
		require.Equal(t, []string{tc.permission}, enforcer.checked, op)

		w, _ = runProfile(t, handler, profileRequest{method: nethttp.MethodPost, target: "/", body: `{}`})
		require.Equal(t, nethttp.StatusUnauthorized, w.Code, op)
	}
}

func TestPhase5AdminOperationsFallBackToRoles(t *testing.T) {
	t.Parallel()

	// The strongest role each fallback set leaves out.
	outside := map[string]string{"admin": roleEditor, "editor": roleAuthor, "author": "subscriber"}

	for op, tc := range phase5AdminOps() {
		deps := phase5Deps()
		deps.Roles = fakeRoleLookup{names: []string{outside[tc.roles[len(tc.roles)-1]]}}
		handler := tc.handler(NewControllers(deps))

		w, body := runProfile(t, handler, profileRequest{method: nethttp.MethodPost, target: "/", body: `{}`, user: &testUserID})
		require.Equal(t, nethttp.StatusForbidden, w.Code, op)
		require.Equal(t, "forbidden", errorCode(body), op)
	}
}

func TestImpersonationStopAndCurrentNeedOnlyASignedInCaller(t *testing.T) {
	t.Parallel()

	enforcer := &recordingEnforcer{}
	deps := phase5Deps()
	deps.RBAC = enforcer
	c := NewControllers(deps).Impersonation

	for name, handler := range map[string]gin.HandlerFunc{"stop": c.Stop, "current": c.Current} {
		w, _ := runProfile(t, handler, profileRequest{method: nethttp.MethodPost, target: "/", body: `{}`})
		require.Equal(t, nethttp.StatusUnauthorized, w.Code, name)

		w, _ = runProfile(t, handler, profileRequest{method: nethttp.MethodPost, target: "/", body: `{}`, user: &testUserID})
		require.NotEqual(t, nethttp.StatusForbidden, w.Code, name)
	}

	require.Empty(t, enforcer.checked, "an impersonator who lost impersonation.start can still end the session")
}
