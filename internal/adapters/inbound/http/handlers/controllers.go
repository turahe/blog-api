package handlers

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/middleware"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	"github.com/turahe/blog-api/internal/adapters/inbound/routes"
	authports "github.com/turahe/blog-api/internal/core/auth/ports"
	categoryservice "github.com/turahe/blog-api/internal/core/category/service"
	commentservice "github.com/turahe/blog-api/internal/core/comment/service"
	healthports "github.com/turahe/blog-api/internal/core/health/ports"
	mediaports "github.com/turahe/blog-api/internal/core/media/ports"
	postservice "github.com/turahe/blog-api/internal/core/post/service"
	rbacports "github.com/turahe/blog-api/internal/core/rbac/ports"
	tagservice "github.com/turahe/blog-api/internal/core/tag/service"
	userservice "github.com/turahe/blog-api/internal/core/user/service"
)

// Deps are the services controllers need when wiring routes.
type Deps struct {
	Logger     *slog.Logger
	Health     healthports.Service
	Auth       authports.Service
	Users      *userservice.UserService
	AdminUsers adminUserAPI
	TwoFactor  twoFactorAPI
	AdminLogin adminLoginAPI
	OAuth      oauthAPI
	RoleAdmin  roleAPI
	Activity   activityAPI
	// Notifications serves the caller's in-app inbox; nil keeps the routes as 501 stubs.
	Notifications notificationAPI
	// NotificationStream feeds the SSE stream; nil answers 503 (no message broker).
	NotificationStream notificationStreamHub
	SSEPingInterval    time.Duration
	Profiles           profileAPI
	// Privacy serves /me/privacy; nil keeps the routes as 501 stubs.
	Privacy privacyAPI
	// PrivacyRequests queues data exports and erasures; nil keeps the routes as 501 stubs.
	PrivacyRequests privacyRequestsAPI
	// Impersonation serves /admin/impersonation; nil keeps the routes as 501 stubs.
	Impersonation impersonationAPI
	// Newsletter serves /newsletter, /me/newsletter, and /admin/newsletter; nil keeps them as 501 stubs.
	Newsletter newsletterAPI
	// NewsletterProvider is the read-only delivery provider shown in the admin provider config.
	NewsletterProvider responses.NewsletterProvider
	EmailChange        authports.EmailChanger
	// AvatarMaxBytes > 0 enables avatar upload and removal (requires media storage).
	AvatarMaxBytes int64
	Roles          RoleLookup
	RBAC           rbacports.Enforcer
	Posts          *postservice.PostService
	Categories     *categoryservice.CategoryService
	Tags           *tagservice.Service
	Media          mediaports.Service
	Comments       *commentservice.Service
	Settings       settingsAPI
	SettingsValues settingsValues
	// Consent records analytics consent; nil keeps the consent routes as 501 stubs.
	Consent      consentAPI
	RateLimiter  middleware.Limiter
	CommentRates CommentRates
	// LoginPerMinute is the per-IP budget for auth.login; zero disables the limit.
	LoginPerMinute int
	Version        string
}

// CommentRates are per-caller request budgets per minute; zero disables the limit.
type CommentRates struct {
	CreatePerMinute  int
	ActionsPerMinute int
}

// NewControllers builds domain handlers from deps for routes.Register*.
const (
	roleAdmin     = "admin"
	roleEditor    = "editor"
	roleAuthor    = "author"
	roleModerator = "moderator"

	permCommentModerate  = "comment.moderate"
	permProfileRead      = "user.profile.read"
	permProfileEdit      = "user.profile.edit"
	permSettingsRead     = "settings.read"
	permSettingsUpdate   = "settings.update"
	permSettingsHistory  = "settings.history.read"
	settingsPutPerMinute = 60

	permPostRevisionsView    = "post.revisions.view"
	permPostRevisionsViewAll = "post.revisions.view_all"
	permPostRevisionsRestore = "post.revisions.restore"
	permPostSEOView          = "post.seo.view"
	permPostSEOEdit          = "post.seo.edit"
	permPostSlugEdit         = "post.slug.edit"
	permMediaUsageRead       = "media.usage.read"
)

