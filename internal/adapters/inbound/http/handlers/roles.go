package handlers

import (
	"context"
	nethttp "net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/middleware"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/requests"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	rbacdomain "github.com/turahe/blog-api/internal/core/rbac/domain"
	rbacservice "github.com/turahe/blog-api/internal/core/rbac/service"
)

const permRoleRead = "role.read"

// roleAPI is the role administration API consumed by role handlers.
type roleAPI interface {
	List(ctx context.Context) ([]rbacdomain.Role, error)
	Get(ctx context.Context, name string) (rbacdomain.Role, error)
	Create(ctx context.Context, in rbacservice.NewRole) (rbacdomain.Role, error)
	Update(ctx context.Context, name, description string) (rbacdomain.Role, error)
	Delete(ctx context.Context, name string) error
	SetPermissions(ctx context.Context, name string, keys []string) (rbacdomain.Role, error)
	Permissions(ctx context.Context) ([]rbacdomain.Permission, error)
	UserRoles(ctx context.Context, userID uuid.UUID) ([]string, error)
	AssignUserRoles(ctx context.Context, userID uuid.UUID, names []string) ([]string, error)
	RevokeUserRole(ctx context.Context, actor, userID uuid.UUID, name string) ([]string, error)
}

func writeRoleError(c *gin.Context, err error) {
	code, message, status := rbacservice.MapError(err)
	if status >= nethttp.StatusInternalServerError {
		responses.RecordError(c, err)
	}

	responses.FailureFor(c, status, responses.FailureOpts{
		Service: responses.ServiceRBAC,
		Case:    responses.CaseCodeForStatus(status),
		Code:    code,
		Message: message,
		Details: nil,
	})
}

func roleOK(c *gin.Context, status int, data any) {
	responses.SuccessFor(c, status, responses.ServiceRBAC, responses.CaseSuccess, data)
}

func roleParam(c *gin.Context, key string) string {
	return strings.TrimSpace(c.Param(key))
}

func userRolesBody(userID uuid.UUID, names []string) gin.H {
	return gin.H{"userId": userID.String(), "roles": names}
}

// adminRolesListHandler godoc
//
//	@Summary	List roles
//	@Tags		admin
//	@Produce	json
//	@Success	200	{object}	responses.Envelope
//	@Failure	401	{object}	responses.Envelope
//	@Failure	403	{object}	responses.Envelope
//	@Security	Bearer
//	@Router		/api/v1/admin/roles [get]
func adminRolesListHandler(roles roleAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		list, err := roles.List(c.Request.Context())
		if err != nil {
			writeRoleError(c, err)
			return
		}

		out := make([]gin.H, len(list))
		for i, role := range list {
			out[i] = responses.Role(role)
		}

		roleOK(c, nethttp.StatusOK, out)
	}
}

// adminRoleGetHandler godoc
//
//	@Summary	Get a role
//	@Tags		admin
//	@Produce	json
//	@Param		param1	path		string	true	"role name"
//	@Success	200		{object}	responses.Envelope
//	@Failure	404		{object}	responses.Envelope
//	@Security	Bearer
//	@Router		/api/v1/admin/roles/{param1} [get]
func adminRoleGetHandler(roles roleAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		role, err := roles.Get(c.Request.Context(), roleParam(c, "param1"))
		if err != nil {
			writeRoleError(c, err)
			return
		}

		roleOK(c, nethttp.StatusOK, responses.Role(role))
	}
}

// adminRoleCreateHandler godoc
//
//	@Summary		Create a role
//	@Description	Names are 2-60 characters of a-z, 0-9 and '_'. Permissions must be registered keys; "*" is reserved for the admin role.
//	@Tags			admin
//	@Accept			json
//	@Produce		json
//	@Param			body	body		requests.CreateRole	true	"role"
//	@Success		201		{object}	responses.Envelope
//	@Failure		400		{object}	responses.Envelope
//	@Failure		409		{object}	responses.Envelope
//	@Failure		422		{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/admin/roles [post]
func adminRoleCreateHandler(roles roleAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req requests.CreateRole
		if !requests.BindJSON(c, &req) {
			return
		}

		role, err := roles.Create(c.Request.Context(), rbacservice.NewRole{
			Name: req.Name, Description: req.Description, Permissions: req.Permissions,
		})
		if err != nil {
			writeRoleError(c, err)
			return
		}

		roleOK(c, nethttp.StatusCreated, responses.Role(role))
	}
}

// adminRoleUpdateHandler godoc
//
//	@Summary	Update a role's description
//	@Tags		admin
//	@Accept		json
//	@Produce	json
//	@Param		param1	path		string				true	"role name"
//	@Param		body	body		requests.UpdateRole	true	"fields"
//	@Success	200		{object}	responses.Envelope
//	@Failure	400		{object}	responses.Envelope
//	@Failure	404		{object}	responses.Envelope
//	@Security	Bearer
//	@Router		/api/v1/admin/roles/{param1} [patch]
func adminRoleUpdateHandler(roles roleAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req requests.UpdateRole
		if !requests.BindJSON(c, &req) {
			return
		}

		role, err := roles.Update(c.Request.Context(), roleParam(c, "param1"), req.Description)
		if err != nil {
			writeRoleError(c, err)
			return
		}

		roleOK(c, nethttp.StatusOK, responses.Role(role))
	}
}

