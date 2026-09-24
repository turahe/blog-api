package handlers

import (
	"context"
	"errors"
	"io"
	nethttp "net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/middleware"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	mediadomain "github.com/turahe/blog-api/internal/core/media/domain"
	mediaservice "github.com/turahe/blog-api/internal/core/media/service"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
	userservice "github.com/turahe/blog-api/internal/core/user/service"
)

// multipartOverhead is allowed on top of the avatar size for multipart framing.
const multipartOverhead = 64 << 10

type profileAPI interface {
	Get(ctx context.Context, userID uuid.UUID) (userdomain.ProfileView, error)
	Update(ctx context.Context, actor, target uuid.UUID, patch userdomain.ProfilePatch) (userdomain.ProfileView, error)
	Public(ctx context.Context, ref string, viewer *uuid.UUID, privileged bool) (userdomain.ProfileView, error)
	UploadAvatar(ctx context.Context, userID uuid.UUID, filename string, data []byte) (mediadomain.MediaAsset, error)
	DeleteAvatar(ctx context.Context, userID uuid.UUID) error
}

// mePatchProfileHandler godoc
//
//	@Summary		Update my profile
//	@Description	Allowlisted fields only; omitted keys are kept, null clears, unknown keys are rejected.
//	@Tags			self-service
//	@Accept			json
//	@Produce		json
//	@Param			body	body		requests.PatchProfile	true	"profile fields"
//	@Success		200		{object}	responses.Envelope
//	@Failure		400		{object}	responses.Envelope
//	@Failure		401		{object}	responses.Envelope
//	@Failure		409		{object}	responses.Envelope
//	@Failure		429		{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/me/profile [patch]
func mePatchProfileHandler(profiles profileAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := currentUser(c)
		if !ok {
			return
		}

		patchProfile(c, profiles, userID, userID, true)
	}
}

// adminGetProfileHandler godoc
//
//	@Summary	Get a user's profile
//	@Tags		admin
//	@Produce	json
//	@Param		param1	path		string	true	"user UUID"
//	@Success	200		{object}	responses.Envelope
//	@Failure	404		{object}	responses.Envelope
//	@Security	Bearer
//	@Router		/api/v1/admin/users/{param1}/profile [get]
func adminGetProfileHandler(profiles profileAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		target, ok := userParam(c)
		if !ok {
			return
		}

		view, err := profiles.Get(c.Request.Context(), target)
		if writeProfileError(c, err, false) {
			return
		}

		responses.SuccessFor(c, nethttp.StatusOK, responses.ServiceUsers, responses.CaseSuccess, responses.AdminProfile(view))
	}
}

// adminPatchProfileHandler godoc
//
//	@Summary	Update a user's profile
//	@Tags		admin
//	@Accept		json
//	@Produce	json
//	@Param		param1	path		string					true	"user UUID"
//	@Param		body	body		requests.PatchProfile	true	"profile fields"
//	@Success	200		{object}	responses.Envelope
//	@Failure	400		{object}	responses.Envelope
//	@Failure	404		{object}	responses.Envelope
//	@Failure	409		{object}	responses.Envelope
//	@Security	Bearer
//	@Router		/api/v1/admin/users/{param1}/profile [patch]
func adminPatchProfileHandler(profiles profileAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		actor, ok := currentUser(c)
		if !ok {
			return
		}

		target, ok := userParam(c)
		if !ok {
			return
		}

		patchProfile(c, profiles, actor, target, false)
	}
}

func patchProfile(c *gin.Context, profiles profileAPI, actor, target uuid.UUID, self bool) {
	patch, err := decodeProfilePatch(c.Request.Body)
	if err != nil {
		writeProfilePatchError(c, err)
		return
	}

	view, err := profiles.Update(c.Request.Context(), actor, target, patch)
	if writeProfileError(c, err, self) {
		return
	}

	if self {
		responses.SuccessFor(c, nethttp.StatusOK, responses.ServiceUsers, responses.CaseSuccess, responses.Profile(view))
		return
	}

	responses.SuccessFor(c, nethttp.StatusOK, responses.ServiceUsers, responses.CaseSuccess, responses.AdminProfile(view))
}