// Fallback role sets for gate when no RBAC enforcer is wired.
var (
	editorRoles     = []string{roleAdmin, roleEditor}
	authorRoles     = []string{roleAdmin, roleEditor, roleAuthor}
	moderatorRoles  = []string{roleAdmin, roleEditor, roleModerator}
	hardDeleteRoles = []string{roleAdmin, roleModerator}
	adminRoles      = []string{roleAdmin}
)

// NewControllers builds domain handlers from deps for routes.Register*; nil services fall back to 501 stubs.
func NewControllers(deps Deps) routes.Controllers {
	c := routes.Controllers{Stub: routes.NotImplemented, Guard: middleware.ImpersonationGuard()}
	if deps.Health != nil {
		c.Health = routes.Health{
			Live:    Live(deps.Health),
			Ready:   ready(deps.Health),
			Version: version(deps.Version),
		}
	}

	c.Auth = authControllers(deps)

	if deps.Users != nil {
		c.Users.MeGet = meGetHandler(deps.Users)
		c.Users.AdminUsersList = gate(deps, "user.read", editorRoles, adminUsersListHandler(deps.Users))
	}

	if deps.AdminUsers != nil {
		canManageRoles := func(c *gin.Context) bool { return holds(c, deps, permRoleManage, adminRoles) }
		c.Users.AdminCreate = gate(deps, "user.create", adminRoles, adminCreateUserHandler(deps.AdminUsers, canManageRoles))
		c.Users.AdminPasswordReset = gate(deps, "user.password.admin_reset", adminRoles, adminResetPasswordHandler(deps.AdminUsers))
	}

	if r := deps.RoleAdmin; r != nil {
		c.Roles = routes.Roles{
			List:            gate(deps, permRoleRead, adminRoles, adminRolesListHandler(r)),
			Get:             gate(deps, permRoleRead, adminRoles, adminRoleGetHandler(r)),
			Create:          gate(deps, permRoleManage, adminRoles, adminRoleCreateHandler(r)),
			Update:          gate(deps, permRoleManage, adminRoles, adminRoleUpdateHandler(r)),
			Delete:          gate(deps, permRoleManage, adminRoles, adminRoleDeleteHandler(r)),
			SetPermissions:  gate(deps, permRoleManage, adminRoles, adminRolePermissionsSetHandler(r)),
			Permissions:     gate(deps, permRoleRead, adminRoles, adminPermissionsListHandler(r)),
			UserRolesList:   gate(deps, permRoleRead, adminRoles, adminUserRolesListHandler(r)),
			UserRolesAssign: gate(deps, permRoleManage, adminRoles, adminUserRolesAssignHandler(r)),
			UserRoleRevoke:  gate(deps, permRoleManage, adminRoles, adminUserRoleRevokeHandler(r)),
		}
	}

	wireProfiles(&c.Users, &c.Activity, deps)

	c.Activity.MeList, c.Activity.AdminUserList = activityControllers(deps)
	c.Analytics.ConsentStore, c.Analytics.ConsentGet, c.Analytics.ConsentWithdraw, c.Analytics.IngestGate = consentControllers(deps)
	c.Notifications = notificationControllers(deps)
	c.Impersonation = impersonationControllers(deps)
	c.Newsletter = newsletterControllers(deps)

	c.Posts = postControllers(deps)

	if deps.Categories != nil {
		c.Cats = routes.Categories{
			PublicList:  listCategoriesHandler(deps.Categories),
			PublicGet:   getCategoryHandler(deps.Categories),
			AdminList:   gate(deps, "category.read", editorRoles, chain(middleware.NoReadCache(), adminListCategoriesHandler(deps.Categories))),
			AdminCreate: gate(deps, "category.create", editorRoles, adminCreateCategoryHandler(deps.Categories)),
			AdminUpdate: gate(deps, "category.update", editorRoles, adminUpdateCategoryHandler(deps.Categories)),
			AdminDelete: gate(deps, "category.delete", editorRoles, adminDeleteCategoryHandler(deps.Categories)),
			AdminMove:   gate(deps, "category.update", editorRoles, adminMoveCategoryHandler(deps.Categories)),
		}
	}

	if deps.Tags != nil {
		c.Tags = routes.Tags{
			PublicList:  listTagsHandler(deps.Tags),
			AdminCreate: gate(deps, "tag.create", editorRoles, adminCreateTagHandler(deps.Tags)),
			AdminUpdate: gate(deps, "tag.update", editorRoles, adminUpdateTagHandler(deps.Tags)),
			AdminMerge:  gate(deps, "tag.update", editorRoles, adminMergeTagHandler(deps.Tags)),
			AdminDelete: gate(deps, "tag.delete", editorRoles, adminDeleteTagHandler(deps.Tags)),
		}
	}

	if deps.Media != nil {
		presignLimit := middleware.RateLimit(deps.RateLimiter, deps.Logger, "media.presign", 60, time.Minute)
		c.Media = routes.Media{
			PublicGet:       publicGetMediaHandler(deps.Media),
			PublicTransform: publicTransformMediaHandler(deps.Media),
			AdminCreate:     gate(deps, "media.create", authorRoles, chain(presignLimit, adminPresignMediaHandler(deps.Media))),
			AdminComplete:   gate(deps, "media.create", authorRoles, adminCompleteMediaHandler(deps.Media)),
			AdminList:       gate(deps, "media.create", authorRoles, adminListMediaHandler(deps.Media)),
			AdminTagsPatch:  gate(deps, "media.create", authorRoles, adminPatchMediaTagsHandler(deps.Media)),
			AdminDelete:     gate(deps, "media.delete", editorRoles, adminDeleteMediaHandler(deps.Media)),
			AdminUsage:      gate(deps, permMediaUsageRead, editorRoles, adminMediaUsageHandler(deps.Media)),
		}
	}

	if deps.Comments != nil {
		limit := func(bucket string, perMinute int, handler gin.HandlerFunc) gin.HandlerFunc {
			return chain(middleware.RateLimit(deps.RateLimiter, deps.Logger, bucket, perMinute, time.Minute), handler)
		}
		actions := deps.CommentRates.ActionsPerMinute
		c.Comments = routes.Comments{
			PostList:   listPostCommentsHandler(deps.Comments),
			PostCreate: limit("comments.create", deps.CommentRates.CreatePerMinute, createPostCommentHandler(deps.Comments)),
			Get:        getCommentHandler(deps.Comments),
			Flag:       limit("comments.flag", actions, flagCommentHandler(deps.Comments)),
			MeList:     listMyCommentsHandler(deps.Comments),
			Patch:      patchCommentHandler(deps.Comments),
			Delete:     deleteCommentHandler(deps.Comments),
			Upvote:     limit("comments.upvote", actions, upvoteCommentHandler(deps.Comments)),

			AdminList:         gate(deps, permCommentModerate, moderatorRoles, adminListCommentsHandler(deps.Comments)),
			AdminGet:          gate(deps, permCommentModerate, moderatorRoles, adminGetCommentHandler(deps.Comments)),
			AdminStats:        gate(deps, permCommentModerate, moderatorRoles, adminCommentStatsHandler(deps.Comments)),
			AdminModerate:     gate(deps, permCommentModerate, moderatorRoles, adminModerateCommentHandler(deps.Comments)),
			AdminBulkModerate: gate(deps, permCommentModerate, moderatorRoles, adminBulkModerateCommentsHandler(deps.Comments)),
			AdminHardDelete:   gate(deps, "comment.delete", hardDeleteRoles, adminHardDeleteCommentHandler(deps.Comments)),
		}
	}

	c.Settings = settingsControllers(deps)

	return c
}

