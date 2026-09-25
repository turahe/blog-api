package handlers

import (
	"context"
	nethttp "net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/requests"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
	authservice "github.com/turahe/blog-api/internal/core/auth/service"
)

// oauthAPI is the social login API consumed by the /auth/oauth handlers.
type oauthAPI interface {
	OAuthStart(ctx context.Context, provider, redirectURI string) (authdomain.OAuthStart, error)
	OAuthCallback(ctx context.Context, provider, code, state, userAgent, ip string) (authdomain.LoginResult, error)
}

// oauthStartHandler godoc
//
//	@Summary		Start an OAuth sign-in
//	@Description	Returns the provider authorization URL (with state and a PKCE challenge) for the
//	@Description	browser to visit. redirect_uri must be listed in OAUTH_REDIRECT_URIS. The state
//	@Description	is single use and expires after 10 minutes.
//	@Tags			auth
//	@Produce		json
//	@Param			provider		path		string	true	"google or github"
//	@Param			redirect_uri	query		string	true	"registered client callback URL"
//	@Success		200				{object}	responses.Envelope
//	@Failure		400				{object}	responses.Envelope
//	@Failure		404				{object}	responses.Envelope
//	@Failure		429				{object}	responses.Envelope
//	@Router			/api/v1/auth/oauth/{provider}/start [get]
func oauthStartHandler(api oauthAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		redirectURI := strings.TrimSpace(c.Query("redirect_uri"))
		if redirectURI == "" {
			responses.Failure(c, nethttp.StatusBadRequest, responses.ErrorCodeValidation, "redirect_uri is required")
			return
		}

		start, err := api.OAuthStart(c.Request.Context(), c.Param("param1"), redirectURI)
		if err != nil {
			writeAuthError(c, err, authservice.MapError)
			return
		}

		c.Header("Cache-Control", "no-store")
		responses.SuccessFor(c, nethttp.StatusOK, responses.ServiceAuth, responses.CaseSuccess, gin.H{
			"authorize_url": start.AuthorizeURL,
			"expires_at":    start.ExpiresAt.UTC().Format(time.RFC3339),
		})
	}
}

// oauthCallbackHandler godoc
//
//	@Summary		Complete an OAuth sign-in
//	@Description	Exchanges the code and state the provider sent to the client's redirect_uri. Signs in
//	@Description	the account linked to the provider identity, or the existing account whose email the
//	@Description	provider verified (the identity is then linked). No account is ever created.
//	@Description	Responds like POST /api/v1/auth/login: a token pair or a two-factor challenge.
//	@Tags			auth
//	@Accept			json
//	@Produce		json
//	@Param			provider	path		string					true	"google or github"
//	@Param			body		body		requests.OAuthCallback	true	"code and state"
//	@Success		200			{object}	responses.Envelope
//	@Failure		400			{object}	responses.Envelope
//	@Failure		401			{object}	responses.Envelope
//	@Failure		404			{object}	responses.Envelope
//	@Failure		409			{object}	responses.Envelope
//	@Failure		429			{object}	responses.Envelope
//	@Router			/api/v1/auth/oauth/{provider}/callback [post]
func oauthCallbackHandler(api oauthAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req requests.OAuthCallback
		if !requests.BindJSON(c, &req) {
			return
		}

		result, err := api.OAuthCallback(c.Request.Context(), c.Param("param1"), req.Code, req.State,
			c.Request.UserAgent(), c.ClientIP())
		writeLoginResult(c, result, err)
	}
}
