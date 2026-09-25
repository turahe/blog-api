package handlers

import (
	"context"
	nethttp "net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/requests"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
	authservice "github.com/turahe/blog-api/internal/core/auth/service"
)

// twoFactorAPI is the two-factor API consumed by the challenge and /me/2fa handlers.
type twoFactorAPI interface {
	CompleteTwoFactor(ctx context.Context, challengeToken, code string) (authdomain.TokenPair, error)
	TwoFactorStatus(ctx context.Context, userID uuid.UUID) (authdomain.TwoFactorStatus, error)
	SetupTwoFactor(ctx context.Context, userID uuid.UUID) (authdomain.TwoFactorSetup, error)
	ConfirmTwoFactor(ctx context.Context, userID uuid.UUID, code string) ([]string, error)
	DisableTwoFactor(ctx context.Context, userID uuid.UUID, password, code string) error
	RegenerateBackupCodes(ctx context.Context, userID uuid.UUID, code string) ([]string, error)
}

func authOK(c *gin.Context, data any) {
	responses.SuccessFor(c, nethttp.StatusOK, responses.ServiceAuth, responses.CaseSuccess, data)
}

// twoFactorChallengeHandler godoc
//
//	@Summary		Complete a two-factor login
//	@Description	Exchanges the challengeToken from POST /api/v1/auth/login and a 6-digit TOTP code
//	@Description	or a backup code for a token pair. A challenge expires after 5 minutes or 5 wrong codes.
//	@Tags			auth
//	@Accept			json
//	@Produce		json
//	@Param			body	body		requests.TwoFactorChallenge	true	"challenge and code"
//	@Success		200		{object}	responses.Envelope
//	@Failure		400		{object}	responses.Envelope
//	@Failure		401		{object}	responses.Envelope
//	@Failure		429		{object}	responses.Envelope
//	@Router			/api/v1/auth/2fa/challenge [post]
func twoFactorChallengeHandler(mfa twoFactorAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req requests.TwoFactorChallenge
		if !requests.BindJSON(c, &req) {
			return
		}

		pair, err := mfa.CompleteTwoFactor(c.Request.Context(), req.ChallengeToken, req.Code)
		if err != nil {
			writeAuthError(c, err, authservice.MapError)
			return
		}

		authOK(c, responses.TokenPair(pair))
	}
}

// meTwoFactorGetHandler godoc
//
//	@Summary	Two-factor status
//	@Tags		self-service
//	@Produce	json
//	@Success	200	{object}	responses.Envelope
//	@Failure	401	{object}	responses.Envelope
//	@Security	Bearer
//	@Router		/api/v1/me/2fa [get]
func meTwoFactorGetHandler(mfa twoFactorAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := currentUser(c)
		if !ok {
			return
		}

		status, err := mfa.TwoFactorStatus(c.Request.Context(), userID)
		if err != nil {
			writeAuthError(c, err, authservice.MapError)
			return
		}

		authOK(c, responses.TwoFactorStatus(status))
	}
}

// meTwoFactorSetupHandler godoc
//
//	@Summary		Start two-factor setup
//	@Description	Returns a new TOTP secret and otpauth:// URL. Calling it again replaces an
//	@Description	unconfirmed secret; it is refused once two-factor is enabled.
//	@Tags			self-service
//	@Produce		json
//	@Success		200	{object}	responses.Envelope
//	@Failure		401	{object}	responses.Envelope
//	@Failure		409	{object}	responses.Envelope
//	@Failure		503	{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/me/2fa/setup [post]
func meTwoFactorSetupHandler(mfa twoFactorAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := currentUser(c)
		if !ok {
			return
		}

		setup, err := mfa.SetupTwoFactor(c.Request.Context(), userID)
		if err != nil {
			writeAuthError(c, err, authservice.MapError)
			return
		}

		c.Header("Cache-Control", "no-store")
		authOK(c, gin.H{"secret": setup.Secret, "otpauthUrl": setup.OTPAuthURL})
	}
}

// meTwoFactorConfirmHandler godoc
//
//	@Summary		Confirm two-factor setup
//	@Description	Enables two-factor with a code from the authenticator and returns 10 backup codes, shown only once.
//	@Tags			self-service
//	@Accept			json
//	@Produce		json
//	@Param			body	body		requests.TwoFactorCode	true	"TOTP code"
//	@Success		200		{object}	responses.Envelope
//	@Failure		401		{object}	responses.Envelope
//	@Failure		409		{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/me/2fa/confirm [post]
func meTwoFactorConfirmHandler(mfa twoFactorAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := currentUser(c)
		if !ok {
			return
		}

		var req requests.TwoFactorCode
		if !requests.BindJSON(c, &req) {
			return
		}

		codes, err := mfa.ConfirmTwoFactor(c.Request.Context(), userID, req.Code)
		if err != nil {
			writeAuthError(c, err, authservice.MapError)
			return
		}

		c.Header("Cache-Control", "no-store")
		authOK(c, gin.H{"enabled": true, "backupCodes": codes})
	}
}

// meTwoFactorDisableHandler godoc
//
//	@Summary	Disable two-factor
//	@Tags		self-service
//	@Accept		json
//	@Produce	json
//	@Param		body	body		requests.DisableTwoFactor	true	"password and a TOTP or backup code"
//	@Success	200		{object}	responses.Envelope
//	@Failure	401		{object}	responses.Envelope
//	@Failure	403		{object}	responses.Envelope
//	@Failure	409		{object}	responses.Envelope
//	@Security	Bearer
//	@Router		/api/v1/me/2fa [delete]
func meTwoFactorDisableHandler(mfa twoFactorAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := currentUser(c)
		if !ok {
			return
		}

		var req requests.DisableTwoFactor
		if !requests.BindJSON(c, &req) {
			return
		}

		if err := mfa.DisableTwoFactor(c.Request.Context(), userID, req.Password, req.Code); err != nil {
			writeAuthError(c, err, authservice.MapError)
			return
		}

		authOK(c, gin.H{"enabled": false})
	}
}

// meTwoFactorBackupCodesHandler godoc
//
//	@Summary		Regenerate backup codes
//	@Description	Replaces every backup code after checking a current TOTP code; the new codes are shown only once.
//	@Tags			self-service
//	@Accept			json
//	@Produce		json
//	@Param			body	body		requests.TwoFactorCode	true	"TOTP code"
//	@Success		200		{object}	responses.Envelope
//	@Failure		401		{object}	responses.Envelope
//	@Failure		409		{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/me/2fa/backup-codes [post]
func meTwoFactorBackupCodesHandler(mfa twoFactorAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := currentUser(c)
		if !ok {
			return
		}

		var req requests.TwoFactorCode
		if !requests.BindJSON(c, &req) {
			return
		}

		codes, err := mfa.RegenerateBackupCodes(c.Request.Context(), userID, req.Code)
		if err != nil {
			writeAuthError(c, err, authservice.MapError)
			return
		}

		c.Header("Cache-Control", "no-store")
		authOK(c, gin.H{"backupCodes": codes})
	}
}
