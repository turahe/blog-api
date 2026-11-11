package handlers

import (
	"context"
	"errors"
	nethttp "net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	"github.com/turahe/blog-api/internal/adapters/inbound/routes"
	notificationdomain "github.com/turahe/blog-api/internal/core/notification/domain"
	"github.com/turahe/blog-api/internal/shared/pagination"
)

// headerUnreadCount carries the caller's unread total on inbox list responses.
const headerUnreadCount = "X-Unread-Count"

type notificationAPI interface {
	List(ctx context.Context, userID uuid.UUID, unreadOnly bool, pr pagination.PageRequest) (notificationdomain.ListResult, error)
	MarkRead(ctx context.Context, userID, id uuid.UUID) (notificationdomain.Notification, error)
}

var notificationsCfg = pagination.CursorConfig{
	Kind: "notifications_me",
	Sort: []pagination.SortField{
		{Name: "created_at", Column: "n.created_at", Dir: pagination.Desc, Type: pagination.TypeTime},
		{Name: "id", Column: "n.id", Dir: pagination.Desc, Type: pagination.TypeInt64},
	},
	TTL:            pagination.DefaultTTL,
	MaxPerPage:     pagination.DefaultMaxPerPage,
	DefaultPerPage: pagination.DefaultPerPage,
}

func notificationControllers(deps Deps) routes.Notifications {
	if deps.Notifications == nil {
		return routes.Notifications{}
	}

	return routes.Notifications{
		List:   meNotificationsListHandler(deps.Notifications),
		Read:   meNotificationReadHandler(deps.Notifications),
		Stream: meNotificationsStreamHandler(deps.NotificationStream, deps.SSEPingInterval, deps.Impersonation),
	}
}

// meNotificationsListHandler godoc
//
//	@Summary		List my notifications
//	@Description	In-app notices (replies, moderation outcomes, publications), newest first. The X-Unread-Count header holds the unread total across all pages.
//	@Tags			me
//	@Produce		json
//	@Param			unread			query		bool	false	"only unread notifications"
//	@Param			after			query		string	false	"opaque forward cursor"
//	@Param			before			query		string	false	"opaque backward cursor"
//	@Param			limit			query		int		false	"items per page (alias: perPage)"	default(20)
//	@Param			perPage			query		int		false	"items per page"					default(20)
//	@Param			includeTotal	query		bool	false	"include total item count (slow)"	default(false)
//	@Param			page			query		int		false	"page number (legacy offset mode)"	default(1)
//	@Success		200				{object}	responses.Envelope
//	@Header			200				{integer}	X-Unread-Count	"unread notifications"
//	@Failure		400				{object}	responses.Envelope
//	@Failure		401				{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/me/notifications [get]
func meNotificationsListHandler(inbox notificationAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := currentUser(c)
		if !ok {
			return
		}

		unreadOnly := false

		if raw := strings.TrimSpace(c.Query("unread")); raw != "" {
			var err error
			if unreadOnly, err = strconv.ParseBool(raw); err != nil {
				failNotification(c, nethttp.StatusBadRequest, responses.ErrorCodeValidation, "unread must be true or false")
				return
			}
		}

		pr, err := pagination.ParseRequest(c, notificationsCfg)
		if err != nil {
			responses.Failure(c, nethttp.StatusBadRequest, pagination.ErrorCode(err), pagination.ErrorCause(err))
			return
		}

		result, err := inbox.List(c.Request.Context(), userID, unreadOnly, pr)
		if err != nil {
			failNotificationInternal(c, err)
			return
		}

		items := make([]gin.H, 0, len(result.Items))
		for _, item := range result.Items {
			items = append(items, responses.Notification(item))
		}

		c.Header(headerUnreadCount, strconv.FormatInt(notificationdomain.UnreadTotalFrom(result), 10))
		responses.SuccessPaginatedResult[notificationdomain.Notification](c, nethttp.StatusOK, responses.CursorPageOpts[notificationdomain.Notification]{
			Service: responses.ServiceNotifications,
			Result:  result,
			Data:    items,
		})
	}
}

// meNotificationReadHandler godoc
//
//	@Summary		Mark a notification read
//	@Description	Idempotent: a notification that is already read keeps its first readAt.
//	@Tags			me
//	@Produce		json
//	@Param			param1	path		string	true	"notification UUID"
//	@Success		200		{object}	responses.Envelope
//	@Failure		400		{object}	responses.Envelope
//	@Failure		401		{object}	responses.Envelope
//	@Failure		404		{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/me/notifications/{param1}/read [post]
func meNotificationReadHandler(inbox notificationAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := currentUser(c)
		if !ok {
			return
		}

		id, err := uuid.Parse(strings.TrimSpace(c.Param("param1")))
		if err != nil {
			failNotification(c, nethttp.StatusBadRequest, responses.ErrorCodeValidation, "Invalid notification id")
			return
		}

		item, err := inbox.MarkRead(c.Request.Context(), userID, id)
		if errors.Is(err, notificationdomain.ErrNotFound) {
			failNotification(c, nethttp.StatusNotFound, responses.ErrorCodeNotFound, "Notification not found")
			return
		}

		if err != nil {
			failNotificationInternal(c, err)
			return
		}

		responses.SuccessFor(c, nethttp.StatusOK, responses.ServiceNotifications, responses.CaseSuccess, responses.Notification(item))
	}
}

func failNotification(c *gin.Context, status int, code, message string) {
	responses.FailureFor(c, status, responses.FailureOpts{
		Service: responses.ServiceNotifications,
		Code:    code,
		Message: message,
	})
}

func failNotificationInternal(c *gin.Context, err error) {
	responses.RecordError(c, err)
	failNotification(c, nethttp.StatusInternalServerError, responses.ErrorCodeInternal, "An unexpected error occurred")
}