// postControllers wires the post handlers when the service is present. Every admin write
// runs under withPostEditor so its revision names the signed-in user.
func postControllers(deps Deps) routes.Posts {
	p := deps.Posts
	if p == nil {
		return routes.Posts{}
	}

	write := func(permission string, roles []string, handler gin.HandlerFunc) gin.HandlerFunc {
		return gate(deps, permission, roles, withPostEditor(handler))
	}
	allPosts := func(c *gin.Context) bool { return holds(c, deps, permPostRevisionsViewAll, editorRoles) }
	seo := postSEOAccess(deps)

	return routes.Posts{
		PublicList:        listPublishedPostsHandler(p),
		PublicGet:         getPublishedPostHandler(p),
		AdminList:         gate(deps, "post.read", authorRoles, adminListPostsHandler(p, deps.Roles)),
		AdminCreate:       write("post.create", authorRoles, adminCreatePostHandler(p)),
		AdminPublish:      write("post.publish", editorRoles, adminPublishPostHandler(p)),
		AdminUnpublish:    write("post.publish", editorRoles, adminUnpublishPostHandler(p)),
		AdminArchive:      write("post.publish", editorRoles, adminArchivePostHandler(p)),
		AdminUpdate:       write("post.update", authorRoles, adminUpdatePostHandler(p, deps.Roles)),
		AdminMediaReplace: write("post.update", authorRoles, adminReplacePostMediaHandler(p, deps.Media)),
		AdminDelete:       write("post.delete", editorRoles, adminDeletePostHandler(p)),
		AdminRestore:      write("post.delete", editorRoles, adminRestorePostHandler(p)),
		RevisionsList:     gate(deps, permPostRevisionsView, authorRoles, adminListPostRevisionsHandler(p, allPosts)),
		RevisionGet:       gate(deps, permPostRevisionsView, authorRoles, adminGetPostRevisionHandler(p, allPosts)),
		RevisionRestore:   write(permPostRevisionsRestore, authorRoles, adminRestorePostRevisionHandler(p, allPosts)),
		SEOGet:            gate(deps, permPostSEOView, authorRoles, adminGetPostSEOHandler(p, seo)),
		SEOUpdate:         write(permPostSEOEdit, authorRoles, adminUpdatePostSEOHandler(p, seo)),
		SEOPreview:        gate(deps, permPostSEOView, authorRoles, adminPreviewPostSEOHandler(p, seo)),
		PublicSEOMeta:     publicPostSEOMetaHandler(p),
	}
}

