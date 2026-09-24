package handlers

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/middleware"
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
	Logger      *slog.Logger
	Health      healthports.Service
	Auth        authports.Service
	Users       *userservice.UserService
	AdminUsers  adminUserAPI
	RoleAdmin   roleAPI
	Profiles    profileAPI
	EmailChange authports.EmailChanger
	// AvatarMaxBytes > 0 enables avatar upload and removal (requires media storage).
	AvatarMaxBytes int64
	Roles          RoleLookup
	RBAC           rbacports.Enforcer
	Posts          *postservice.PostService
	Categories     *categoryservice.CategoryService
	Tags           *tagservice.Service
	Media          mediaports.Service
	Comments       *commentservice.Service
	RateLimiter    middleware.Limiter
	CommentRates   CommentRates
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

	permCommentModerate = "comment.moderate"
	permProfileRead     = "user.profile.read"
	permProfileEdit     = "user.profile.edit"
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
	c := routes.Controllers{Stub: routes.NotImplemented}
	if deps.Health != nil {
		c.Health = routes.Health{
			Live:    Live(deps.Health),
			Ready:   ready(deps.Health),
			Version: version(deps.Version),
		}
	}

	if deps.Auth != nil {
		c.Auth = routes.Auth{
			Login:                 chain(middleware.RateLimit(deps.RateLimiter, deps.Logger, "auth.login", deps.LoginPerMinute, time.Minute), loginHandler(deps.Auth)),
			Refresh:               refreshHandler(deps.Auth),
			Logout:                logoutHandler(deps.Auth),
			PasswordForgot:        forgotPasswordHandler(deps.Auth),
			PasswordResetValidity: resetTokenValidityHandler(deps.Auth),
			PasswordReset:         resetPasswordHandler(deps.Auth),
			MePasswordUpdate:      changePasswordHandler(deps.Auth),
		}
	}

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

	wireProfiles(&c.Users, deps)

	if deps.Posts != nil {
		c.Posts = routes.Posts{
			PublicList:        listPublishedPostsHandler(deps.Posts),
			PublicGet:         getPublishedPostHandler(deps.Posts),
			AdminList:         gate(deps, "post.read", authorRoles, adminListPostsHandler(deps.Posts, deps.Roles)),
			AdminCreate:       gate(deps, "post.create", authorRoles, adminCreatePostHandler(deps.Posts)),
			AdminPublish:      gate(deps, "post.publish", editorRoles, adminPublishPostHandler(deps.Posts)),
			AdminUnpublish:    gate(deps, "post.publish", editorRoles, adminUnpublishPostHandler(deps.Posts)),
			AdminArchive:      gate(deps, "post.publish", editorRoles, adminArchivePostHandler(deps.Posts)),
			AdminUpdate:       gate(deps, "post.update", authorRoles, adminUpdatePostHandler(deps.Posts, deps.Roles)),
			AdminMediaReplace: gate(deps, "post.update", authorRoles, adminReplacePostMediaHandler(deps.Posts)),
			AdminDelete:       gate(deps, "post.delete", editorRoles, adminDeletePostHandler(deps.Posts)),
			AdminRestore:      gate(deps, "post.delete", editorRoles, adminRestorePostHandler(deps.Posts)),
		}
	}

	if deps.Categories != nil {
		c.Cats = routes.Categories{
			PublicList:  listCategoriesHandler(deps.Categories),
			PublicGet:   getCategoryHandler(deps.Categories),
			AdminList:   gate(deps, "category.read", editorRoles, chain(middleware.NoReadCache(), listCategoriesHandler(deps.Categories))),
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
			PublicGet:      publicGetMediaHandler(deps.Media),
			AdminCreate:    gate(deps, "media.create", authorRoles, chain(presignLimit, adminPresignMediaHandler(deps.Media))),
			AdminComplete:  gate(deps, "media.create", authorRoles, adminCompleteMediaHandler(deps.Media)),
			AdminList:      gate(deps, "media.create", authorRoles, adminListMediaHandler(deps.Media)),
			AdminTagsPatch: gate(deps, "media.create", authorRoles, adminPatchMediaTagsHandler(deps.Media)),
			AdminDelete:    gate(deps, "media.delete", editorRoles, adminDeleteMediaHandler(deps.Media)),
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

	return c
}

// wireProfiles binds profile, avatar, and email change handlers for the services that are present.
func wireProfiles(users *routes.Users, deps Deps) {
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
