package handlers

import (
	"errors"
	nethttp "net/http"

	"github.com/gin-gonic/gin"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/middleware"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
	userservice "github.com/turahe/blog-api/internal/core/user/service"
	"github.com/turahe/blog-api/internal/shared/pagination"
)

var usersAdminCfg = pagination.CursorConfig{
	Kind: "users_admin",
	Sort: []pagination.SortField{
		{Name: "created_at", Dir: pagination.Desc, Type: pagination.TypeTime},
		{Name: "id", Dir: pagination.Desc, Type: pagination.TypeInt64},
	},
	TTL:            pagination.DefaultTTL,
	MaxPerPage:     pagination.DefaultMaxPerPage,
	DefaultPerPage: pagination.DefaultPerPage,
}

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
//	@Summary		List users
//	@Description	Supports both legacy offset pagination (page/perPage) and cursor-based keyset pagination (after/before/limit).
//	@Tags			admin
//	@Produce		json
//	@Param			page			query		int		false	"page (legacy offset mode)"			default(1)
//	@Param			perPage			query		int		false	"per page (legacy offset mode, alias limit)"	default(20)
//	@Param			limit			query		int		false	"page size (cursor or offset)"		default(20)
//	@Param			after			query		string	false	"opaque cursor: return items after this point"
//	@Param			before			query		string	false	"opaque cursor: return items before this point"
//	@Param			includeTotal	query		bool	false	"when false, skip COUNT(*) to reduce DB load"	default(true)
//	@Success		200				{object}	responses.Envelope
//	@Failure		401				{object}	responses.Envelope
//	@Failure		403				{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/admin/users [get]
func adminUsersListHandler(users *userservice.UserService) gin.HandlerFunc {
	return func(c *gin.Context) {
		pr, err := pagination.ParseRequest(c, usersAdminCfg)
		if err != nil {
			responses.Failure(c, nethttp.StatusBadRequest, pagination.ErrorCode(err), pagination.ErrorCause(err))
			return
		}

		filter := userdomain.ListFilter{PageRequest: pr}
		result, err := users.List(c.Request.Context(), filter)
		if err != nil {
			switch {
			case errors.Is(err, pagination.ErrCursorMalformed),
				errors.Is(err, pagination.ErrCursorInvalidSignature),
				errors.Is(err, pagination.ErrCursorExpired),
				errors.Is(err, pagination.ErrCursorWrongKind),
				errors.Is(err, pagination.ErrCursorMissingField),
				errors.Is(err, pagination.ErrCursorFieldType),
				errors.Is(err, pagination.ErrCursorUnsupported):
				responses.Failure(c, nethttp.StatusBadRequest, pagination.ErrorCode(err), pagination.ErrorCause(err))
			default:
				responses.Internal(c, err, "Failed to list users")
			}
			return
		}

		out := make([]gin.H, 0, len(result.Items))
		for _, user := range result.Items {
			out = append(out, responses.User(user))
		}

		responses.SuccessPaginatedResult[userdomain.User](c, nethttp.StatusOK, responses.CursorPageOpts[userdomain.User]{
			Service: responses.ServiceUsers,
			Result:  result,
			Data:    out,
		})
	}
}
