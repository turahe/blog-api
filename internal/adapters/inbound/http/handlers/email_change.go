package handlers

import (
	nethttp "net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/requests"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	authports "github.com/turahe/blog-api/internal/core/auth/ports"
	authservice "github.com/turahe/blog-api/internal/core/auth/service"
)

// meRequestEmailChangeHandler godoc
//
//	@Summary		Request an email change
//	@Description	Verifies the password and sends a confirmation token to the new address; the current address gets a notice.
//	@Tags			self-service
//	@Accept			json
//	@Produce		json
//	@Param			body	body		requests.RequestEmailChange	true	"new address and password"
//	@Success		202		{object}	responses.Envelope
//	@Failure		400		{object}	responses.Envelope
//	@Failure		403		{object}	responses.Envelope
//	@Failure		409		{object}	responses.Envelope
//	@Failure		429		{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/me/email/request-change [post]
func meRequestEmailChangeHandler(auth authports.EmailChanger) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := currentUser(c)
		if !ok {
			return
		}

		var req requests.RequestEmailChange
		if !requests.BindJSON(c, &req) {
			return
		}

		request, err := auth.RequestEmailChange(c.Request.Context(), userID, req.NewEmail, req.PasswordProof)
		if err != nil {
			writeAuthError(c, err, authservice.MapEmailChangeError)
			return
		}

		responses.SuccessFor(c, nethttp.StatusAccepted, responses.ServiceAuth, responses.CaseAccepted, gin.H{
			"newEmail":  request.NewEmail,
			"expiresAt": request.ExpiresAt.UTC().Format(time.RFC3339),
		})
	}
}

// meConfirmEmailChangeHandler godoc
//
//	@Summary		Confirm an email change
//	@Description	Consumes the token sent to the new address, marks it verified, and revokes every session.
//	@Tags			self-service
//	@Accept			json
//	@Produce		json
//	@Param			body	body		requests.ConfirmEmailChange	true	"confirmation token"
//	@Success		200		{object}	responses.Envelope
//	@Failure		400		{object}	responses.Envelope
//	@Failure		409		{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/me/email/confirm-change [post]
func meConfirmEmailChangeHandler(auth authports.EmailChanger) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := currentUser(c)
		if !ok {
			return
		}

		var req requests.ConfirmEmailChange
		if !requests.BindJSON(c, &req) {
			return
		}

		user, err := auth.ConfirmEmailChange(c.Request.Context(), userID, req.Token)
		if err != nil {
			writeAuthError(c, err, authservice.MapEmailChangeError)
			return
		}

		responses.SuccessFor(c, nethttp.StatusOK, responses.ServiceAuth, responses.CaseSuccess, gin.H{
			"email":           user.Email,
			"emailVerifiedAt": responses.RFC3339(user.EmailVerifiedAt),
			"sessionsRevoked": true,
		})
	}
}
