package middleware

import (
	nethttp "net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	"github.com/turahe/blog-api/internal/adapters/inbound/routes"
	"github.com/turahe/blog-api/internal/core/audit"
	auditdomain "github.com/turahe/blog-api/internal/core/audit/domain"
	auditports "github.com/turahe/blog-api/internal/core/audit/ports"
)

const (
	maxAuditUserAgent = 512
	categoryLogin     = "login"
)

// auditSkipped lists mutating operations that are not worth an entry: token
// refresh runs every few minutes per session, forgot-password must not reveal
// whether the account exists, and upvotes and SEO previews change nothing that
// needs accounting.
var auditSkipped = map[string]bool{
	"auth.refresh":            true,
	"auth.password.forgot":    true,
	"self.comments.upvote":    true,
	"admin.posts.seo.preview": true,
}

// auditFailures lists auth operations whose rejections (bad credentials, bad
// codes, rate limits) are recorded as failures.
var auditFailures = map[string]bool{
	"auth.login":          true,
	"admin.auth.login":    true,
	"auth.2fa.challenge":  true,
	"auth.oauth.callback": true,
	"auth.password.reset": true,
}

// auditAttempts lists admin operations whose 4xx rejections of a signed-in user are all
// recorded as failures (validation, permission, version conflict), not only auth
// rejections, together with the reasons the service attaches.
var auditAttempts = map[string]bool{
	"admin.settings.put":        true,
	"admin.impersonation.start": true,
	"admin.analytics.export":    true,
}

// auditCategories maps operations to the user-facing activity categories shown on
// /me/activity. Operations without a category are visible to admins only.
var auditCategories = map[string]string{
	"auth.login":                       categoryLogin,
	"admin.auth.login":                 categoryLogin,
	"auth.2fa.challenge":               categoryLogin,
	"auth.oauth.callback":              categoryLogin,
	"auth.logout":                      "logout",
	"auth.password.reset":              "password_reset",
	"admin.users.password.admin_reset": "password_reset",
	"me.password.update":               "password_change",
	"me.profile.patch":                 "profile_edit",
	"admin.users.profile.patch":        "profile_edit",
	"me.avatar.upload":                 "avatar_update",
	"me.avatar.delete":                 "avatar_update",
	"me.email.request_change":          "email_change",
	"me.email.confirm_change":          "email_change",
	"me.2fa.confirm":                   "twofa_enable",
	"me.2fa.disable":                   "twofa_disable",
	"me.2fa.backup_codes":              "twofa_backup_codes",
	"admin.users.roles.assign":         "role_change",
	"admin.users.roles.revoke":         "role_change",
	"admin.posts.create":               "post_create",
	"admin.posts.update":               "post_edit",
	"admin.posts.publish":              "post_publish",
	"public.posts.comments.create":     "comment_create",
	"admin.impersonation.start":        "impersonation_start",
	"admin.impersonation.stop":         "impersonation_end",
}

// resourceTypes maps the collection segment of an operation id to a resource type.
var resourceTypes = map[string]string{
	"users":         auditdomain.ResourceUser,
	"posts":         "post",
	"categories":    "category",
	"tags":          "tag",
	"comments":      "comment",
	"media":         "media",
	"roles":         "role",
	"settings":      "settings",
	"newsletter":    "newsletter",
	"analytics":     "analytics",
	"impersonation": "impersonation",
}

// Audit records one entry per audited mutating request after the handler
// returns: admin and self-service changes that succeeded, authentication events
// (including rejected logins), and public changes by a signed-in user. Every
// request made with an impersonation token is recorded, reads and failures
// included, so what staff saw as the user is accounted for. Handlers and
// services annotate the entry through the audit package. Recording never
// blocks the request.
func Audit(writer auditports.Writer) gin.HandlerFunc {
	return func(c *gin.Context) {
		if writer == nil {
			c.Next()
			return
		}

		ctx, scope := audit.WithScope(c.Request.Context())
		c.Request = c.Request.WithContext(ctx)

		c.Next()

		route, ok := routes.RouteOf(c)
		if !ok || (!isMutating(c.Request.Method) && !impersonating(c)) {
			return
		}

		entry, ok := auditEntry(c, route, scope)
		if ok {
			writer.Record(c.Request.Context(), entry)
		}
	}
}

