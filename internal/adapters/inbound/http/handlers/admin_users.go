package handlers

import (
	"context"
	"errors"
	"io"
	nethttp "net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/requests"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
	authservice "github.com/turahe/blog-api/internal/core/auth/service"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
)

const permRoleManage = "role.manage"

// adminUserAPI is the administrator account API consumed by admin user handlers.
type adminUserAPI interface {
	AdminCreateUser(ctx context.Context, in authdomain.NewUser) (userdomain.User, error)
	AdminResetPassword(ctx context.Context, userID uuid.UUID, revokeSessions bool) (authdomain.AdminReset, error)
}

func writeAdminUserError(c *gin.Context, err error) {
	code, message, status := authservice.MapError(err)
	if status >= nethttp.StatusInternalServerError {
		responses.RecordError(c, err)
	}

	responses.FailureFor(c, status, responses.FailureOpts{
		Service: responses.ServiceUsers,
		Case:    responses.CaseCodeForStatus(status),
		Code:    code,
		Message: message,
		Details: nil,
	})
}

// adminCreateUserHandler godoc
//
//	@Summary		Create a user
//	@Description	Creates an active account. Assigning roles also requires role.manage.
//	@Tags			admin
//	@Accept			json
//	@Produce		json
//	@Param			body	body		requests.AdminCreateUser	true	"new account"
//	@Success		201		{object}	responses.Envelope
//	@Failure		400		{object}	responses.Envelope
//	@Failure		403		{object}	responses.Envelope
//	@Failure		409		{object}	responses.Envelope
//	@Failure		422		{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/admin/users [post]
func adminCreateUserHandler(admin adminUserAPI, canManageRoles func(*gin.Context) bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req requests.AdminCreateUser
		if !requests.BindJSON(c, &req) {
			return
		}

		if len(req.Roles) > 0 && !canManageRoles(c) {
			responses.Failure(c, nethttp.StatusForbidden, "rbac.forbidden", "Assigning roles requires role.manage")
			return
		}

		user, err := admin.AdminCreateUser(c.Request.Context(), authdomain.NewUser{
			Email: req.Email, Username: req.Username, FullName: req.FullName,
			Password: req.Password, Roles: req.Roles,
		})
		if err != nil {
			writeAdminUserError(c, err)
			return
		}

		responses.SuccessFor(c, nethttp.StatusCreated, responses.ServiceUsers, responses.CaseSuccess, responses.User(user))
	}
}

// adminResetPasswordHandler godoc
//
//	@Summary		Reset a user's password
//	@Description	Emails the user a new reset link, invalidating earlier links. Sessions are revoked unless revoke_sessions is false.
//	@Tags			admin
//	@Accept			json
//	@Produce		json
//	@Param			param1	path		string						true	"user UUID"
//	@Param			body	body		requests.AdminResetPassword	false	"options"
//	@Success		202		{object}	responses.Envelope
//	@Failure		400		{object}	responses.Envelope
//	@Failure		401		{object}	responses.Envelope
//	@Failure		404		{object}	responses.Envelope
//	@Failure		409		{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/admin/users/{param1}/password/admin-reset [post]
func adminResetPasswordHandler(admin adminUserAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		target, ok := userParam(c)
		if !ok {
			return
		}

		var req requests.AdminResetPassword
		if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
			requests.FailValidation(c, err)
			return
		}

		revoke := true
		if req.RevokeSessions != nil {
			revoke = *req.RevokeSessions
		}

		result, err := admin.AdminResetPassword(c.Request.Context(), target, revoke)
		if err != nil {
			writeAdminUserError(c, err)
			return
		}

		responses.SuccessFor(c, nethttp.StatusAccepted, responses.ServiceUsers, responses.CaseAccepted, gin.H{
			"reset_link_expires_at": result.ExpiresAt.UTC().Format(time.RFC3339),
			"sessions_revoked":      result.SessionsRevoked,
		})
	}
}
