package handlers

import (
	nethttp "net/http"

	"github.com/gin-gonic/gin"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/requests"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
	authports "github.com/turahe/blog-api/internal/core/auth/ports"
	authservice "github.com/turahe/blog-api/internal/core/auth/service"
)

// registerHandler godoc
//
//	@Summary		Register an account
//	@Description	Starts a sign-up while the `security.registration_enabled` setting is on and emails a
//	@Description	verification token, valid for 24 hours, to complete at POST /api/v1/auth/verify-email. The answer
//	@Description	is the same `202` whether or not the email already has an account; that account is
//	@Description	emailed about the attempt instead. No account exists, and the username stays free,
//	@Description	until the email is verified. At most 3 unexpired sign-ups per address are kept; further
//	@Description	attempts send nothing. Limited to 10 requests per hour per client IP.
//	@Tags			auth
//	@Accept			json
//	@Produce		json
//	@Param			body	body		requests.Register	true	"sign-up"
//	@Success		202		{object}	responses.Envelope
//	@Failure		400		{object}	responses.Envelope	"validation_error"
//	@Failure		403		{object}	responses.Envelope	"auth.registration.closed"
//	@Failure		409		{object}	responses.Envelope	"user.username.taken"
//	@Failure		422		{object}	responses.Envelope	"password.strength"
//	@Failure		429		{object}	responses.Envelope	"rate limited; see Retry-After"
//	@Router			/api/v1/auth/register [post]
func registerHandler(registrar authports.Registrar) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req requests.Register
		if !requests.BindJSON(c, &req) {
			return
		}

		err := registrar.Register(c.Request.Context(), authdomain.SignUp{
			Email: req.Email, Username: req.Username, FullName: req.FullName, Password: req.Password,
		})
		if err != nil {
			writeAuthError(c, err, authservice.MapError)

			return
		}

		responses.SuccessFor(c, nethttp.StatusAccepted, responses.ServiceAuth, responses.CaseAccepted, gin.H{})
	}
}

// verifyEmailHandler godoc
//
//	@Summary		Verify email and activate a registered account
//	@Description	Exchanges the emailed sign-up token, together with the password chosen at sign-up, for an
//	@Description	active account with a verified email, and signs it in (same token pair as login, with a
//	@Description	7-day session). Requiring the password means neither someone who registered another
//	@Description	person's address nor the owner of an address someone else registered can activate the
//	@Description	account alone. A wrong password counts toward the login lockout. Using a token removes
//	@Description	every other pending sign-up for the address.
//	@Tags			auth
//	@Accept			json
//	@Produce		json
//	@Param			body	body		requests.VerifyEmail	true	"token and password"
//	@Success		201		{object}	responses.Envelope
//	@Failure		400		{object}	responses.Envelope	"auth.registration.token_invalid, auth.registration.token_expired"
//	@Failure		401		{object}	responses.Envelope	"wrong password"
//	@Failure		403		{object}	responses.Envelope	"auth.registration.closed"
//	@Failure		409		{object}	responses.Envelope	"auth.email.taken, user.username.taken"
//	@Failure		429		{object}	responses.Envelope	"rate limited or locked; see Retry-After"
//	@Router			/api/v1/auth/verify-email [post]
func verifyEmailHandler(registrar authports.Registrar) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req requests.VerifyEmail
		if !requests.BindJSON(c, &req) {
			return
		}

		pair, err := registrar.VerifyEmail(c.Request.Context(), req.Token, req.Password, c.Request.UserAgent(), c.ClientIP())
		if err != nil {
			setLockedRetryAfter(c, err)
			writeAuthError(c, err, authservice.MapError)

			return
		}

		responses.SuccessFor(c, nethttp.StatusCreated, responses.ServiceAuth, responses.CaseSuccess, responses.TokenPair(pair))
	}
}
