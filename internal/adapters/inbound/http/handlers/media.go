package handlers

import (
	"errors"
	nethttp "net/http"
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

func adminPresignMediaHandler(media mediaports.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := middleware.CurrentUserID(c)
		if !ok {
			responses.Failure(c, nethttp.StatusUnauthorized, "unauthorized", "Authentication required")
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

func adminCompleteMediaHandler(media mediaports.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(strings.TrimSpace(c.Param("param1")))
		if err != nil {
			responses.Failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid media id")
			return
		}

		asset, err := media.CompleteUpload(c.Request.Context(), id)
		if mapMediaError(c, err) {
			return
		}

		responses.Success(c, nethttp.StatusOK, responses.MediaAsset(asset))
	}
}

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
		responses.SuccessPaginatedFor(c, nethttp.StatusOK, responses.ServiceMedia, items, result.Page, result.PerPage, result.Total)
	}
}

func adminDeleteMediaHandler(media mediaports.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(strings.TrimSpace(c.Param("param1")))
		if err != nil {
			responses.Failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid media id")
			return
		}
		if err := media.Delete(c.Request.Context(), id); mapMediaError(c, err) {
			return
		}
		responses.Success(c, nethttp.StatusOK, nil)
	}
}

func adminPatchMediaTagsHandler(media mediaports.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(strings.TrimSpace(c.Param("param1")))
		if err != nil {
			responses.Failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid media id")
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

func publicGetMediaHandler(media mediaports.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(strings.TrimSpace(c.Param("param1")))
		if err != nil {
			responses.Failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid media id")
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
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback
	}
	var n int
	for _, ch := range raw {
		if ch < '0' || ch > '9' {
			return fallback
		}
		n = n*10 + int(ch-'0')
	}
	if n < 1 {
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
		responses.FailureFor(c, nethttp.StatusBadRequest, responses.ServiceMedia, responses.CaseValidation, "validation_error", err.Error(), nil)
	case errors.Is(err, mediaservice.ErrNotFound):
		responses.FailureFor(c, nethttp.StatusNotFound, responses.ServiceMedia, responses.CaseNotFound, "not_found", "Media not found", nil)
	case errors.Is(err, mediaservice.ErrUploadIncomplete):
		responses.FailureFor(c, nethttp.StatusConflict, responses.ServiceMedia, responses.CaseConflict, "media.upload_incomplete", "Upload incomplete", nil)
	case errors.Is(err, mediaservice.ErrUploadExpired):
		responses.FailureFor(c, nethttp.StatusConflict, responses.ServiceMedia, responses.CaseConflict, "media.upload_expired", "Upload expired", nil)
	case errors.Is(err, mediaservice.ErrStorage):
		responses.FailureFor(c, nethttp.StatusBadGateway, responses.ServiceMedia, responses.CaseInternalError, "storage_unavailable", "Storage unavailable", nil)
	default:
		responses.FailureFor(c, nethttp.StatusBadGateway, responses.ServiceMedia, responses.CaseInternalError, "storage_unavailable", "Storage unavailable", nil)
	}
	return true
}
