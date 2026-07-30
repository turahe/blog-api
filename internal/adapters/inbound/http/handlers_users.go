package http

import (
	"context"
	nethttp "net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
	userservice "github.com/turahe/blog-api/internal/core/user/service"
)

type roleLookup interface {
	ListRoleNames(ctx context.Context, userID uuid.UUID) ([]string, error)
}

type permissionEnforcer interface {
	Enforce(ctx context.Context, userID uuid.UUID, permission string) (bool, error)
}

func requirePermission(enforcer permissionEnforcer, permission string) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := currentUserID(c)
		if !ok {
			failure(c, nethttp.StatusUnauthorized, "unauthorized", "Authentication required")
			return
		}
		if enforcer == nil {
			failure(c, nethttp.StatusForbidden, "forbidden", "Authorization unavailable")
			return
		}
		allowed, err := enforcer.Enforce(c.Request.Context(), userID, permission)
		if err != nil {
			failure(c, nethttp.StatusInternalServerError, "internal_error", "Authorization check failed")
			return
		}
		if !allowed {
			failure(c, nethttp.StatusForbidden, "rbac.forbidden", "Insufficient permissions")
			return
		}
	}
}

func meGetHandler(users *userservice.UserService) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := currentUserID(c)
		if !ok {
			failure(c, nethttp.StatusUnauthorized, "unauthorized", "Authentication required")
			return
		}
		user, err := users.GetByID(c.Request.Context(), userID)
		if err != nil {
			failure(c, nethttp.StatusUnauthorized, "unauthorized", "User not found")
			return
		}
		success(c, nethttp.StatusOK, userJSON(user))
	}
}

func adminUsersListHandler(users *userservice.UserService) gin.HandlerFunc {
	return func(c *gin.Context) {
		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "20"))
		items, total, err := users.List(c.Request.Context(), page, perPage)
		if err != nil {
			failure(c, nethttp.StatusInternalServerError, "internal_error", "Failed to list users")
			return
		}
		out := make([]gin.H, 0, len(items))
		for _, user := range items {
			out = append(out, userJSON(user))
		}
		if page < 1 {
			page = 1
		}
		if perPage < 1 || perPage > 100 {
			perPage = 20
		}
		successWithMeta(c, nethttp.StatusOK, out, &Meta{
			RequestID: requestID(c),
			Page:      page,
			PerPage:   perPage,
			Total:     total,
		})
	}
}

func requireRoles(lookup roleLookup, roles ...string) gin.HandlerFunc {
	allowed := map[string]struct{}{}
	for _, role := range roles {
		allowed[role] = struct{}{}
	}
	return func(c *gin.Context) {
		userID, ok := currentUserID(c)
		if !ok {
			failure(c, nethttp.StatusUnauthorized, "unauthorized", "Authentication required")
			return
		}
		names, err := lookup.ListRoleNames(c.Request.Context(), userID)
		if err != nil {
			failure(c, nethttp.StatusInternalServerError, "internal_error", "Failed to resolve roles")
			return
		}
		for _, name := range names {
			if _, ok := allowed[name]; ok {
				return
			}
		}
		failure(c, nethttp.StatusForbidden, "forbidden", "Insufficient permissions")
	}
}

func userJSON(user userdomain.User) gin.H {
	var emailVerified any
	if user.EmailVerifiedAt != nil {
		emailVerified = user.EmailVerifiedAt.UTC().Format(time.RFC3339)
	}
	return gin.H{
		"id":                user.ID.String(),
		"email":             user.Email,
		"username":          user.Username,
		"full_name":         user.FullName,
		"status":            string(user.Status),
		"email_verified_at": emailVerified,
		"login_count":       user.LoginCount,
		"created_at":        user.CreatedAt.UTC().Format(time.RFC3339),
		"updated_at":        user.UpdatedAt.UTC().Format(time.RFC3339),
	}
}
