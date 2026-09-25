package handlers

import (
	"context"
	"errors"
	nethttp "net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/requests"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
	privacydomain "github.com/turahe/blog-api/internal/core/privacy/domain"
	privacyservice "github.com/turahe/blog-api/internal/core/privacy/service"
)

type privacyRequestsAPI interface {
	RequestExport(ctx context.Context, userID uuid.UUID) (privacyservice.Export, bool, error)
	RequestErase(ctx context.Context, userID uuid.UUID, password string) (privacydomain.Request, bool, error)
}

// meExportHandler godoc
//
//	@Summary		Export my personal data
//	@Description	Queues a JSON export of everything stored about the caller, or returns the open or still
//	@Description	downloadable one. 202 while it is being prepared (poll this endpoint); 200 with a short-lived
//	@Description	download_url once ready. After the archive expires, the next call queues a new export.
//	@Tags			self-service
//	@Produce		json
//	@Success		200	{object}	responses.Envelope
//	@Success		202	{object}	responses.Envelope
//	@Failure		401	{object}	responses.Envelope
//	@Failure		429	{object}	responses.Envelope
//	@Failure		503	{object}	responses.Envelope	"privacy.export_unavailable"
//	@Security		Bearer
//	@Router			/api/v1/me/activity/export [get]
func meExportHandler(privacy privacyRequestsAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := currentUser(c)
		if !ok {
			return
		}

		export, _, err := privacy.RequestExport(c.Request.Context(), userID)
		if writePrivacyRequestError(c, err) {
			return
		}

		status := nethttp.StatusAccepted
		if export.DownloadURL != "" {
			status = nethttp.StatusOK
		}

		c.Header("Cache-Control", "no-store")
		responses.SuccessFor(c, status, responses.ServiceUsers, responses.CaseSuccess,
			responses.PrivacyRequest(export.Request, export.DownloadURL, export.DownloadExpiresAt))
	}
}

// meEraseHandler godoc
//
//	@Summary		Erase my account
//	@Description	Queues the anonymization of the caller's account: activity, consents, sessions, and tokens
//	@Description	are deleted; email, name, username, avatar, and profile are scrubbed; posts and comments stay,
//	@Description	shown as "Deleted user". Runs within minutes and cannot be undone. Repeating the call while
//	@Description	the erasure is pending returns the same request.
//	@Tags			self-service
//	@Accept			json
//	@Produce		json
//	@Param			body	body		requests.EraseAccount	true	"password confirmation"
//	@Success		202		{object}	responses.Envelope
//	@Failure		400		{object}	responses.Envelope
//	@Failure		401		{object}	responses.Envelope
//	@Failure		403		{object}	responses.Envelope	"privacy.erase_requires_reauth"
//	@Failure		429		{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/me/activity/erase [post]
func meEraseHandler(privacy privacyRequestsAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := currentUser(c)
		if !ok {
			return
		}

		var body requests.EraseAccount
		if !requests.BindJSON(c, &body) {
			return
		}

		request, _, err := privacy.RequestErase(c.Request.Context(), userID, body.CurrentPassword)
		if writePrivacyRequestError(c, err) {
			return
		}

		responses.SuccessFor(c, nethttp.StatusAccepted, responses.ServiceUsers, responses.CaseSuccess,
			responses.PrivacyRequest(request, "", nil))
	}
}

func writePrivacyRequestError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}

	opts := responses.FailureOpts{Service: responses.ServiceUsers}
	status := nethttp.StatusInternalServerError

	switch {
	case errors.Is(err, privacydomain.ErrReauth):
		status, opts.Case, opts.Code = nethttp.StatusForbidden, responses.CaseForbidden, "privacy.erase_requires_reauth"
		opts.Message = "Erasing your account requires your current password"
	case errors.Is(err, privacydomain.ErrExportUnavailable):
		status, opts.Case, opts.Code = nethttp.StatusServiceUnavailable, responses.CaseInternalError, "privacy.export_unavailable"
		opts.Message = "Data export storage is not configured"
	case errors.Is(err, authdomain.ErrUserInactive):
		status, opts.Case, opts.Code = nethttp.StatusUnauthorized, responses.CaseUnauthorized, responses.ErrorCodeUnauthorized
		opts.Message = "Account is not active"
	default:
		responses.RecordError(c, err)

		opts.Case, opts.Code, opts.Message = responses.CaseInternalError, responses.ErrorCodeInternal, "Failed to process privacy request"
	}

	responses.FailureFor(c, status, opts)

	return true
}
