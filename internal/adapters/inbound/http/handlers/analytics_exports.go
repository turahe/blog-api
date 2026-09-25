package handlers

import (
	"context"
	"errors"
	nethttp "net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/requests"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	"github.com/turahe/blog-api/internal/adapters/inbound/routes"
	analyticsdomain "github.com/turahe/blog-api/internal/core/analytics/domain"
	analyticsservice "github.com/turahe/blog-api/internal/core/analytics/service"
)

type analyticsExportsAPI interface {
	Request(ctx context.Context, actor uuid.UUID, in analyticsservice.ExportRequest) (analyticsservice.ExportView, error)
	Get(ctx context.Context, actor, id uuid.UUID) (analyticsservice.ExportView, error)
	List(ctx context.Context, actor uuid.UUID) ([]analyticsservice.ExportView, error)
}

// adminAnalyticsExportHandler godoc
//
//	@Summary		Request an analytics export
//	@Description	Queues a ZIP of CSV files with every rollup (site, pages, referrers, audience, navigation, searches, search positions, clicked results) of whole periods covering from through to, plus the daily retention cohorts. No raw events, visitor hashes, or session ids are exported. Requires the current password, and two_factor_code when two-factor is enabled. The archive is built in the background: poll GET /api/v1/admin/analytics/exports/{param1} for a presigned download link. One export per user may be open; a second request answers 409 with the open one in error.details.
//	@Tags			admin
//	@Accept			json
//	@Produce		json
//	@Param			body	body		requests.AnalyticsExport	true	"window and step-up credentials"
//	@Success		202		{object}	responses.Envelope
//	@Failure		400		{object}	responses.Envelope
//	@Failure		401		{object}	responses.Envelope
//	@Failure		403		{object}	responses.Envelope
//	@Failure		409		{object}	responses.Envelope
//	@Failure		503		{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/admin/analytics/export [post]
func adminAnalyticsExportHandler(exports analyticsExportsAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		actor, ok := currentUser(c)
		if !ok {
			return
		}

		var req requests.AnalyticsExport
		if err := c.ShouldBindJSON(&req); err != nil {
			requests.FailValidation(c, err)
			return
		}

		view, err := exports.Request(c.Request.Context(), actor, req.Input())
		if errors.Is(err, analyticsdomain.ErrExportOpen) {
			responses.FailureFor(c, nethttp.StatusConflict, responses.FailureOpts{
				Service: responses.ServiceAnalytics, Case: responses.CaseConflict, Code: "analytics.export_in_progress",
				Message: "An export is already in progress", Details: responses.AnalyticsExport(view, time.Now()),
			})

			return
		}

		if writeAnalyticsExportError(c, err) {
			return
		}

		c.Header("Cache-Control", "no-store")
		responses.SuccessFor(c, nethttp.StatusAccepted, responses.ServiceAnalytics, responses.CaseSuccess,
			responses.AnalyticsExport(view, time.Now()))
	}
}

// adminAnalyticsExportsListHandler godoc
//
//	@Summary		List my analytics exports
//	@Description	The caller's 20 most recent exports, newest first. Completed exports whose archive still exists carry a fresh presigned download_url valid for ANALYTICS_EXPORT_URL_TTL (never past archive_expires_at).
//	@Tags			admin
//	@Produce		json
//	@Success		200	{object}	responses.Envelope
//	@Failure		401	{object}	responses.Envelope
//	@Failure		403	{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/admin/analytics/exports [get]
func adminAnalyticsExportsListHandler(exports analyticsExportsAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		actor, ok := currentUser(c)
		if !ok {
			return
		}

		views, err := exports.List(c.Request.Context(), actor)
		if writeAnalyticsExportError(c, err) {
			return
		}

		c.Header("Cache-Control", "no-store")
		responses.SuccessFor(c, nethttp.StatusOK, responses.ServiceAnalytics, responses.CaseSuccess,
			responses.AnalyticsExports(views, time.Now()))
	}
}

// adminAnalyticsExportGetHandler godoc
//
//	@Summary		Get an analytics export
//	@Description	Status of one of the caller's exports; other users' exports answer 404. A completed export whose archive still exists carries a fresh presigned download_url; after ANALYTICS_EXPORT_RETENTION the archive is deleted and status is expired.
//	@Tags			admin
//	@Produce		json
//	@Param			param1	path		string	true	"export id"
//	@Success		200		{object}	responses.Envelope
//	@Failure		400		{object}	responses.Envelope
//	@Failure		401		{object}	responses.Envelope
//	@Failure		403		{object}	responses.Envelope
//	@Failure		404		{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/admin/analytics/exports/{param1} [get]
func adminAnalyticsExportGetHandler(exports analyticsExportsAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		actor, ok := currentUser(c)
		if !ok {
			return
		}

		id, err := uuid.Parse(strings.TrimSpace(c.Param("param1")))
		if err != nil {
			responses.Failure(c, nethttp.StatusBadRequest, responses.ErrorCodeValidation, "Invalid export id")
			return
		}

		view, err := exports.Get(c.Request.Context(), actor, id)
		if writeAnalyticsExportError(c, err) {
			return
		}

		c.Header("Cache-Control", "no-store")
		responses.SuccessFor(c, nethttp.StatusOK, responses.ServiceAnalytics, responses.CaseSuccess,
			responses.AnalyticsExport(view, time.Now()))
	}
}

func writeAnalyticsExportError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}

	opts := responses.FailureOpts{Service: responses.ServiceAnalytics}

	var status int

	switch {
	case errors.Is(err, analyticsdomain.ErrValidation):
		status, opts.Case, opts.Code = nethttp.StatusBadRequest, responses.CaseValidation, responses.ErrorCodeValidation
		opts.Message = err.Error()
	case errors.Is(err, analyticsdomain.ErrStepUpRequired):
		status, opts.Case, opts.Code = nethttp.StatusForbidden, responses.CaseForbidden, "analytics.step_up_required"
		opts.Message = "Confirm with your current password and, when two-factor is enabled, a code"
	case errors.Is(err, analyticsdomain.ErrExportNotFound):
		status, opts.Case, opts.Code = nethttp.StatusNotFound, responses.CaseNotFound, responses.ErrorCodeNotFound
		opts.Message = "Export not found"
	case errors.Is(err, analyticsdomain.ErrExportUnavailable):
		status, opts.Case, opts.Code = nethttp.StatusServiceUnavailable, responses.CaseInternalError, "analytics.export_unavailable"
		opts.Message = "Export storage is not configured"
	default:
		responses.Internal(c, err, "Failed to process the analytics export")
		return true
	}

	responses.FailureFor(c, status, opts)

	return true
}

func analyticsExportControllers(deps Deps, e *routes.AnalyticsExports) {
	exports := deps.AnalyticsExports
	if exports == nil {
		return
	}

	e.Create = gate(deps, analyticsdomain.PermExport, adminRoles, adminAnalyticsExportHandler(exports))
	e.List = gate(deps, analyticsdomain.PermExport, adminRoles, adminAnalyticsExportsListHandler(exports))
	e.Get = gate(deps, analyticsdomain.PermExport, adminRoles, adminAnalyticsExportGetHandler(exports))
}
