// Package handlers implements the Gin HTTP handlers bound by routes.Register*.
package handlers

import (
	"errors"
	"io"
	"math"
	nethttp "net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/middleware"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/requests"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
	authports "github.com/turahe/blog-api/internal/core/auth/ports"
	authservice "github.com/turahe/blog-api/internal/core/auth/service"
)

// writeAuthError writes the auth envelope chosen by mapErr, recording err for
// the access log when it maps to a server error.
func writeAuthError(c *gin.Context, err error, mapErr func(error) (code, message string, status int)) {
	code, message, status := mapErr(err)
	if status >= nethttp.StatusInternalServerError {
		responses.RecordError(c, err)
	}

	responses.FailureFor(c, status, responses.FailureOpts{
		Service: responses.ServiceAuth,
		Case:    responses.CaseCodeForStatus(status),
		Code:    code,
		Message: message,
		Details: nil,
	})
}

func setLockedRetryAfter(c *gin.Context, err error) {
	if locked, ok := errors.AsType[authdomain.LockedError](err); ok {
		c.Header("Retry-After", strconv.Itoa(max(int(math.Ceil(locked.RetryAfter.Seconds())), 1)))
	}
}

// loginHandler godoc
//
//	@Summary		Login
//	@Tags			auth
//	@Accept			json
//	@Produce		json
//	@Description	Returns a token pair, or for accounts with two-factor enabled
//	@Description	`{"two_factor_required": true, "challenge_token", "expires_at", "expires_in"}`
//	@Description	to complete at POST /api/v1/auth/2fa/challenge.
//	@Param			body	body		requests.Login	true	"credentials"
//	@Success		200		{object}	responses.Envelope
//	@Failure		400		{object}	responses.Envelope
//	@Failure		401		{object}	responses.Envelope
//	@Failure		429		{object}	responses.Envelope	"rate limited or account locked; see Retry-After"
//	@Failure		503		{object}	responses.Envelope	"two-factor account but 2FA is not configured"
//	@Router			/api/v1/auth/login [post]
func loginHandler(auth authports.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req requests.Login
		if !requests.BindJSON(c, &req) {
			return
		}

		result, err := auth.Login(c.Request.Context(), req.Email, req.Password, c.Request.UserAgent(), c.ClientIP(), req.Remember)
		if err != nil {
			setLockedRetryAfter(c, err)
			writeAuthError(c, err, authservice.MapError)

			return
		}

		if result.Challenge != nil {
			responses.SuccessFor(c, nethttp.StatusOK, responses.ServiceAuth, responses.CaseSuccess,
				responses.TwoFactorChallenge(*result.Challenge, time.Now()))

			return
		}

		responses.SuccessFor(c, nethttp.StatusOK, responses.ServiceAuth, responses.CaseSuccess, responses.TokenPair(result.Tokens))
	}
}

// refreshHandler godoc
//
//	@Summary	Refresh access token
//	@Tags		auth
//	@Accept		json
//	@Produce	json
//	@Param		body	body		requests.Refresh	true	"refresh token"
//	@Success	200		{object}	responses.Envelope
//	@Failure	401		{object}	responses.Envelope
//	@Router		/api/v1/auth/refresh [post]
func refreshHandler(auth authports.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req requests.Refresh
		if !requests.BindJSON(c, &req) {
			return
		}

		pair, err := auth.Refresh(c.Request.Context(), req.RefreshToken, c.Request.UserAgent(), c.ClientIP())
		if err != nil {
			writeAuthError(c, err, authservice.MapError)

			return
		}

		responses.SuccessFor(c, nethttp.StatusOK, responses.ServiceAuth, responses.CaseSuccess, responses.TokenPair(pair))
	}
}

// logoutHandler godoc
//
//	@Summary	Logout
//	@Tags		auth
//	@Accept		json
//	@Produce	json
//	@Param		body	body		requests.Logout	false	"optional refresh token"
//	@Success	200		{object}	responses.Envelope
//	@Failure	401		{object}	responses.Envelope
//	@Security	Bearer
//	@Router		/api/v1/auth/logout [post]
func logoutHandler(auth authports.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := middleware.CurrentUserID(c)
		if !ok {
			responses.FailureFor(c, nethttp.StatusUnauthorized, responses.FailureOpts{
				Service: responses.ServiceAuth,
				Case:    responses.CaseUnauthorized,
				Code:    responses.ErrorCodeUnauthorized,
				Message: "Authentication required",
				Details: nil,
			})

			return
		}

		var req requests.Logout
		if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
			requests.FailValidation(c, err)
			return
		}

		if err := auth.Logout(c.Request.Context(), userID, req.RefreshToken); err != nil {
			writeAuthError(c, err, authservice.MapError)

			return
		}

		responses.SuccessFor(c, nethttp.StatusOK, responses.ServiceAuth, responses.CaseSuccess, gin.H{})
	}
}

