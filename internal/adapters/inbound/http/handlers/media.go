package handlers

import (
	"errors"
	nethttp "net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/middleware"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/requests"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	mediadomain "github.com/turahe/blog-api/internal/core/media/domain"
	mediaports "github.com/turahe/blog-api/internal/core/media/ports"
	mediaservice "github.com/turahe/blog-api/internal/core/media/service"
)

// adminPresignMediaHandler godoc
//
//	@Summary	Presign media upload
//	@Tags		admin
//	@Accept		json
//	@Produce	json
//	@Param		body	body		requests.PresignMedia	true	"upload metadata"
//	@Success	201		{object}	responses.Envelope
//	@Failure	400		{object}	responses.Envelope
//	@Security	Bearer
//	@Router		/api/v1/admin/media [post]
func adminPresignMediaHandler(media mediaports.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := middleware.CurrentUserID(c)
		if !ok {
			responses.Failure(c, nethttp.StatusUnauthorized, responses.ErrorCodeUnauthorized, "Authentication required")
			return
		}

		var req requests.PresignMedia
		if !requests.BindJSON(c, &req) {
			return
		}

		uploadedBy := userID

		result, err := media.PresignUpload(
			c.Request.Context(),
			&uploadedBy,
			req.OriginalFilename,
			req.ContentType,
			req.SizeBytes,
			req.Tags,
		)
		if mapMediaError(c, err) {
			return
		}

		responses.Success(c, nethttp.StatusCreated, responses.MediaPresign(result))
	}
}

// adminCompleteMediaHandler godoc
//
//	@Summary	Complete media upload
//	@Tags		admin
//	@Produce	json
//	@Param		param1	path		string	true	"media UUID"
//	@Success	200		{object}	responses.Envelope
//	@Failure	404		{object}	responses.Envelope
//	@Security	Bearer
//	@Router		/api/v1/admin/media/{param1}/complete [post]
func adminCompleteMediaHandler(media mediaports.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(strings.TrimSpace(c.Param("param1")))
		if err != nil {
			responses.Failure(c, nethttp.StatusBadRequest, responses.ErrorCodeValidation, "Invalid media id")
			return
		}

		asset, err := media.CompleteUpload(c.Request.Context(), id)
		if mapMediaError(c, err) {
			return
		}

		responses.Success(c, nethttp.StatusOK, responses.MediaAsset(asset))
	}
}

// adminListMediaHandler godoc
//
//	@Summary	List media assets
//	@Tags		admin
//	@Produce	json
//	@Param		page		query		int		false	"page"		default(1)
//	@Param		per_page	query		int		false	"per page"	default(20)
//	@Param		q			query		string	false	"search"
//	@Param		disk		query		string	false	"disk filter"
//	@Success	200			{object}	responses.Envelope
//	@Security	Bearer
//	@Router		/api/v1/admin/media [get]
func adminListMediaHandler(media mediaports.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		page := parsePositiveInt(c.Query("page"), 1)
		perPage := parsePositiveInt(c.Query("per_page"), 20)

		result, err := media.List(c.Request.Context(), mediadomain.ListFilter{
			Page:    page,
			PerPage: perPage,
			Query:   c.Query("q"),
			Disk:    c.Query("disk"),
		})
		if mapMediaError(c, err) {
			return
		}

		items := make([]gin.H, 0, len(result.Items))
		for _, asset := range result.Items {
			items = append(items, responses.MediaAsset(asset))
		}

		responses.SuccessPaginatedFor(c, nethttp.StatusOK, responses.PageOpts{
			Service: responses.ServiceMedia,
			Data:    items,
			Page:    result.Page,
			PerPage: result.PerPage,
			Total:   result.Total,
		})
	}
}

// adminDeleteMediaHandler godoc
//
//	@Summary	Delete media asset
//	@Tags		admin
//	@Param		param1	path		string	true	"media UUID"
//	@Success	200		{object}	responses.Envelope
//	@Security	Bearer
//	@Router		/api/v1/admin/media/{param1} [delete]
func adminDeleteMediaHandler(media mediaports.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(strings.TrimSpace(c.Param("param1")))
		if err != nil {
			responses.Failure(c, nethttp.StatusBadRequest, responses.ErrorCodeValidation, "Invalid media id")
			return
		}

		if err := media.Delete(c.Request.Context(), id); mapMediaError(c, err) {
			return
		}

		responses.Success(c, nethttp.StatusOK, nil)
	}
}