// publicUserProfileHandler godoc
//
//	@Summary		Public user profile
//	@Description	Looked up by username or user UUID. Private profiles are 404 except for the owner and admins.
//	@Tags			public
//	@Produce		json
//	@Param			param1	path		string	true	"username or user UUID"
//	@Success		200		{object}	responses.Envelope
//	@Failure		404		{object}	responses.Envelope
//	@Router			/api/v1/users/{param1} [get]
func publicUserProfileHandler(profiles profileAPI, privileged func(*gin.Context) bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		var viewer *uuid.UUID
		if id, ok := middleware.CurrentUserID(c); ok {
			viewer = &id
		}

		view, err := profiles.Public(c.Request.Context(), c.Param("param1"), viewer, viewer != nil && privileged(c))
		if writeProfileError(c, err, false) {
			return
		}

		if !view.Privacy.AllowIndexing || view.Privacy.Visibility != userdomain.VisibilityPublic {
			c.Header("X-Robots-Tag", "noindex")
		}

		responses.SuccessFor(c, nethttp.StatusOK, responses.ServiceUsers, responses.CaseSuccess, responses.PublicProfile(view))
	}
}

// meUploadAvatarHandler godoc
//
//	@Summary		Upload my avatar
//	@Description	Multipart field "file". JPEG, PNG, WebP, or GIF identified by content (not the declared type), at most 8192x8192.
//	@Tags			self-service
//	@Accept			multipart/form-data
//	@Produce		json
//	@Param			file	formData	file	true	"image"
//	@Success		201		{object}	responses.Envelope
//	@Failure		400		{object}	responses.Envelope
//	@Failure		413		{object}	responses.Envelope
//	@Failure		429		{object}	responses.Envelope
//	@Failure		502		{object}	responses.Envelope
//	@Failure		503		{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/me/avatar [post]
func meUploadAvatarHandler(profiles profileAPI, maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := currentUser(c)
		if !ok {
			return
		}

		filename, data, ok := readAvatar(c, maxBytes)
		if !ok {
			return
		}

		asset, err := profiles.UploadAvatar(c.Request.Context(), userID, filename, data)
		if writeProfileError(c, err, true) {
			return
		}

		responses.SuccessFor(c, nethttp.StatusCreated, responses.ServiceUsers, responses.CaseSuccess, gin.H{
			"avatar": responses.MediaAsset(asset),
		})
	}
}

// readAvatar reads the multipart "file" field, writing the error response when it cannot.
func readAvatar(c *gin.Context, maxBytes int64) (string, []byte, bool) {
	c.Request.Body = nethttp.MaxBytesReader(c.Writer, c.Request.Body, maxBytes+multipartOverhead)

	file, header, err := c.Request.FormFile("file")
	if c.Request.MultipartForm != nil {
		defer func() { _ = c.Request.MultipartForm.RemoveAll() }()
	}

	if err != nil {
		if _, tooLarge := errors.AsType[*nethttp.MaxBytesError](err); tooLarge {
			failAvatarTooLarge(c, maxBytes)
			return "", nil, false
		}

		responses.Failure(c, nethttp.StatusBadRequest, responses.ErrorCodeValidation, `Multipart field "file" is required`)

		return "", nil, false
	}

	defer func() { _ = file.Close() }()

	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		responses.Failure(c, nethttp.StatusBadRequest, responses.ErrorCodeValidation, "Failed to read upload")
		return "", nil, false
	}

	if int64(len(data)) > maxBytes {
		failAvatarTooLarge(c, maxBytes)
		return "", nil, false
	}

	return header.Filename, data, true
}

func failAvatarTooLarge(c *gin.Context, maxBytes int64) {
	responses.FailureFor(c, nethttp.StatusRequestEntityTooLarge, responses.FailureOpts{
		Service: responses.ServiceUsers,
		Case:    responses.CaseValidation,
		Code:    "profile.avatar_too_large",
		Message: "Avatar exceeds the upload limit",
		Details: gin.H{"max_bytes": maxBytes},
	})
}

