package handlers

import (
	"context"
	"errors"
	nethttp "net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/middleware"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/requests"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	settingsdomain "github.com/turahe/blog-api/internal/core/settings/domain"
	settingsservice "github.com/turahe/blog-api/internal/core/settings/service"
)

const errorCodeSettingsVersionConflict = "settings.version_conflict"

type settingsAPI interface {
	List(ctx context.Context, filter settingsservice.ListFilter) ([]settingsdomain.Setting, error)
	Update(ctx context.Context, actor settingsservice.Actor, updates []settingsservice.Update) (settingsservice.Result, error)
	History(ctx context.Context, filter settingsdomain.HistoryFilter) (settingsdomain.HistoryPage, error)
}

// adminGetSettingsHandler godoc
//
//	@Summary		Get settings
//	@Description	Returns the settings catalogue with current values. server_only keys are never returned; admin_only keys need include_sensitive_admin=true and settings.update.
//	@Tags			admin
//	@Produce		json
//	@Param			category				query		string	false	"site, content, media, analytics, notifications, seo, or security"
//	@Param			include_sensitive_admin	query		bool	false	"include admin_only keys"
//	@Success		200						{object}	responses.Envelope
//	@Failure		400						{object}	responses.Envelope
//	@Failure		403						{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/admin/settings [get]
func adminGetSettingsHandler(settings settingsAPI, canSeeAdminOnly func(*gin.Context) bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		filter := settingsservice.ListFilter{Category: settingsdomain.Category(c.Query("category"))}

		if raw := c.Query("include_sensitive_admin"); raw != "" {
			include, err := strconv.ParseBool(raw)
			if err != nil {
				failSettings(c, nethttp.StatusBadRequest, responses.ErrorCodeValidation, "include_sensitive_admin must be a boolean", nil)
				return
			}

			filter.IncludeAdminOnly = include && canSeeAdminOnly(c)
		}

		items, err := settings.List(c.Request.Context(), filter)
		if mapSettingsError(c, err) {
			return
		}

		out := make([]gin.H, 0, len(items))
		defaulted := []string{}

		for _, item := range items {
			out = append(out, responses.Setting(item))
			if item.Defaulted {
				defaulted = append(defaulted, item.Key)
			}
		}

		responses.SuccessFor(c, nethttp.StatusOK, responses.ServiceSettings, responses.CaseSuccess,
			gin.H{"settings": out, "default_applied": defaulted})
	}
}

// adminUpdateSettingsHandler godoc
//
//	@Summary		Update settings
//	@Description	Validates every key, then applies the changed ones atomically. Any invalid key rejects the whole request with 422; a stale per-key version returns 409.
//	@Tags			admin
//	@Accept			json
//	@Produce		json
//	@Param			body	body		requests.UpdateSettings	true	"updates"
//	@Success		200		{object}	responses.Envelope
//	@Failure		400		{object}	responses.Envelope
//	@Failure		403		{object}	responses.Envelope
//	@Failure		409		{object}	responses.Envelope
//	@Failure		422		{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/admin/settings [put]
func adminUpdateSettingsHandler(settings settingsAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req requests.UpdateSettings
		if !requests.BindJSON(c, &req) {
			return
		}

		updates := make([]settingsservice.Update, 0, len(req.Updates))
		for _, u := range req.Updates {
			updates = append(updates, settingsservice.Update{Key: u.Key, Value: u.Value, Version: u.Version})
		}

		actor := settingsservice.Actor{RequestID: responses.RequestID(c)}
		if id, ok := middleware.CurrentUserID(c); ok {
			actor.UserID = &id
		}

		result, err := settings.Update(c.Request.Context(), actor, updates)
		if mapSettingsError(c, err) {
			return
		}

		applied := make([]gin.H, 0, len(result.Applied))
		for _, change := range result.Applied {
			applied = append(applied, responses.SettingChange(change))
		}

		responses.SuccessFor(c, nethttp.StatusOK, responses.ServiceSettings, responses.CaseSuccess,
			gin.H{"applied": applied, "unchanged": result.Unchanged})
	}
}

// adminSettingsHistoryHandler godoc
//
//	@Summary	List settings history
//	@Tags		admin
//	@Produce	json
//	@Param		key			query		string	false	"only changes of this key"
//	@Param		page		query		int		false	"page (default 1)"
//	@Param		per_page	query		int		false	"page size (default 20, max 100)"
//	@Success	200			{object}	responses.Envelope
//	@Failure	403			{object}	responses.Envelope
//	@Security	Bearer
//	@Router		/api/v1/admin/settings/history [get]
func adminSettingsHistoryHandler(settings settingsAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		filter := settingsdomain.HistoryFilter{Key: c.Query("key")}
		filter.Page, filter.PerPage = pageParams(c)

		page, err := settings.History(c.Request.Context(), filter)
		if mapSettingsError(c, err) {
			return
		}

		items := make([]gin.H, 0, len(page.Items))
		for _, entry := range page.Items {
			items = append(items, responses.SettingHistory(entry))
		}

		responses.SuccessPaginatedFor(c, nethttp.StatusOK, responses.PageOpts{
			Service: responses.ServiceSettings,
			Data:    items,
			Page:    page.Page,
			PerPage: page.PerPage,
			Total:   page.Total,
		})
	}
}

func mapSettingsError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}

	var invalid *settingsdomain.ValidationError

	switch {
	case errors.As(err, &invalid):
		failSettings(c, nethttp.StatusUnprocessableEntity, responses.ErrorCodeValidation,
			"One or more settings are invalid", gin.H{"violations": invalid.Violations})
	case errors.Is(err, settingsservice.ErrValidation):
		failSettings(c, nethttp.StatusBadRequest, responses.ErrorCodeValidation, err.Error(), nil)
	case errors.Is(err, settingsdomain.ErrVersionConflict):
		failSettings(c, nethttp.StatusConflict, errorCodeSettingsVersionConflict,
			"A setting changed since it was read; reload and retry", nil)
	default:
		responses.RecordError(c, err)
		failSettings(c, nethttp.StatusInternalServerError, responses.ErrorCodeInternal, "An unexpected error occurred", nil)
	}

	return true
}

func failSettings(c *gin.Context, status int, code, message string, details any) {
	responses.FailureFor(c, status, responses.FailureOpts{
		Service: responses.ServiceSettings,
		Code:    code,
		Message: message,
		Details: details,
	})
}
