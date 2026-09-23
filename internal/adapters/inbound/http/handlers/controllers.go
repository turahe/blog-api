package handlers

import (
	"github.com/gin-gonic/gin"
	"github.com/turahe/blog-api/internal/adapters/inbound/routes"
	authports "github.com/turahe/blog-api/internal/core/auth/ports"
	categoryservice "github.com/turahe/blog-api/internal/core/category/service"
	healthports "github.com/turahe/blog-api/internal/core/health/ports"
	mediaports "github.com/turahe/blog-api/internal/core/media/ports"
	postservice "github.com/turahe/blog-api/internal/core/post/service"
	rbacports "github.com/turahe/blog-api/internal/core/rbac/ports"
	tagservice "github.com/turahe/blog-api/internal/core/tag/service"
	userservice "github.com/turahe/blog-api/internal/core/user/service"
)

// Deps are the services controllers need when wiring routes.
type Deps struct {
	Health     healthports.Service
	Auth       authports.Service
	Users      *userservice.UserService
	Roles      RoleLookup
	RBAC       rbacports.Enforcer
	Posts      *postservice.PostService
	Categories *categoryservice.CategoryService
	Tags       *tagservice.Service
	Media      mediaports.Service
	Version    string
}

// NewControllers builds domain handlers from deps for routes.Register*.
func NewControllers(deps Deps) routes.Controllers {
	c := routes.Controllers{Stub: routes.NotImplemented}
	if deps.Health != nil {
		c.Health = routes.Health{
			Live:    Live(deps.Health),
			Ready:   Ready(deps.Health),
			Version: Version(deps.Version),
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
			AdminUsersList: gate(deps, "user.read", []string{"admin", "editor"}, adminUsersListHandler(deps.Users)),
		}
	}
	if deps.Posts != nil {
		authorRoles := []string{"admin", "editor", "author"}
		c.Posts = routes.Posts{
			PublicList:        listPublishedPostsHandler(deps.Posts),
			PublicGet:         getPublishedPostHandler(deps.Posts),
			AdminList:         gate(deps, "post.read", authorRoles, adminListPostsHandler(deps.Posts, deps.Roles)),
			AdminCreate:       gate(deps, "post.create", authorRoles, adminCreatePostHandler(deps.Posts)),
			AdminPublish:      gate(deps, "post.publish", []string{"admin", "editor"}, adminPublishPostHandler(deps.Posts)),
			AdminUpdate:       gate(deps, "post.update", authorRoles, adminUpdatePostHandler(deps.Posts, deps.Roles)),
			AdminMediaReplace: gate(deps, "post.update", authorRoles, adminReplacePostMediaHandler(deps.Posts)),
		}
	}
	if deps.Categories != nil {
		editorRoles := []string{"admin", "editor"}
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
		editorRoles := []string{"admin", "editor"}
		c.Tags = routes.Tags{
			PublicList:  listTagsHandler(deps.Tags),
			AdminCreate: gate(deps, "tag.create", editorRoles, adminCreateTagHandler(deps.Tags)),
			AdminUpdate: gate(deps, "tag.update", editorRoles, adminUpdateTagHandler(deps.Tags)),
			AdminMerge:  gate(deps, "tag.update", editorRoles, adminMergeTagHandler(deps.Tags)),
			AdminDelete: gate(deps, "tag.delete", editorRoles, adminDeleteTagHandler(deps.Tags)),
		}
	}
	if deps.Media != nil {
		authorRoles := []string{"admin", "editor", "author"}
		editorRoles := []string{"admin", "editor"}
		c.Media = routes.Media{
			PublicGet:      publicGetMediaHandler(deps.Media),
			AdminCreate:    gate(deps, "media.create", authorRoles, adminPresignMediaHandler(deps.Media)),
			AdminComplete:  gate(deps, "media.create", authorRoles, adminCompleteMediaHandler(deps.Media)),
			AdminList:      gate(deps, "media.create", authorRoles, adminListMediaHandler(deps.Media)),
			AdminTagsPatch: gate(deps, "media.create", authorRoles, adminPatchMediaTagsHandler(deps.Media)),
			AdminDelete:    gate(deps, "media.delete", editorRoles, adminDeleteMediaHandler(deps.Media)),
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
