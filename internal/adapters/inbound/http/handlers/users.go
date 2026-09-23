package handlers

import (
	"context"
	nethttp "net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/middleware"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	userservice "github.com/turahe/blog-api/internal/core/user/service"
)

type RoleLookup interface {
	ListRoleNames(ctx context.Context, userID uuid.UUID) ([]string, error)
}

type permissionEnforcer interface {
	Enforce(ctx context.Context, userID uuid.UUID, permission string) (bool, error)
}

func requirePermission(enforcer permissionEnforcer, permission string) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := middleware.CurrentUserID(c)
		if !ok {
			responses.Failure(c, nethttp.StatusUnauthorized, "unauthorized", "Authentication required")
			return
		}
		if enforcer == nil {
			responses.Failure(c, nethttp.StatusForbidden, "forbidden", "Authorization unavailable")
			return
		}
		allowed, err := enforcer.Enforce(c.Request.Context(), userID, permission)
		if err != nil {
			responses.Failure(c, nethttp.StatusInternalServerError, "internal_error", "Authorization check failed")
			return
		}
		if !allowed {
			responses.Failure(c, nethttp.StatusForbidden, "rbac.forbidden", "Insufficient permissions")
			return
		}
	}
}

func meGetHandler(users *userservice.UserService) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := middleware.CurrentUserID(c)
		if !ok {
			responses.Failure(c, nethttp.StatusUnauthorized, "unauthorized", "Authentication required")
			return
		}
		user, err := users.GetByID(c.Request.Context(), userID)
		if err != nil {
			responses.Failure(c, nethttp.StatusUnauthorized, "unauthorized", "User not found")
			return
		}
		responses.SuccessFor(c, nethttp.StatusOK, responses.ServiceUsers, responses.CaseSuccess, responses.User(user))
	}
}

func adminUsersListHandler(users *userservice.UserService) gin.HandlerFunc {
	return func(c *gin.Context) {
		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "20"))
		items, total, err := users.List(c.Request.Context(), page, perPage)
		if err != nil {
			responses.Failure(c, nethttp.StatusInternalServerError, "internal_error", "Failed to list users")
			return
		}
		out := make([]gin.H, 0, len(items))
		for _, user := range items {
			out = append(out, responses.User(user))
		}
		if page < 1 {
			page = 1
		}
		if perPage < 1 || perPage > 100 {
			perPage = 20
		}
		responses.SuccessPaginatedFor(c, nethttp.StatusOK, responses.ServiceUsers, out, page, perPage, total)
	}
}

func requireRoles(lookup RoleLookup, roles ...string) gin.HandlerFunc {
	allowed := map[string]struct{}{}
	for _, role := range roles {
		allowed[role] = struct{}{}
	}
	return func(c *gin.Context) {
		userID, ok := middleware.CurrentUserID(c)
		if !ok {
			responses.Failure(c, nethttp.StatusUnauthorized, "unauthorized", "Authentication required")
			return
		}
		names, err := lookup.ListRoleNames(c.Request.Context(), userID)
		if err != nil {
			responses.Failure(c, nethttp.StatusInternalServerError, "internal_error", "Failed to resolve roles")
			return
		}
		for _, name := range names {
			if _, ok := allowed[name]; ok {
				return
			}
		}
		responses.Failure(c, nethttp.StatusForbidden, "forbidden", "Insufficient permissions")
	}
}