// meDeleteAvatarHandler godoc
//
//	@Summary	Remove my avatar
//	@Tags		self-service
//	@Produce	json
//	@Success	200	{object}	responses.Envelope
//	@Failure	401	{object}	responses.Envelope
//	@Failure	429	{object}	responses.Envelope
//	@Security	Bearer
//	@Router		/api/v1/me/avatar [delete]
func meDeleteAvatarHandler(profiles profileAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := currentUser(c)
		if !ok {
			return
		}

		if writeProfileError(c, profiles.DeleteAvatar(c.Request.Context(), userID), true) {
			return
		}

		responses.SuccessFor(c, nethttp.StatusOK, responses.ServiceUsers, responses.CaseSuccess, gin.H{"avatar_id": nil})
	}
}

func currentUser(c *gin.Context) (uuid.UUID, bool) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		responses.Failure(c, nethttp.StatusUnauthorized, responses.ErrorCodeUnauthorized, "Authentication required")
	}

	return userID, ok
}

func userParam(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(strings.TrimSpace(c.Param("param1")))
	if err != nil {
		responses.Failure(c, nethttp.StatusBadRequest, responses.ErrorCodeValidation, "Invalid user id")
		return uuid.Nil, false
	}

	return id, true
}

func writeProfilePatchError(c *gin.Context, err error) {
	code := responses.ErrorCodeValidation

	switch {
	case errors.Is(err, errUnknownProfileField):
		code = "profile.unknown_field"
	case errors.Is(err, errUnsupportedProfileField):
		code = "profile.field_unsupported"
	}

	responses.FailureFor(c, nethttp.StatusBadRequest, responses.FailureOpts{
		Service: responses.ServiceUsers,
		Case:    responses.CaseValidation,
		Code:    code,
		Message: err.Error(),
	})
}

// writeProfileError sends the envelope for err and reports whether it did. For self
// routes a missing user means the token outlived the account, so it is a 401.
func writeProfileError(c *gin.Context, err error, self bool) bool {
	if err == nil {
		return false
	}

	status, caseCode, code, message := nethttp.StatusInternalServerError, responses.CaseInternalError, responses.ErrorCodeInternal, "Failed to process profile"

	switch {
	case errors.Is(err, userdomain.ErrNotFound) && self:
		status, caseCode, code, message = nethttp.StatusUnauthorized, responses.CaseUnauthorized, responses.ErrorCodeUnauthorized, "User not found"
	case errors.Is(err, userdomain.ErrNotFound):
		status, caseCode, code, message = nethttp.StatusNotFound, responses.CaseNotFound, responses.ErrorCodeNotFound, "User not found"
	case errors.Is(err, userdomain.ErrProfileValidation), errors.Is(err, mediaservice.ErrValidation):
		status, caseCode, code, message = nethttp.StatusBadRequest, responses.CaseValidation, responses.ErrorCodeValidation, err.Error()
	case errors.Is(err, userservice.ErrEmptyPatch):
		status, caseCode, code, message = nethttp.StatusBadRequest, responses.CaseValidation, responses.ErrorCodeValidation, "No profile fields to update"
	case errors.Is(err, userdomain.ErrDisplayNameTaken):
		status, caseCode, code, message = nethttp.StatusConflict, responses.CaseConflict, "profile.display_name_taken", "Display name is already taken"
	case errors.Is(err, mediaservice.ErrStorage):
		responses.RecordError(c, err)

		status, caseCode, code, message = nethttp.StatusBadGateway, responses.CaseInternalError, "storage_unavailable", "Storage unavailable"
	case errors.Is(err, userservice.ErrAvatarsUnavailable):
		status, caseCode, code, message = nethttp.StatusServiceUnavailable, responses.CaseInternalError, "profile.avatar_unavailable", "Avatar storage is not configured"
	default:
		responses.RecordError(c, err)
	}

	responses.FailureFor(c, status, responses.FailureOpts{
		Service: responses.ServiceUsers,
		Case:    caseCode,
		Code:    code,
		Message: message,
	})

	return true
}
