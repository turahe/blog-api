package handlers

import (
	"context"
	"errors"
	nethttp "net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/requests"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
)

type privacyAPI interface {
	Privacy(ctx context.Context, userID uuid.UUID) (userdomain.Privacy, error)
	UpdatePrivacy(ctx context.Context, userID uuid.UUID, patch userdomain.PrivacyPatch, password string) (userdomain.Privacy, error)
}

// meGetPrivacyHandler godoc
//
//	@Summary	Get my privacy settings
//	@Tags		self-service
//	@Produce	json
//	@Success	200	{object}	responses.Envelope
//	@Failure	401	{object}	responses.Envelope
//	@Security	Bearer
//	@Router		/api/v1/me/privacy [get]
func meGetPrivacyHandler(privacy privacyAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := currentUser(c)
		if !ok {
			return
		}

		settings, err := privacy.Privacy(c.Request.Context(), userID)
		if writePrivacyError(c, err) {
			return
		}

		responses.SuccessFor(c, nethttp.StatusOK, responses.ServiceUsers, responses.CaseSuccess, responses.Privacy(settings))
	}
}

// meUpdatePrivacyHandler godoc
//
//	@Summary		Update my privacy settings
//	@Description	Partial update; omitted flags are kept. Narrowing visibility_profile (public → unlisted → private)
//	@Description	requires current_password; widening it and the other flags do not.
//	@Tags			self-service
//	@Accept			json
//	@Produce		json
//	@Param			body	body		requests.UpdatePrivacy	true	"privacy flags"
//	@Success		200		{object}	responses.Envelope
//	@Failure		400		{object}	responses.Envelope
//	@Failure		401		{object}	responses.Envelope
//	@Failure		403		{object}	responses.Envelope	"privacy.level_change_requires_reauth"
//	@Failure		429		{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/me/privacy [put]
func meUpdatePrivacyHandler(privacy privacyAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := currentUser(c)
		if !ok {
			return
		}

		var body requests.UpdatePrivacy
		if !requests.BindJSON(c, &body) {
			return
		}

		patch := userdomain.PrivacyPatch{
			ShowEmail:     body.VisibilityEmail,
			ShowContact:   body.VisibilityContact,
			AllowIndexing: body.SearchAllowIndexing,
		}
		if body.VisibilityProfile != nil {
			v := userdomain.Visibility(*body.VisibilityProfile)
			patch.Visibility = &v
		}

		settings, err := privacy.UpdatePrivacy(c.Request.Context(), userID, patch, body.CurrentPassword)
		if writePrivacyError(c, err) {
			return
		}

		responses.SuccessFor(c, nethttp.StatusOK, responses.ServiceUsers, responses.CaseSuccess, responses.Privacy(settings))
	}
}

func writePrivacyError(c *gin.Context, err error) bool {
	if !errors.Is(err, userdomain.ErrPrivacyReauth) {
		return writeProfileError(c, err, true)
	}

	responses.FailureFor(c, nethttp.StatusForbidden, responses.FailureOpts{
		Service: responses.ServiceUsers,
		Case:    responses.CaseForbidden,
		Code:    "privacy.level_change_requires_reauth",
		Message: "Narrowing profile visibility requires your current password",
	})

	return true
}
