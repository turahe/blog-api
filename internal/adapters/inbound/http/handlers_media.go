package http

import (
	"errors"
	nethttp "net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	mediadomain "github.com/turahe/blog-api/internal/core/media/domain"
	mediaports "github.com/turahe/blog-api/internal/core/media/ports"
	mediaservice "github.com/turahe/blog-api/internal/core/media/service"
)

type mediaPresignRequest struct {
	OriginalFilename string   `json:"original_filename"`
	ContentType      string   `json:"content_type"`
	SizeBytes        int64    `json:"size_bytes"`
	Tags             []string `json:"tags"`
}

func adminPresignMediaHandler(media mediaports.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := currentUserID(c)
		if !ok {
			failure(c, nethttp.StatusUnauthorized, "unauthorized", "Authentication required")
			return
		}

		var req mediaPresignRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid request body")
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

		success(c, nethttp.StatusCreated, mediaPresignJSON(result))
	}
}

func adminCompleteMediaHandler(media mediaports.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(strings.TrimSpace(c.Param("param1")))
		if err != nil {
			failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid media id")
			return
		}

		asset, err := media.CompleteUpload(c.Request.Context(), id)
		if mapMediaError(c, err) {
			return
		}

		success(c, nethttp.StatusOK, mediaAssetJSON(asset))
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
			items = append(items, mediaAssetJSON(asset))
		}
		successWithMeta(c, nethttp.StatusOK, gin.H{"items": items}, &Meta{
			Page:    result.Page,
			PerPage: result.PerPage,
			Total:   result.Total,
		})
	}
}

func adminDeleteMediaHandler(media mediaports.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(strings.TrimSpace(c.Param("param1")))
		if err != nil {
			failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid media id")
			return
		}
		if err := media.Delete(c.Request.Context(), id); mapMediaError(c, err) {
			return
		}
		success(c, nethttp.StatusOK, nil)
	}
}

type mediaTagsRequest struct {
	Tags []string `json:"tags"`
}

func adminPatchMediaTagsHandler(media mediaports.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(strings.TrimSpace(c.Param("param1")))
		if err != nil {
			failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid media id")
			return
		}
		var req mediaTagsRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid request body")
			return
		}
		asset, err := media.UpdateTags(c.Request.Context(), id, req.Tags)
		if mapMediaError(c, err) {
			return
		}
		success(c, nethttp.StatusOK, mediaAssetJSON(asset))
	}
}

func publicGetMediaHandler(media mediaports.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(strings.TrimSpace(c.Param("param1")))
		if err != nil {
			failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid media id")
			return
		}
		asset, err := media.GetReady(c.Request.Context(), id)
		if mapMediaError(c, err) {
			return
		}
		success(c, nethttp.StatusOK, mediaAssetJSON(asset))
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
		failure(c, nethttp.StatusBadRequest, "validation_error", err.Error())
	case errors.Is(err, mediaservice.ErrNotFound):
		failure(c, nethttp.StatusNotFound, "not_found", "Media not found")
	case errors.Is(err, mediaservice.ErrUploadIncomplete):
		failure(c, nethttp.StatusConflict, "media.upload_incomplete", "Upload incomplete")
	case errors.Is(err, mediaservice.ErrUploadExpired):
		failure(c, nethttp.StatusConflict, "media.upload_expired", "Upload expired")
	case errors.Is(err, mediaservice.ErrStorage):
		failure(c, nethttp.StatusBadGateway, "storage_unavailable", "Storage unavailable")
	default:
		failure(c, nethttp.StatusBadGateway, "storage_unavailable", "Storage unavailable")
	}
	return true
}

func mediaPresignJSON(result mediadomain.PresignResult) gin.H {
	headers := result.RequiredHeaders
	if headers == nil {
		headers = map[string]string{}
	}
	return gin.H{
		"media_id":         result.Asset.ID.String(),
		"storage_key":      result.Asset.StorageKey,
		"upload_url":       result.UploadURL,
		"required_headers": headers,
		"expires_at":       result.ExpiresAt.UTC().Format(time.RFC3339),
		"disk":             result.Asset.Disk,
	}
}

func mediaAssetJSON(asset mediadomain.MediaAsset) gin.H {
	var uploadedBy any
	if asset.UploadedBy != nil {
		uploadedBy = asset.UploadedBy.String()
	}
	return gin.H{
		"id":                asset.ID.String(),
		"storage_key":       asset.StorageKey,
		"original_filename": asset.OriginalFilename,
		"content_type":      asset.ContentType,
		"size_bytes":        asset.SizeBytes,
		"width":             asset.Width,
		"height":            asset.Height,
		"checksum_sha256":   asset.ChecksumSHA256,
		"disk":              asset.Disk,
		"status":            asset.Status,
		"tags":              asset.Tags,
		"uploaded_by":       uploadedBy,
		"created_at":        asset.CreatedAt.UTC().Format(time.RFC3339),
		"updated_at":        asset.UpdatedAt.UTC().Format(time.RFC3339),
	}
}