// forgotPasswordHandler godoc
//
//	@Summary	Request password reset
//	@Tags		auth
//	@Accept		json
//	@Produce	json
//	@Param		body	body		requests.ForgotPassword	true	"email or username"
//	@Success	202		{object}	responses.Envelope
//	@Failure	400		{object}	responses.Envelope
//	@Router		/api/v1/auth/password/forgot [post]
func forgotPasswordHandler(auth authports.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req requests.ForgotPassword
		if !requests.BindJSON(c, &req) {
			return
		}

		if err := auth.ForgotPassword(c.Request.Context(), req.EmailOrUsername); err != nil {
			writeAuthError(c, err, authservice.MapError)

			return
		}

		responses.SuccessFor(c, nethttp.StatusAccepted, responses.ServiceAuth, responses.CaseAccepted, gin.H{})
	}
}

// resetTokenValidityHandler godoc
//
//	@Summary	Check password reset token
//	@Tags		auth
//	@Produce	json
//	@Param		param1	path		string	true	"reset token"
//	@Success	200		{object}	responses.Envelope
//	@Failure	400		{object}	responses.Envelope
//	@Router		/api/v1/auth/password/reset/{param1} [get]
func resetTokenValidityHandler(auth authports.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := strings.TrimSpace(c.Param("param1"))

		validity, err := auth.CheckResetToken(c.Request.Context(), token)
		if err != nil {
			writeAuthError(c, err, authservice.MapResetError)

			return
		}

		responses.SuccessFor(c, nethttp.StatusOK, responses.ServiceAuth, responses.CaseSuccess, gin.H{
			"valid":      validity.Valid,
			"expires_at": validity.ExpiresAt.UTC().Format(time.RFC3339),
		})
	}
}

// resetPasswordHandler godoc
//
//	@Summary	Reset password
//	@Tags		auth
//	@Accept		json
//	@Produce	json
//	@Param		body	body		requests.ResetPassword	true	"reset payload"
//	@Success	200		{object}	responses.Envelope
//	@Failure	400		{object}	responses.Envelope
//	@Router		/api/v1/auth/password/reset [post]
func resetPasswordHandler(auth authports.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req requests.ResetPassword
		if !requests.BindJSON(c, &req) {
			return
		}

		if err := auth.ResetPassword(c.Request.Context(), req.Token, req.NewPassword, req.ConfirmPassword); err != nil {
			writeAuthError(c, err, authservice.MapResetError)

			return
		}

		responses.SuccessFor(c, nethttp.StatusOK, responses.ServiceAuth, responses.CaseSuccess, gin.H{})
	}
}

// changePasswordHandler godoc
//
//	@Summary	Change own password
//	@Tags		self-service
//	@Accept		json
//	@Produce	json
//	@Param		body	body		requests.ChangePassword	true	"password change"
//	@Success	200		{object}	responses.Envelope
//	@Failure	400		{object}	responses.Envelope
//	@Failure	401		{object}	responses.Envelope
//	@Security	Bearer
//	@Router		/api/v1/me/password [put]
func changePasswordHandler(auth authports.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := middleware.CurrentUserID(c)
		if !ok {
			responses.FailureFor(c, nethttp.StatusUnauthorized, responses.FailureOpts{
				Service: responses.ServiceAuth,
				Case:    responses.CaseUnauthorized,
				Code:    responses.ErrorCodeUnauthorized,
				Message: "Authentication required",
				Details: nil,
			})

			return
		}

		var req requests.ChangePassword
		if !requests.BindJSON(c, &req) {
			return
		}

		revokeAll := true
		if req.RevokeAllSessions != nil {
			revokeAll = *req.RevokeAllSessions
		}

		changedAt, invalidated, err := auth.ChangePassword(
			c.Request.Context(), userID, req.CurrentPassword, req.NewPassword, req.ConfirmPassword, revokeAll,
		)
		if err != nil {
			writeAuthError(c, err, authservice.MapError)

			return
		}

		responses.SuccessFor(c, nethttp.StatusOK, responses.ServiceAuth, responses.CaseSuccess, gin.H{
			"password_changed_at":  changedAt.UTC().Format(time.RFC3339),
			"sessions_invalidated": invalidated,
		})
	}
}
