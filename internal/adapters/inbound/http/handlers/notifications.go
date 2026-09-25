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
)

// headerUnreadCount carries the caller's unread total on inbox list responses.
const headerUnreadCount = "X-Unread-Count"

type notificationAPI interface {
	List(ctx context.Context, userID uuid.UUID, unreadOnly bool, page, perPage int) (notificationdomain.ListResult, error)
	MarkRead(ctx context.Context, userID, id uuid.UUID) (notificationdomain.Notification, error)
}

func notificationControllers(deps Deps) routes.Notifications {
	if deps.Notifications == nil {
		return routes.Notifications{}
	}

	return routes.Notifications{
		List:   meNotificationsListHandler(deps.Notifications),
		Read:   meNotificationReadHandler(deps.Notifications),
		Stream: meNotificationsStreamHandler(deps.NotificationStream, deps.SSEPingInterval),
	}
}

// meNotificationsListHandler godoc
//
//	@Summary		List my notifications
//	@Description	In-app notices (replies, moderation outcomes, publications), newest first. The X-Unread-Count header holds the unread total across all pages.
//	@Tags			me
//	@Produce		json
//	@Param			unread		query		bool	false	"only unread notifications"
//	@Param			page		query		int		false	"page"		default(1)
//	@Param			per_page	query		int		false	"per page"	default(20)
//	@Success		200			{object}	responses.Envelope
//	@Header			200			{integer}	X-Unread-Count	"unread notifications"
//	@Failure		400			{object}	responses.Envelope
//	@Failure		401			{object}	responses.Envelope
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

		page, perPage := pageParams(c)

		result, err := inbox.List(c.Request.Context(), userID, unreadOnly, page, perPage)
		if err != nil {
			failNotificationInternal(c, err)
			return
		}

		items := make([]gin.H, 0, len(result.Items))
		for _, item := range result.Items {
			items = append(items, responses.Notification(item))
		}

		c.Header(headerUnreadCount, strconv.FormatInt(result.Unread, 10))
		responses.SuccessPaginatedFor(c, nethttp.StatusOK, responses.PageOpts{
			Service: responses.ServiceNotifications,
			Data:    items,
			Page:    result.Page,
			PerPage: result.PerPage,
			Total:   result.Total,
		})
	}
}

// meNotificationReadHandler godoc
//
//	@Summary		Mark a notification read
//	@Description	Idempotent: a notification that is already read keeps its first read_at.
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