// adminRoleDeleteHandler godoc
//
//	@Summary		Delete a role
//	@Description	Removes the role, its grants, and its user assignments. The admin role cannot be deleted.
//	@Tags			admin
//	@Param			param1	path	string	true	"role name"
//	@Success		204
//	@Failure		403	{object}	responses.Envelope
//	@Failure		404	{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/admin/roles/{param1} [delete]
func adminRoleDeleteHandler(roles roleAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := roles.Delete(c.Request.Context(), roleParam(c, "param1")); err != nil {
			writeRoleError(c, err)
			return
		}

		c.AbortWithStatus(nethttp.StatusNoContent)
	}
}

// adminRolePermissionsSetHandler godoc
//
//	@Summary		Replace a role's permissions
//	@Description	Overwrites the role's permission set; an empty list clears it. The admin role's set is fixed.
//	@Tags			admin
//	@Accept			json
//	@Produce		json
//	@Param			param1	path		string						true	"role name"
//	@Param			body	body		requests.SetRolePermissions	true	"permission keys"
//	@Success		200		{object}	responses.Envelope
//	@Failure		400		{object}	responses.Envelope
//	@Failure		403		{object}	responses.Envelope
//	@Failure		404		{object}	responses.Envelope
//	@Failure		422		{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/admin/roles/{param1}/permissions [put]
func adminRolePermissionsSetHandler(roles roleAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req requests.SetRolePermissions
		if !requests.BindJSON(c, &req) {
			return
		}

		role, err := roles.SetPermissions(c.Request.Context(), roleParam(c, "param1"), req.Permissions)
		if err != nil {
			writeRoleError(c, err)
			return
		}

		roleOK(c, nethttp.StatusOK, responses.Role(role))
	}
}

// adminPermissionsListHandler godoc
//
//	@Summary	List registered permissions
//	@Tags		admin
//	@Produce	json
//	@Success	200	{object}	responses.Envelope
//	@Security	Bearer
//	@Router		/api/v1/admin/permissions [get]
func adminPermissionsListHandler(roles roleAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		list, err := roles.Permissions(c.Request.Context())
		if err != nil {
			writeRoleError(c, err)
			return
		}

		out := make([]gin.H, len(list))
		for i, p := range list {
			out[i] = responses.Permission(p)
		}

		roleOK(c, nethttp.StatusOK, out)
	}
}

// adminUserRolesListHandler godoc
//
//	@Summary	List a user's roles
//	@Tags		admin
//	@Produce	json
//	@Param		param1	path		string	true	"user UUID"
//	@Success	200		{object}	responses.Envelope
//	@Failure	404		{object}	responses.Envelope
//	@Security	Bearer
//	@Router		/api/v1/admin/users/{param1}/roles [get]
func adminUserRolesListHandler(roles roleAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		target, ok := userParam(c)
		if !ok {
			return
		}

		names, err := roles.UserRoles(c.Request.Context(), target)
		if err != nil {
			writeRoleError(c, err)
			return
		}

		roleOK(c, nethttp.StatusOK, userRolesBody(target, names))
	}
}

// adminUserRolesAssignHandler godoc
//
//	@Summary	Assign roles to a user
//	@Tags		admin
//	@Accept		json
//	@Produce	json
//	@Param		param1	path		string						true	"user UUID"
//	@Param		body	body		requests.AssignUserRoles	true	"role names"
//	@Success	200		{object}	responses.Envelope
//	@Failure	400		{object}	responses.Envelope
//	@Failure	404		{object}	responses.Envelope
//	@Security	Bearer
//	@Router		/api/v1/admin/users/{param1}/roles [post]
func adminUserRolesAssignHandler(roles roleAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		target, ok := userParam(c)
		if !ok {
			return
		}

		var req requests.AssignUserRoles
		if !requests.BindJSON(c, &req) {
			return
		}

		names, err := roles.AssignUserRoles(c.Request.Context(), target, req.Roles)
		if err != nil {
			writeRoleError(c, err)
			return
		}

		roleOK(c, nethttp.StatusOK, userRolesBody(target, names))
	}
}

// adminUserRoleRevokeHandler godoc
//
//	@Summary		Revoke a role from a user
//	@Description	Administrators cannot revoke the admin role from themselves.
//	@Tags			admin
//	@Produce		json
//	@Param			param1	path		string	true	"user UUID"
//	@Param			param2	path		string	true	"role name"
//	@Success		200		{object}	responses.Envelope
//	@Failure		403		{object}	responses.Envelope
//	@Failure		404		{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/admin/users/{param1}/roles/{param2} [delete]
func adminUserRoleRevokeHandler(roles roleAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		target, ok := userParam(c)
		if !ok {
			return
		}

		actor, ok := middleware.CurrentUserID(c)
		if !ok {
			responses.Failure(c, nethttp.StatusUnauthorized, responses.ErrorCodeUnauthorized, "Authentication required")
			return
		}

		names, err := roles.RevokeUserRole(c.Request.Context(), actor, target, roleParam(c, "param2"))
		if err != nil {
			writeRoleError(c, err)
			return
		}

		roleOK(c, nethttp.StatusOK, userRolesBody(target, names))
	}
}
