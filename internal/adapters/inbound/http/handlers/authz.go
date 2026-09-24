package handlers

import (
	"context"
	nethttp "net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/middleware"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
)

// RoleLookup resolves role names for a user (admin/editor gates).
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
			responses.Failure(c, nethttp.StatusUnauthorized, responses.ErrorCodeUnauthorized, "Authentication required")
			return
		}

		if enforcer == nil {
			responses.Failure(c, nethttp.StatusForbidden, responses.ErrorCodeForbidden, "Authorization unavailable")
			return
		}

		allowed, err := enforcer.Enforce(c.Request.Context(), userID, permission)
		if err != nil {
			responses.Failure(c, nethttp.StatusInternalServerError, responses.ErrorCodeInternal, "Authorization check failed")
			return
		}

		if !allowed {
			responses.Failure(c, nethttp.StatusForbidden, "rbac.forbidden", "Insufficient permissions")
			return
		}
	}
}

func requireRoles(lookup RoleLookup, roles ...string) gin.HandlerFunc {
	allowed := make(map[string]struct{}, len(roles))
	for _, role := range roles {
		allowed[role] = struct{}{}
	}

	return func(c *gin.Context) {
		userID, ok := middleware.CurrentUserID(c)
		if !ok {
			responses.Failure(c, nethttp.StatusUnauthorized, responses.ErrorCodeUnauthorized, "Authentication required")
			return
		}

		names, err := lookup.ListRoleNames(c.Request.Context(), userID)
		if err != nil {
			responses.Failure(c, nethttp.StatusInternalServerError, responses.ErrorCodeInternal, "Failed to resolve roles")
			return
		}

		for _, name := range names {
			if _, ok := allowed[name]; ok {
				return
			}
		}

		responses.Failure(c, nethttp.StatusForbidden, responses.ErrorCodeForbidden, "Insufficient permissions")
	}
}
