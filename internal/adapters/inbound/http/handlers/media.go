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
	"github.com/turahe/blog-api/internal/shared/pagination"
)

var mediaAdminCfg = pagination.CursorConfig{
	Kind: "media_admin",
	Sort: []pagination.SortField{
		{Name: "created_at", Dir: pagination.Desc, Type: pagination.TypeTime},
		{Name: "id", Dir: pagination.Desc, Type: pagination.TypeInt64},
	},
	TTL:            pagination.DefaultTTL,
	MaxPerPage:     pagination.DefaultMaxPerPage,
	DefaultPerPage: pagination.DefaultPerPage,
}

// adminPresignMediaHandler godoc
//
//	@Summary	Presign media upload
//	@Tags		admin
//	@Accept		json
//	@Produce	json
//	@Param		body	body		requests.PresignMedia	true	"upload metadata"
//	@Success	201		{object}	responses.Envelope
//	@Failure	400		{object}	responses.Envelope
//	@Failure	429		{object}	responses.Envelope
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

		writeMediaAsset(c, media, asset)
	}
}

// adminListMediaHandler godoc
//
//	@Summary		List media assets
//	@Description	Supports both legacy offset pagination (page/perPage) and cursor-based keyset pagination (after/before/limit).
//	@Tags			admin
//	@Produce		json
//	@Param			page			query		int		false	"page (legacy offset mode)"						default(1)
//	@Param			perPage			query		int		false	"per page (legacy offset mode, alias limit)"	default(20)
//	@Param			limit			query		int		false	"page size (cursor or offset)"					default(20)
//	@Param			q				query		string	false	"search"
//	@Param			disk			query		string	false	"disk filter"
//	@Param			status			query		string	false	"status filter"	Enums(pending, ready, failed)
//	@Param			unused			query		bool	false	"only ready assets no avatar, category, post cover, attachment, or SEO image references"
//	@Param			after			query		string	false	"opaque cursor: return items after this point"
//	@Param			before			query		string	false	"opaque cursor: return items before this point"
//	@Param			includeTotal	query		bool	false	"when false, skip COUNT(*) to reduce DB load"	default(true)
//	@Success		200				{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/admin/media [get]
func adminListMediaHandler(media mediaports.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		pr, err := pagination.ParseRequest(c, mediaAdminCfg)
		if err != nil {
			responses.Failure(c, nethttp.StatusBadRequest, pagination.ErrorCode(err), pagination.ErrorCause(err))
			return
		}

		unused, _ := parseBoolQuery(c, "unused", false)
		filter := mediadomain.ListFilter{
			PageRequest: pr,
			Query:       c.Query("q"),
			Disk:        c.Query("disk"),
			Status:      c.Query("status"),
			Unused:      unused,
		}

		result, err := media.List(c.Request.Context(), filter)
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
				mapMediaError(c, err)
			}
			return
		}

		items, ok := renderMedia(c, media, result.Items...)
		if !ok {
			return
		}

		responses.SuccessPaginatedResult[mediadomain.MediaAsset](c, nethttp.StatusOK, responses.CursorPageOpts[mediadomain.MediaAsset]{
			Service: responses.ServiceMedia,
			Result:  result,
			Data:    items,
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

		writeMediaAsset(c, media, asset)
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

		writeMediaAsset(c, media, asset)
	}
}

// transformRedirectCache bounds how long a cache may keep serving the redirect after the asset
// is deleted; the signed URL itself expires after MEDIA_TRANSFORM_URL_TTL.
const transformRedirectCache = "public, max-age=300"

