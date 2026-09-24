package handlers

import (
	"errors"
	nethttp "net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/middleware"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
	userservice "github.com/turahe/blog-api/internal/core/user/service"
)

// meGetHandler godoc
//
//	@Summary	Current user
//	@Tags		self-service
//	@Produce	json
//	@Success	200	{object}	responses.Envelope
//	@Failure	401	{object}	responses.Envelope
//	@Security	Bearer
//	@Router		/api/v1/me [get]
func meGetHandler(users *userservice.UserService) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := middleware.CurrentUserID(c)
		if !ok {
			responses.Failure(c, nethttp.StatusUnauthorized, responses.ErrorCodeUnauthorized, "Authentication required")
			return
		}

		user, err := users.GetByID(c.Request.Context(), userID)
		if errors.Is(err, userdomain.ErrNotFound) {
			responses.Failure(c, nethttp.StatusUnauthorized, responses.ErrorCodeUnauthorized, "User not found")
			return
		}

		if err != nil {
			responses.Internal(c, err, "Failed to load user")
			return
		}

		responses.SuccessFor(c, nethttp.StatusOK, responses.ServiceUsers, responses.CaseSuccess, responses.User(user))
	}
}

// adminUsersListHandler godoc
//
//	@Summary	List users
//	@Tags		admin
//	@Produce	json
//	@Param		page		query		int	false	"page"		default(1)
//	@Param		per_page	query		int	false	"per page"	default(20)
//	@Success	200			{object}	responses.Envelope
//	@Failure	401			{object}	responses.Envelope
//	@Failure	403			{object}	responses.Envelope
//	@Security	Bearer
//	@Router		/api/v1/admin/users [get]
func adminUsersListHandler(users *userservice.UserService) gin.HandlerFunc {
	return func(c *gin.Context) {
		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "20"))

		items, total, err := users.List(c.Request.Context(), page, perPage)
		if err != nil {
			responses.Internal(c, err, "Failed to list users")
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

		responses.SuccessPaginatedFor(c, nethttp.StatusOK, responses.PageOpts{
			Service: responses.ServiceUsers,
			Data:    out,
			Page:    page,
			PerPage: perPage,
			Total:   total,
		})
	}
}