func auditEntry(c *gin.Context, route routes.Route, scope *audit.Scope) (auditdomain.Entry, bool) {
	status := c.Writer.Status()
	success := status >= nethttp.StatusOK && status < nethttp.StatusMultipleChoices

	actor, signedIn := CurrentUserID(c)
	if id := scope.Actor(); id != nil {
		actor, signedIn = *id, true
	}

	if !impersonating(c) && !audited(route, status, success, signedIn) {
		return auditdomain.Entry{}, false
	}

	entry := auditdomain.Entry{
		UUID:       uuid.New(),
		Action:     route.OperationID,
		Category:   auditCategories[route.OperationID],
		Result:     auditdomain.ResultSuccess,
		Status:     status,
		IP:         c.ClientIP(),
		UserAgent:  truncate(c.Request.UserAgent(), maxAuditUserAgent),
		RequestID:  responses.RequestID(c),
		OccurredAt: time.Now().UTC(),
	}

	if !success {
		entry.Result = auditdomain.ResultFailure
	}

	if signedIn {
		entry.ActorID = &actor
	}

	entry.ResourceType, entry.ResourceID = inferResource(c, route, entry.ActorID)
	scope.Apply(&entry)
	markImpersonation(c, &entry)

	if c.GetBool(contextImpersonationRefused) {
		if entry.Metadata == nil {
			entry.Metadata = map[string]any{}
		}

		entry.Metadata["failure_reason"] = ErrorCodeImpersonationForbidden
	}

	return entry, true
}

func impersonating(c *gin.Context) bool {
	_, ok := CurrentImpersonation(c)
	return ok
}

// markImpersonation records the staff member behind an impersonation token, unless the
// entry is already attributed to them (stopping the session).
func markImpersonation(c *gin.Context, entry *auditdomain.Entry) {
	imp, ok := CurrentImpersonation(c)
	if !ok || (entry.ActorID != nil && *entry.ActorID == imp.ActorID) {
		return
	}

	entry.ImpersonatorID = &imp.ActorID

	if entry.Metadata == nil {
		entry.Metadata = map[string]any{}
	}

	entry.Metadata["impersonation_session_id"] = imp.SessionID.String()
}

func audited(route routes.Route, status int, success, signedIn bool) bool {
	if auditSkipped[route.OperationID] {
		return false
	}

	if !success {
		if auditAttempts[route.OperationID] {
			return signedIn && status < nethttp.StatusInternalServerError
		}

		return auditFailures[route.OperationID] && isRejection(status)
	}

	switch route.Group {
	case routes.GroupAdmin, routes.GroupSelfService:
		return true
	case routes.GroupAuth, routes.GroupPublic:
		// A successful login that still awaits a second factor has no actor yet.
		return signedIn
	case routes.GroupHealth, routes.GroupAnalytics:
		return false
	default:
		return false
	}
}

// inferResource derives the resource from the operation id and first path
// parameter; services override it through audit.SetResource.
func inferResource(c *gin.Context, route routes.Route, actor *uuid.UUID) (string, *uuid.UUID) {
	segments := strings.Split(route.OperationID, ".")
	if len(segments) < 2 {
		return "", nil
	}

	if segments[0] == "me" || segments[0] == "auth" {
		return auditdomain.ResourceUser, actor
	}

	resourceType, ok := resourceTypes[segments[1]]
	if !ok {
		resourceType = segments[1]
	}

	if id, err := uuid.Parse(c.Param("param1")); err == nil {
		return resourceType, &id
	}

	return resourceType, nil
}

func isMutating(method string) bool {
	switch method {
	case nethttp.MethodPost, nethttp.MethodPut, nethttp.MethodPatch, nethttp.MethodDelete:
		return true
	default:
		return false
	}
}

func isRejection(status int) bool {
	return status == nethttp.StatusUnauthorized || status == nethttp.StatusForbidden ||
		status == nethttp.StatusTooManyRequests
}

func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}

	return s[:limit]
}
