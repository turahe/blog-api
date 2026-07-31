package http

import (
	"log/slog"
	nethttp "net/http"

	"github.com/gin-gonic/gin"
	v1 "github.com/turahe/blog-api/internal/adapters/inbound/http/v1"
	authports "github.com/turahe/blog-api/internal/core/auth/ports"
	categoryservice "github.com/turahe/blog-api/internal/core/category/service"
	healthports "github.com/turahe/blog-api/internal/core/health/ports"
	mediaports "github.com/turahe/blog-api/internal/core/media/ports"
	postservice "github.com/turahe/blog-api/internal/core/post/service"
	rbacports "github.com/turahe/blog-api/internal/core/rbac/ports"
	tagservice "github.com/turahe/blog-api/internal/core/tag/service"
	userservice "github.com/turahe/blog-api/internal/core/user/service"
)

type Dependencies struct {
	Logger         *slog.Logger
	Health         healthports.Service
	Auth           authports.Service
	Users          *userservice.UserService
	Roles          roleLookup
	RBAC           rbacports.Enforcer
	Posts          *postservice.PostService
	Categories     *categoryservice.CategoryService
	Tags           *tagservice.Service
	Media          mediaports.Service
	Version        string
	TrustedProxies []string
}

func NewRouter(deps Dependencies) (*gin.Engine, error) {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.HandleMethodNotAllowed = true
	router.Use(
		requestIDMiddleware(),
		securityHeadersMiddleware(),
		accessLogMiddleware(deps.Logger),
		recoveryMiddleware(deps.Logger),
	)
	if err := router.SetTrustedProxies(deps.TrustedProxies); err != nil {
		return nil, err
	}

	if err := v1.Register(router, groupMiddleware(deps), handlerFor(deps)); err != nil {
		return nil, err
	}
	router.GET("/api/v1/health", liveHandler(deps))

	router.NoRoute(func(c *gin.Context) {
		failure(c, nethttp.StatusNotFound, "route.not_found", "Route not found")
	})
	router.NoMethod(func(c *gin.Context) {
		failure(c, nethttp.StatusMethodNotAllowed, "method.not_allowed", "Method not allowed")
	})
	return router, nil
}

func groupMiddleware(deps Dependencies) v1.Middleware {
	var required, optional gin.HandlersChain
	if deps.Auth != nil {
		required = gin.HandlersChain{bearerAuthMiddleware(deps.Auth)}
		optional = gin.HandlersChain{optionalBearerAuthMiddleware(deps.Auth)}
	}
	return v1.Middleware{
		ByAuth: map[v1.AuthMode]gin.HandlersChain{
			v1.AuthNone:     nil,
			v1.AuthOptional: optional,
			v1.AuthRequired: required,
		},
		ByGroup: map[v1.Group]gin.HandlersChain{
			v1.GroupHealth: nil, v1.GroupAuth: nil, v1.GroupMe: nil, v1.GroupSelf: nil,
			v1.GroupPublic: nil, v1.GroupAdmin: nil, v1.GroupAnalytics: nil,
		},
	}
}