// settingsControllers wires the admin settings handlers when the service is present.
func settingsControllers(deps Deps) routes.Settings {
	s := deps.Settings
	if s == nil {
		return routes.Settings{}
	}

	canUpdate := func(c *gin.Context) bool { return holds(c, deps, permSettingsUpdate, adminRoles) }
	putLimit := middleware.RateLimit(deps.RateLimiter, deps.Logger, "settings.update", settingsPutPerMinute, time.Minute)

	return routes.Settings{
		Get:     gate(deps, permSettingsRead, adminRoles, adminGetSettingsHandler(s, canUpdate)),
		Put:     gate(deps, permSettingsUpdate, adminRoles, chain(putLimit, adminUpdateSettingsHandler(s))),
		History: gate(deps, permSettingsHistory, adminRoles, adminSettingsHistoryHandler(s)),
	}
}

// wireProfiles binds profile, avatar, privacy, and email change handlers for the services that are present.
func wireProfiles(users *routes.Users, activity *routes.Activity, deps Deps) {
	limit := func(bucket string, n int, window time.Duration, handler gin.HandlerFunc) gin.HandlerFunc {
		return chain(middleware.RateLimit(deps.RateLimiter, deps.Logger, bucket, n, window), handler)
	}

	if deps.Profiles != nil {
		privileged := func(c *gin.Context) bool { return holds(c, deps, permProfileRead, adminRoles) }
		users.MeProfilePatch = limit("profile.update", 30, time.Minute, mePatchProfileHandler(deps.Profiles))
		users.PublicProfile = publicUserProfileHandler(deps.Profiles, privileged)
		users.AdminProfileGet = gate(deps, permProfileRead, adminRoles, adminGetProfileHandler(deps.Profiles))
		users.AdminProfilePatch = gate(deps, permProfileEdit, adminRoles, adminPatchProfileHandler(deps.Profiles))
	}

	if deps.Profiles != nil && deps.AvatarMaxBytes > 0 {
		users.MeAvatarUpload = limit("profile.avatar", 10, time.Minute, meUploadAvatarHandler(deps.Profiles, deps.AvatarMaxBytes))
		users.MeAvatarDelete = limit("profile.avatar", 10, time.Minute, meDeleteAvatarHandler(deps.Profiles))
	}

	if deps.PrivacyRequests != nil {
		activity.MeExport = limit("privacy.export", 30, time.Minute, meExportHandler(deps.PrivacyRequests))
		activity.MeErase = limit("privacy.erase", 5, time.Hour, meEraseHandler(deps.PrivacyRequests))
	}

	if deps.Privacy != nil {
		users.MePrivacyGet = meGetPrivacyHandler(deps.Privacy)
		users.MePrivacyUpdate = limit("privacy.update", 10, time.Minute, meUpdatePrivacyHandler(deps.Privacy))
	}

	if deps.EmailChange != nil {
		users.MeEmailRequestChange = limit("email.change.request", 3, time.Hour, meRequestEmailChangeHandler(deps.EmailChange))
		users.MeEmailConfirmChange = limit("email.change.confirm", 10, time.Minute, meConfirmEmailChangeHandler(deps.EmailChange))
	}
}

