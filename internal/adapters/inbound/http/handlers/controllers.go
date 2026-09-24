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
	Logger       *slog.Logger
	Health       healthports.Service
	Auth         authports.Service
	Users        *userservice.UserService
	Roles        RoleLookup
	RBAC         rbacports.Enforcer
	Posts        *postservice.PostService
	Categories   *categoryservice.CategoryService
	Tags         *tagservice.Service
	Media        mediaports.Service
	Comments     *commentservice.Service
	RateLimiter  middleware.Limiter
	CommentRates CommentRates
	Version      string
}

// CommentRates are per-caller request budgets per minute; zero disables the limit.
type CommentRates struct {
	CreatePerMinute  int
	ActionsPerMinute int
}

// NewControllers builds domain handlers from deps for routes.Register*.
const (
	roleAdmin  = "admin"
	roleEditor = "editor"
	roleAuthor = "author"
)

// Fallback role sets for gate when no RBAC enforcer is wired.
var (
	editorRoles = []string{roleAdmin, roleEditor}
	authorRoles = []string{roleAdmin, roleEditor, roleAuthor}
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
			Login:                 loginHandler(deps.Auth),
			Refresh:               refreshHandler(deps.Auth),
			Logout:                logoutHandler(deps.Auth),
			PasswordForgot:        forgotPasswordHandler(deps.Auth),
			PasswordResetValidity: resetTokenValidityHandler(deps.Auth),
			PasswordReset:         resetPasswordHandler(deps.Auth),
			MePasswordUpdate:      changePasswordHandler(deps.Auth),
		}
	}

	if deps.Users != nil {
		c.Users = routes.Users{
			MeGet:          meGetHandler(deps.Users),
			AdminUsersList: gate(deps, "user.read", editorRoles, adminUsersListHandler(deps.Users)),
		}
	}

	if deps.Posts != nil {
		c.Posts = routes.Posts{
			PublicList:        listPublishedPostsHandler(deps.Posts),
			PublicGet:         getPublishedPostHandler(deps.Posts),
			AdminList:         gate(deps, "post.read", authorRoles, adminListPostsHandler(deps.Posts, deps.Roles)),
			AdminCreate:       gate(deps, "post.create", authorRoles, adminCreatePostHandler(deps.Posts)),
			AdminPublish:      gate(deps, "post.publish", editorRoles, adminPublishPostHandler(deps.Posts)),
			AdminUpdate:       gate(deps, "post.update", authorRoles, adminUpdatePostHandler(deps.Posts, deps.Roles)),
			AdminMediaReplace: gate(deps, "post.update", authorRoles, adminReplacePostMediaHandler(deps.Posts)),
		}
	}

	if deps.Categories != nil {
		c.Cats = routes.Categories{
			PublicList:  listCategoriesHandler(deps.Categories),
			PublicGet:   getCategoryHandler(deps.Categories),
			AdminList:   gate(deps, "category.read", editorRoles, listCategoriesHandler(deps.Categories)),
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
		c.Media = routes.Media{
			PublicGet:      publicGetMediaHandler(deps.Media),
			AdminCreate:    gate(deps, "media.create", authorRoles, adminPresignMediaHandler(deps.Media)),
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
		}
	}

	return c
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