func handlerFor(deps Dependencies) func(v1.Route) gin.HandlerFunc {
	implemented := map[string]gin.HandlerFunc{
		"health.live": liveHandler(deps), "health.ready": readyHandler(deps), "health.version": versionHandler(deps),
	}
	if deps.Auth != nil {
		implemented["auth.login"] = loginHandler(deps.Auth)
		implemented["auth.refresh"] = refreshHandler(deps.Auth)
		implemented["auth.logout"] = logoutHandler(deps.Auth)
		implemented["auth.password.forgot"] = forgotPasswordHandler(deps.Auth)
		implemented["auth.password.reset_token_validity"] = resetTokenValidityHandler(deps.Auth)
		implemented["auth.password.reset"] = resetPasswordHandler(deps.Auth)
		implemented["me.password.update"] = changePasswordHandler(deps.Auth)
	}
	if deps.Users != nil {
		implemented["me.get"] = meGetHandler(deps.Users)
		listUsers := adminUsersListHandler(deps.Users)
		if deps.RBAC != nil {
			implemented["admin.users.list"] = chain(requirePermission(deps.RBAC, "user.read"), listUsers)
		} else if deps.Roles != nil {
			implemented["admin.users.list"] = chain(requireRoles(deps.Roles, "admin", "editor"), listUsers)
		} else {
			implemented["admin.users.list"] = listUsers
		}
	}
	if deps.Posts != nil {
		implemented["public.posts.list"] = listPublishedPostsHandler(deps.Posts)
		implemented["public.posts.get"] = getPublishedPostHandler(deps.Posts)
		listPosts := adminListPostsHandler(deps.Posts, deps.Roles)
		createPost := adminCreatePostHandler(deps.Posts)
		publishPost := adminPublishPostHandler(deps.Posts)
		updatePost := adminUpdatePostHandler(deps.Posts, deps.Roles)
		replaceMedia := adminReplacePostMediaHandler(deps.Posts)
		if deps.RBAC != nil {
			implemented["admin.posts.list"] = chain(requirePermission(deps.RBAC, "post.read"), listPosts)
			implemented["admin.posts.create"] = chain(requirePermission(deps.RBAC, "post.create"), createPost)
			implemented["admin.posts.publish"] = chain(requirePermission(deps.RBAC, "post.publish"), publishPost)
			implemented["admin.posts.update"] = chain(requirePermission(deps.RBAC, "post.update"), updatePost)
			implemented["admin.posts.media.replace"] = chain(requirePermission(deps.RBAC, "post.update"), replaceMedia)
		} else if deps.Roles != nil {
			implemented["admin.posts.list"] = chain(requireRoles(deps.Roles, "admin", "editor", "author"), listPosts)
			implemented["admin.posts.create"] = chain(requireRoles(deps.Roles, "admin", "editor", "author"), createPost)
			implemented["admin.posts.publish"] = chain(requireRoles(deps.Roles, "admin", "editor"), publishPost)
			implemented["admin.posts.update"] = chain(requireRoles(deps.Roles, "admin", "editor", "author"), updatePost)
			implemented["admin.posts.media.replace"] = chain(requireRoles(deps.Roles, "admin", "editor", "author"), replaceMedia)
		} else {
			implemented["admin.posts.list"] = listPosts
			implemented["admin.posts.create"] = createPost
			implemented["admin.posts.publish"] = publishPost
			implemented["admin.posts.update"] = updatePost
			implemented["admin.posts.media.replace"] = replaceMedia
		}
	}
	if deps.Categories != nil {
		implemented["public.categories.list"] = listCategoriesHandler(deps.Categories)
		implemented["public.categories.get"] = getCategoryHandler(deps.Categories)
		createCategory := adminCreateCategoryHandler(deps.Categories)
		updateCategory := adminUpdateCategoryHandler(deps.Categories)
		deleteCategory := adminDeleteCategoryHandler(deps.Categories)
		reorderCategories := adminReorderCategoriesHandler(deps.Categories)
		if deps.RBAC != nil {
			implemented["admin.categories.create"] = chain(requirePermission(deps.RBAC, "category.create"), createCategory)
			implemented["admin.categories.update"] = chain(requirePermission(deps.RBAC, "category.update"), updateCategory)
			implemented["admin.categories.delete"] = chain(requirePermission(deps.RBAC, "category.delete"), deleteCategory)
			implemented["admin.categories.reorder"] = chain(requirePermission(deps.RBAC, "category.update"), reorderCategories)
		} else if deps.Roles != nil {
			gate := requireRoles(deps.Roles, "admin", "editor")
			implemented["admin.categories.create"] = chain(gate, createCategory)
			implemented["admin.categories.update"] = chain(gate, updateCategory)
			implemented["admin.categories.delete"] = chain(gate, deleteCategory)
			implemented["admin.categories.reorder"] = chain(gate, reorderCategories)
		} else {
			implemented["admin.categories.create"] = createCategory
			implemented["admin.categories.update"] = updateCategory
			implemented["admin.categories.delete"] = deleteCategory
			implemented["admin.categories.reorder"] = reorderCategories
		}
	}
	if deps.Tags != nil {
		implemented["public.tags.list"] = listTagsHandler(deps.Tags)
		createTag := adminCreateTagHandler(deps.Tags)
		updateTag := adminUpdateTagHandler(deps.Tags)
		mergeTag := adminMergeTagHandler(deps.Tags)
		deleteTag := adminDeleteTagHandler(deps.Tags)
		if deps.RBAC != nil {
			implemented["admin.tags.create"] = chain(requirePermission(deps.RBAC, "tag.create"), createTag)
			implemented["admin.tags.update"] = chain(requirePermission(deps.RBAC, "tag.update"), updateTag)
			implemented["admin.tags.merge"] = chain(requirePermission(deps.RBAC, "tag.update"), mergeTag)
			implemented["admin.tags.delete"] = chain(requirePermission(deps.RBAC, "tag.delete"), deleteTag)
		} else if deps.Roles != nil {
			gate := requireRoles(deps.Roles, "admin", "editor")
			implemented["admin.tags.create"] = chain(gate, createTag)
			implemented["admin.tags.update"] = chain(gate, updateTag)
			implemented["admin.tags.merge"] = chain(gate, mergeTag)
			implemented["admin.tags.delete"] = chain(gate, deleteTag)
		} else {
			implemented["admin.tags.create"] = createTag
			implemented["admin.tags.update"] = updateTag
			implemented["admin.tags.merge"] = mergeTag
			implemented["admin.tags.delete"] = deleteTag
		}
	}
	if deps.Media != nil {
		presignMedia := adminPresignMediaHandler(deps.Media)
		completeMedia := adminCompleteMediaHandler(deps.Media)
		listMedia := adminListMediaHandler(deps.Media)
		deleteMedia := adminDeleteMediaHandler(deps.Media)
		tagsMedia := adminPatchMediaTagsHandler(deps.Media)
		publicGet := publicGetMediaHandler(deps.Media)
		implemented["public.media.get"] = publicGet
		if deps.RBAC != nil {
			implemented["admin.media.create"] = chain(requirePermission(deps.RBAC, "media.create"), presignMedia)
			implemented["admin.media.complete"] = chain(requirePermission(deps.RBAC, "media.create"), completeMedia)
			implemented["admin.media.list"] = chain(requirePermission(deps.RBAC, "media.create"), listMedia)
			implemented["admin.media.tags.patch"] = chain(requirePermission(deps.RBAC, "media.create"), tagsMedia)
			implemented["admin.media.delete"] = chain(requirePermission(deps.RBAC, "media.delete"), deleteMedia)
		} else if deps.Roles != nil {
			implemented["admin.media.create"] = chain(requireRoles(deps.Roles, "admin", "editor", "author"), presignMedia)
			implemented["admin.media.complete"] = chain(requireRoles(deps.Roles, "admin", "editor", "author"), completeMedia)
			implemented["admin.media.list"] = chain(requireRoles(deps.Roles, "admin", "editor", "author"), listMedia)
			implemented["admin.media.tags.patch"] = chain(requireRoles(deps.Roles, "admin", "editor", "author"), tagsMedia)
			implemented["admin.media.delete"] = chain(requireRoles(deps.Roles, "admin", "editor"), deleteMedia)
		} else {
			implemented["admin.media.create"] = presignMedia
			implemented["admin.media.complete"] = completeMedia
			implemented["admin.media.list"] = listMedia
			implemented["admin.media.tags.patch"] = tagsMedia
			implemented["admin.media.delete"] = deleteMedia
		}
	}
	return func(route v1.Route) gin.HandlerFunc {
		if handler, ok := implemented[route.OperationID]; ok {
			return handler
		}
		return notImplementedHandler(route)
	}
}

func liveHandler(deps Dependencies) gin.HandlerFunc {
	return func(c *gin.Context) { success(c, nethttp.StatusOK, deps.Health.Live()) }
}

func readyHandler(deps Dependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		status := deps.Health.Ready(c.Request.Context())
		httpStatus := nethttp.StatusOK
		if status.Status != "ok" {
			httpStatus = nethttp.StatusServiceUnavailable
		}
		success(c, httpStatus, status)
	}
}

func versionHandler(deps Dependencies) gin.HandlerFunc {
	return func(c *gin.Context) { success(c, nethttp.StatusOK, gin.H{"version": deps.Version}) }
}

func notImplementedHandler(route v1.Route) gin.HandlerFunc {
	return func(c *gin.Context) {
		failureWithDetails(c, nethttp.StatusNotImplemented, "operation.not_implemented",
			"Operation is registered but not implemented",
			gin.H{"operation_id": route.OperationID, "route_group": route.Group, "auth_mode": route.Auth})
	}
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