// publicTransformMediaHandler godoc
//
//	@Summary		Redirect to a resized image
//	@Description	Validates the width against MEDIA_TRANSFORM_WIDTHS and redirects to a signed, expiring imgproxy URL. Images are never enlarged. Without imgproxy configured the endpoint answers 501.
//	@Tags			public
//	@Param			param1	path	string	true	"media UUID"
//	@Param			w		query	int		true	"width in pixels, one of MEDIA_TRANSFORM_WIDTHS"
//	@Param			format	query	string	false	"output format; omitted keeps the source format"	Enums(webp, avif, jpeg, png)
//	@Success		302
//	@Failure		400	{object}	responses.Envelope
//	@Failure		404	{object}	responses.Envelope
//	@Failure		501	{object}	responses.Envelope
//	@Router			/api/v1/media/{param1}/transform [get]
func publicTransformMediaHandler(media mediaports.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(strings.TrimSpace(c.Param("param1")))
		if err != nil {
			responses.Failure(c, nethttp.StatusBadRequest, responses.ErrorCodeValidation, "Invalid media id")
			return
		}

		width, err := strconv.Atoi(strings.TrimSpace(c.Query("w")))
		if err != nil {
			responses.Failure(c, nethttp.StatusBadRequest, responses.ErrorCodeValidation, "w must be an integer width")
			return
		}

		target, err := media.TransformURL(c.Request.Context(), id, mediadomain.Transform{
			Width:  width,
			Format: strings.ToLower(strings.TrimSpace(c.Query("format"))),
		})
		if errors.Is(err, mediaservice.ErrTransformDisabled) {
			responses.Failure(c, nethttp.StatusNotImplemented, "media.transform_disabled", "Image transforms are not configured")
			return
		}

		if mapMediaError(c, err) {
			return
		}

		c.Header("Cache-Control", transformRedirectCache)
		c.Redirect(nethttp.StatusFound, target)
	}
}

// adminMediaUsageHandler godoc
//
//	@Summary		Storage usage
//	@Description	Counts and bytes of stored media by status (soft-deleted assets as deleted until the orphan cleanup purges them), content type, and top uploaders. userId narrows the report to one uploader.
//	@Tags			admin
//	@Produce		json
//	@Param			userId	query		string	false	"uploader UUID"
//	@Param			top		query		int		false	"uploaders to list (1-100)"	default(10)
//	@Success		200		{object}	responses.Envelope
//	@Failure		400		{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/admin/media/usage [get]
func adminMediaUsageHandler(media mediaports.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		var topLimit int
		if raw := strings.TrimSpace(c.Query("top")); raw != "" {
			if n, err := strconv.Atoi(raw); err == nil && n >= 1 {
				topLimit = n
			}
		}
		filter := mediadomain.UsageFilter{TopLimit: topLimit}

		if raw := strings.TrimSpace(c.Query("userId")); raw != "" {
			id, err := uuid.Parse(raw)
			if err != nil {
				responses.Failure(c, nethttp.StatusBadRequest, responses.ErrorCodeValidation, "userId must be a UUID")
				return
			}

			filter.UploadedBy = &id
		}

		usage, err := media.Usage(c.Request.Context(), filter)
		if mapMediaError(c, err) {
			return
		}

		c.Header("Cache-Control", "no-store")
		responses.SuccessFor(c, nethttp.StatusOK, responses.ServiceMedia, responses.CaseSuccess, responses.MediaUsage(usage))
	}
}

// renderMedia serializes assets with their preset URLs. It writes the error and returns false
// when the presets cannot be read.
func renderMedia(c *gin.Context, media mediaports.Service, assets ...mediadomain.MediaAsset) ([]gin.H, bool) {
	var variants map[uuid.UUID]map[string]string

	if media != nil {
		var err error
		if variants, err = media.Variants(c.Request.Context(), assets...); mapMediaError(c, err) {
			return nil, false
		}
	}

	out := make([]gin.H, 0, len(assets))
	for _, asset := range assets {
		var urls map[string]string
		if variants != nil {
			urls = variants[asset.UUID]
		}

		out = append(out, responses.MediaAssetWithVariants(asset, urls))
	}

	return out, true
}

func writeMediaAsset(c *gin.Context, media mediaports.Service, asset mediadomain.MediaAsset) {
	if items, ok := renderMedia(c, media, asset); ok {
		responses.Success(c, nethttp.StatusOK, items[0])
	}
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
