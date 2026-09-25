package handlers

import (
	"context"
	"errors"
	nethttp "net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	auditdomain "github.com/turahe/blog-api/internal/core/audit/domain"
	auditservice "github.com/turahe/blog-api/internal/core/audit/service"
)

const (
	permActivityReadAll = "user.activity.read_all"
	dateLayout          = time.DateOnly
)

type activityAPI interface {
	ForOwner(ctx context.Context, filter auditdomain.ActivityFilter) (auditdomain.ActivityPage, error)
	ForAdmin(ctx context.Context, filter auditdomain.ActivityFilter) (auditdomain.ActivityPage, error)
}

func activityControllers(deps Deps) (me, admin gin.HandlerFunc) {
	if deps.Activity == nil {
		return nil, nil
	}

	return meActivityHandler(deps.Activity),
		gate(deps, permActivityReadAll, adminRoles, adminUserActivityHandler(deps.Activity))
}

// meActivityHandler godoc
//
//	@Summary		List my account activity
//	@Description	Sign-ins, security changes, and content actions on or by the caller's account, newest first. IP addresses are reduced to their network and user agents to "browser on OS".
//	@Tags			me
//	@Produce		json
//	@Param			category	query		string	false	"comma-separated categories, for example login,password_change"
//	@Param			from		query		string	false	"RFC 3339 timestamp or YYYY-MM-DD"
//	@Param			to			query		string	false	"RFC 3339 timestamp or YYYY-MM-DD (inclusive)"
//	@Param			page		query		int		false	"page"		default(1)
//	@Param			perPage		query		int		false	"per page"	default(20)
//	@Success		200			{object}	responses.Envelope
//	@Failure		400			{object}	responses.Envelope
//	@Failure		401			{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/me/activity [get]
func meActivityHandler(activity activityAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := currentUser(c)
		if !ok {
			return
		}

		filter, ok := activityFilter(c)
		if !ok {
			return
		}

		filter.UserID = userID

		page, err := activity.ForOwner(c.Request.Context(), filter)
		writeActivityPage(c, page, err, responses.OwnerActivity)
	}
}

// adminUserActivityHandler godoc
//
//	@Summary		List a user's activity
//	@Description	Every audit entry the user performed or that targets the user's account, with IP, user agent, request id, and before/after changes. Requires user.activity.read_all.
//	@Tags			admin
//	@Produce		json
//	@Param			param1		path		string	true	"user UUID"
//	@Param			category	query		string	false	"comma-separated categories"
//	@Param			from		query		string	false	"RFC 3339 timestamp or YYYY-MM-DD"
//	@Param			to			query		string	false	"RFC 3339 timestamp or YYYY-MM-DD (inclusive)"
//	@Param			page		query		int		false	"page"		default(1)
//	@Param			perPage		query		int		false	"per page"	default(20)
//	@Success		200			{object}	responses.Envelope
//	@Failure		400			{object}	responses.Envelope
//	@Failure		401			{object}	responses.Envelope
//	@Failure		403			{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/admin/users/{param1}/activity [get]
func adminUserActivityHandler(activity activityAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		target, ok := userParam(c)
		if !ok {
			return
		}

		filter, ok := activityFilter(c)
		if !ok {
			return
		}

		filter.UserID = target

		page, err := activity.ForAdmin(c.Request.Context(), filter)
		writeActivityPage(c, page, err, responses.AdminActivity)
	}
}

func activityFilter(c *gin.Context) (auditdomain.ActivityFilter, bool) {
	var filter auditdomain.ActivityFilter

	for _, raw := range c.QueryArray("category") {
		for category := range strings.SplitSeq(raw, ",") {
			if category = strings.TrimSpace(category); category != "" {
				filter.Categories = append(filter.Categories, category)
			}
		}
	}

	var err error

	if filter.From, err = activityTime(c.Query("from"), false); err != nil {
		failActivityValidation(c, "from must be an RFC 3339 timestamp or YYYY-MM-DD")
		return filter, false
	}

	if filter.To, err = activityTime(c.Query("to"), true); err != nil {
		failActivityValidation(c, "to must be an RFC 3339 timestamp or YYYY-MM-DD")
		return filter, false
	}

	filter.Page, filter.PerPage = pageParams(c)

	return filter, true
}

// activityTime parses a timestamp or a date; a date bound for "to" covers the whole day.
func activityTime(raw string, endOfDay bool) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return &t, nil
	}

	t, err := time.Parse(dateLayout, raw)
	if err != nil {
		return nil, err
	}

	if endOfDay {
		t = t.Add(24*time.Hour - time.Nanosecond)
	}

	return &t, nil
}

func writeActivityPage(c *gin.Context, page auditdomain.ActivityPage, err error, view func(auditdomain.Entry) gin.H) {
	if errors.Is(err, auditservice.ErrInvalidRange) {
		failActivityValidation(c, "from must not be after to")
		return
	}

	if err != nil {
		responses.RecordError(c, err)
		responses.FailureFor(c, nethttp.StatusInternalServerError, responses.FailureOpts{
			Service: responses.ServiceUsers,
			Code:    responses.ErrorCodeInternal,
			Message: "An unexpected error occurred",
		})

		return
	}

	items := make([]gin.H, 0, len(page.Items))
	for _, entry := range page.Items {
		items = append(items, view(entry))
	}

	responses.SuccessPaginatedFor(c, nethttp.StatusOK, responses.PageOpts{
		Service: responses.ServiceUsers,
		Data:    items,
		Page:    page.Page,
		PerPage: page.PerPage,
		Total:   page.Total,
	})
}

func failActivityValidation(c *gin.Context, message string) {
	responses.FailureFor(c, nethttp.StatusBadRequest, responses.FailureOpts{
		Service: responses.ServiceUsers,
		Code:    responses.ErrorCodeValidation,
		Message: message,
	})
}