// adminPatchMediaTagsHandler godoc
//
//	@Summary	Patch media tags
//	@Tags		admin
//	@Accept		json
//	@Produce	json
//	@Param		param1	path		string					true	"media UUID"
//	@Param		body	body		requests.PatchMediaTags	true	"tags"
//	@Success	200		{object}	responses.Envelope
//	@Security	Bearer
//	@Router		/api/v1/admin/media/{param1}/tags [patch]
func adminPatchMediaTagsHandler(media mediaports.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(strings.TrimSpace(c.Param("param1")))
		if err != nil {
			responses.Failure(c, nethttp.StatusBadRequest, responses.ErrorCodeValidation, "Invalid media id")
			return
		}

		var req requests.PatchMediaTags
		if !requests.BindJSON(c, &req) {
			return
		}

		asset, err := media.UpdateTags(c.Request.Context(), id, req.Tags)
		if mapMediaError(c, err) {
			return
		}

		responses.Success(c, nethttp.StatusOK, responses.MediaAsset(asset))
	}
}

// publicGetMediaHandler godoc
//
//	@Summary	Get ready media asset
//	@Tags		public
//	@Produce	json
//	@Param		param1	path		string	true	"media UUID"
//	@Success	200		{object}	responses.Envelope
//	@Failure	404		{object}	responses.Envelope
//	@Router		/api/v1/media/{param1} [get]
func publicGetMediaHandler(media mediaports.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(strings.TrimSpace(c.Param("param1")))
		if err != nil {
			responses.Failure(c, nethttp.StatusBadRequest, responses.ErrorCodeValidation, "Invalid media id")
			return
		}

		asset, err := media.GetReady(c.Request.Context(), id)
		if mapMediaError(c, err) {
			return
		}

		responses.Success(c, nethttp.StatusOK, responses.MediaAsset(asset))
	}
}

func parsePositiveInt(raw string, fallback int) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 1 {
		return fallback
	}

	return n
}

func mapMediaError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}

	switch {
	case errors.Is(err, mediaservice.ErrValidation):
		responses.FailureFor(c, nethttp.StatusBadRequest, responses.FailureOpts{
			Service: responses.ServiceMedia,
			Case:    responses.CaseValidation,
			Code:    responses.ErrorCodeValidation,
			Message: err.Error(),
			Details: nil,
		})
	case errors.Is(err, mediaservice.ErrNotFound):
		responses.FailureFor(c, nethttp.StatusNotFound, responses.FailureOpts{
			Service: responses.ServiceMedia,
			Case:    responses.CaseNotFound,
			Code:    responses.ErrorCodeNotFound,
			Message: "Media not found",
			Details: nil,
		})
	case errors.Is(err, mediaservice.ErrUploadIncomplete):
		responses.FailureFor(c, nethttp.StatusConflict, responses.FailureOpts{
			Service: responses.ServiceMedia,
			Case:    responses.CaseConflict,
			Code:    "media.upload_incomplete",
			Message: "Upload incomplete",
			Details: nil,
		})
	case errors.Is(err, mediaservice.ErrUploadExpired):
		responses.FailureFor(c, nethttp.StatusConflict, responses.FailureOpts{
			Service: responses.ServiceMedia,
			Case:    responses.CaseConflict,
			Code:    "media.upload_expired",
			Message: "Upload expired",
			Details: nil,
		})
	case errors.Is(err, mediaservice.ErrStorage):
		responses.RecordError(c, err)
		responses.FailureFor(c, nethttp.StatusBadGateway, responses.FailureOpts{
			Service: responses.ServiceMedia,
			Case:    responses.CaseInternalError,
			Code:    "storage_unavailable",
			Message: "Storage unavailable",
			Details: nil,
		})
	default:
		responses.RecordError(c, err)
		responses.FailureFor(c, nethttp.StatusInternalServerError, responses.FailureOpts{
			Service: responses.ServiceMedia,
			Case:    responses.CaseInternalError,
			Code:    responses.ErrorCodeInternal,
			Message: "Failed to process media",
			Details: nil,
		})
	}

	return true
}