func gate(deps Deps, permission string, roles []string, handler gin.HandlerFunc) gin.HandlerFunc {
	if deps.RBAC != nil {
		return chain(requirePermission(deps.RBAC, permission), handler)
	}

	if deps.Roles != nil {
		return chain(requireRoles(deps.Roles, roles...), handler)
	}

	return handler
}

func chain(handlers ...gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		for _, handler := range handlers {
			handler(c)

			if c.IsAborted() {
				return
			}
		}
	}
}

// authControllers wires login, password and two-factor handlers.
func authControllers(deps Deps) routes.Auth {
	limit := func(bucket string) gin.HandlerFunc {
		return middleware.RateLimit(deps.RateLimiter, deps.Logger, bucket, deps.LoginPerMinute, time.Minute)
	}

	var a routes.Auth

	if deps.Auth != nil {
		password := limit("auth.password")
		a = routes.Auth{
			Login:                 chain(limit("auth.login"), loginHandler(deps.Auth)),
			Refresh:               chain(limit("auth.refresh"), refreshHandler(deps.Auth)),
			Logout:                logoutHandler(deps.Auth),
			PasswordForgot:        chain(password, forgotPasswordHandler(deps.Auth)),
			PasswordResetValidity: chain(password, resetTokenValidityHandler(deps.Auth)),
			PasswordReset:         chain(password, resetPasswordHandler(deps.Auth)),
			MePasswordUpdate:      changePasswordHandler(deps.Auth),
		}
	}

	if deps.AdminLogin != nil {
		a.AdminLogin = chain(limit("admin.auth.login"), adminLoginHandler(deps.AdminLogin))
	}

	if deps.OAuth != nil {
		oauth := limit("auth.oauth")
		a.OAuthStart = chain(oauth, oauthStartHandler(deps.OAuth))
		a.OAuthCallback = chain(oauth, oauthCallbackHandler(deps.OAuth))
	}

	if mfa := deps.TwoFactor; mfa != nil {
		a.TwoFactorChallenge = chain(limit("auth.2fa"), twoFactorChallengeHandler(mfa))
		a.MeTwoFactorGet = meTwoFactorGetHandler(mfa)
		a.MeTwoFactorSetup = meTwoFactorSetupHandler(mfa)
		a.MeTwoFactorConfirm = meTwoFactorConfirmHandler(mfa)
		a.MeTwoFactorDisable = meTwoFactorDisableHandler(mfa)
		a.MeTwoFactorBackupCodes = meTwoFactorBackupCodesHandler(mfa)
	}

	return a
}
