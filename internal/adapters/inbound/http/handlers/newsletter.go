package handlers

import (
	"context"
	"errors"
	"io"
	nethttp "net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/middleware"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/requests"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	"github.com/turahe/blog-api/internal/adapters/inbound/routes"
	nldomain "github.com/turahe/blog-api/internal/core/newsletter/domain"
	nlservice "github.com/turahe/blog-api/internal/core/newsletter/service"
)

// Per-caller budgets for the public newsletter routes. The service also limits confirmation
// emails per address, so a spread-out attack cannot flood one inbox.
const (
	newsletterSubscribePer5Min = 5
	newsletterResendPer5Min    = 5
	newsletterConfirmPerMinute = 20
	newsletterTokenPerMinute   = 30
	newsletterWebhookPerMinute = 120
	newsletterMePerMinute      = 10
	newsletterWebhookMaxBytes  = 1 << 20
)

type newsletterPublicAPI interface {
	Subscribe(ctx context.Context, in nlservice.SubscribeInput) error
	Confirm(ctx context.Context, raw, ip string) (nldomain.Subscriber, error)
	ResendConfirm(ctx context.Context, email, ip string) error
	Unsubscribe(ctx context.Context, in nlservice.UnsubscribeInput) error
	Preferences(ctx context.Context, raw string) (nlservice.Preferences, error)
	UpdatePreferences(ctx context.Context, raw string, in nlservice.PreferencesInput) (nlservice.Preferences, error)
	ProviderWebhook(ctx context.Context, in nlservice.WebhookInput) (int, error)
	MySubscription(ctx context.Context, userID uuid.UUID) (nlservice.Preferences, bool, error)
	MySubscribe(ctx context.Context, in nlservice.MySubscribeInput) (nlservice.Preferences, error)
	MyUnsubscribe(ctx context.Context, userID uuid.UUID, lists []string, reason, feedback, ip string) (nlservice.Preferences, error)
}

type newsletterAPI interface {
	newsletterPublicAPI
	newsletterAdminAPI
}

// newsletterControllers wires the newsletter handlers when the service is present.
func newsletterControllers(deps Deps) routes.Newsletter {
	nl := deps.Newsletter
	if nl == nil {
		return routes.Newsletter{}
	}

	limit := func(bucket string, n int, window time.Duration, handler gin.HandlerFunc) gin.HandlerFunc {
		return chain(middleware.RateLimit(deps.RateLimiter, deps.Logger, bucket, n, window), handler)
	}
	fiveMin := 5 * time.Minute

	c := routes.Newsletter{
		Subscribe:        limit("newsletter.subscribe", newsletterSubscribePer5Min, fiveMin, newsletterSubscribeHandler(nl)),
		Confirm:          limit("newsletter.confirm", newsletterConfirmPerMinute, time.Minute, newsletterConfirmHandler(nl)),
		ConfirmResend:    limit("newsletter.resend", newsletterResendPer5Min, fiveMin, newsletterResendHandler(nl)),
		Unsubscribe:      limit("newsletter.unsubscribe", newsletterTokenPerMinute, time.Minute, newsletterUnsubscribeHandler(nl)),
		PreferencesGet:   limit("newsletter.preferences", newsletterTokenPerMinute, time.Minute, newsletterPreferencesHandler(nl)),
		PreferencesPatch: limit("newsletter.preferences", newsletterTokenPerMinute, time.Minute, newsletterUpdatePreferencesHandler(nl)),
		ProviderWebhook:  limit("newsletter.webhook", newsletterWebhookPerMinute, time.Minute, newsletterWebhookHandler(nl)),
		MeSubscriptions:  meNewsletterHandler(nl),
		MeSubscribe:      limit("newsletter.me", newsletterMePerMinute, time.Minute, meNewsletterSubscribeHandler(nl)),
		MeUnsubscribe:    limit("newsletter.me", newsletterMePerMinute, time.Minute, meNewsletterUnsubscribeHandler(nl)),
	}
	wireNewsletterAdmin(&c, deps, nl)

	return c
}

// newsletterSubscribeHandler godoc
//
//	@Summary		Subscribe to the newsletter
//	@Description	Emails a double opt-in confirmation link. Always 202 with the same body whether the address
//	@Description	is new, pending, already subscribed, or suppressed, so it cannot reveal who subscribes. lists
//	@Description	are list slugs (empty means the default lists). Leave honeypot empty. turnstileResponse is
//	@Description	required when Turnstile is configured. Rate limited per IP and per address.
//	@Tags			newsletter
//	@Accept			json
//	@Produce		json
//	@Param			body	body		requests.NewsletterSubscribe	true	"subscription request"
//	@Success		202		{object}	responses.Envelope
//	@Failure		400		{object}	responses.Envelope	"validation_error, newsletter.captcha_failed"
//	@Failure		422		{object}	responses.Envelope	"newsletter.not_configured (no lists)"
//	@Failure		429		{object}	responses.Envelope
//	@Router			/api/v1/newsletter/subscribe [post]
func newsletterSubscribeHandler(nl newsletterPublicAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body requests.NewsletterSubscribe
		if !requests.BindJSON(c, &body) {
			return
		}

		err := nl.Subscribe(c.Request.Context(), nlservice.SubscribeInput{
			Email: body.Email, Name: body.DisplayName, Lists: body.Lists, Format: body.Format,
			Honeypot: body.Honeypot, CaptchaToken: body.TurnstileResponse,
			IP: c.ClientIP(), UserAgent: c.Request.UserAgent(),
		})
		if writeNewsletterError(c, err) {
			return
		}

		responses.SuccessFor(c, nethttp.StatusAccepted, responses.ServiceNewsletter, responses.CaseAccepted,
			responses.NewsletterStatus("pending_confirmation"))
	}
}

// newsletterConfirmHandler godoc
//
//	@Summary		Confirm a newsletter subscription
//	@Description	Uses the token from the confirmation email. The pending lists become active and a welcome
//	@Description	email with a preferences link is sent. A token works once.
//	@Tags			newsletter
//	@Accept			json
//	@Produce		json
//	@Param			body	body		requests.NewsletterToken	true	"confirmation token"
//	@Success		200		{object}	responses.Envelope
//	@Failure		400		{object}	responses.Envelope
//	@Failure		404		{object}	responses.Envelope	"newsletter.token_invalid"
//	@Failure		409		{object}	responses.Envelope	"newsletter.token_used"
//	@Failure		410		{object}	responses.Envelope	"newsletter.token_expired (request a new link)"
//	@Failure		429		{object}	responses.Envelope
//	@Router			/api/v1/newsletter/confirm [post]
func newsletterConfirmHandler(nl newsletterPublicAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body requests.NewsletterToken
		if !requests.BindJSON(c, &body) {
			return
		}

		sub, err := nl.Confirm(c.Request.Context(), body.Token, c.ClientIP())
		if writeNewsletterError(c, err) {
			return
		}

		responses.SuccessFor(c, nethttp.StatusOK, responses.ServiceNewsletter, responses.CaseSuccess,
			responses.NewsletterConfirmed(sub))
	}
}

// newsletterResendHandler godoc
//
//	@Summary		Resend the confirmation email
//	@Description	Sends a new confirmation link to a pending address. Always 202 with the same body, and at
//	@Description	most 3 emails per address in 30 minutes.
//	@Tags			newsletter
//	@Accept			json
//	@Produce		json
//	@Param			body	body		requests.NewsletterResend	true	"address"
//	@Success		202		{object}	responses.Envelope
//	@Failure		400		{object}	responses.Envelope
//	@Failure		429		{object}	responses.Envelope
//	@Router			/api/v1/newsletter/confirm/resend [post]
func newsletterResendHandler(nl newsletterPublicAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body requests.NewsletterResend
		if !requests.BindJSON(c, &body) {
			return
		}

		if writeNewsletterError(c, nl.ResendConfirm(c.Request.Context(), body.Email, c.ClientIP())) {
			return
		}

		responses.SuccessFor(c, nethttp.StatusAccepted, responses.ServiceNewsletter, responses.CaseAccepted,
			responses.NewsletterStatus("pending_confirmation"))
	}
}

// newsletterUnsubscribeHandler godoc
//
//	@Summary		Unsubscribe from the newsletter
//	@Description	Stops all newsletter mail for the subscriber behind an unsubscribe token (email footer).
//	@Description	Also the RFC 8058 one-click target: mail clients POST List-Unsubscribe=One-Click as a form
//	@Description	to this URL with ?token=. The token may be sent in the JSON body or the token query
//	@Description	parameter. Repeating it is harmless.
//	@Tags			newsletter
//	@Accept			json
//	@Accept			x-www-form-urlencoded
//	@Produce		json
//	@Param			token	query		string							false	"unsubscribe token (one-click)"
//	@Param			body	body		requests.NewsletterUnsubscribe	false	"token and optional feedback"
//	@Success		200		{object}	responses.Envelope
//	@Failure		400		{object}	responses.Envelope
//	@Failure		404		{object}	responses.Envelope	"newsletter.token_invalid"
//	@Failure		410		{object}	responses.Envelope	"newsletter.token_expired"
//	@Failure		429		{object}	responses.Envelope
//	@Router			/api/v1/newsletter/unsubscribe [post]
func newsletterUnsubscribeHandler(nl newsletterPublicAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body requests.NewsletterUnsubscribe

		if c.ContentType() == gin.MIMEJSON {
			if !requests.BindJSON(c, &body) {
				return
			}
		} else {
			body.Token = c.PostForm("token")
		}

		if body.Token == "" {
			body.Token = c.Query("token")
		}

		if body.Token == "" || len(body.Token) > 128 {
			newsletterValidation(c, "token is required")
			return
		}

		err := nl.Unsubscribe(c.Request.Context(), nlservice.UnsubscribeInput{
			Token: body.Token, ReasonCode: body.ReasonCode, Feedback: body.Feedback, IP: c.ClientIP(),
		})
		if writeNewsletterError(c, err) {
			return
		}

		responses.SuccessFor(c, nethttp.StatusOK, responses.ServiceNewsletter, responses.CaseSuccess,
			responses.NewsletterStatus(string(nldomain.StatusUnsubscribed)))
	}
}

// newsletterPreferencesHandler godoc
//
//	@Summary		Newsletter preferences
//	@Description	The subscription behind a preferences token (welcome email and issue footers): masked address,
//	@Description	status, format, list memberships, and the lists that can be chosen.
//	@Tags			newsletter
//	@Produce		json
//	@Param			param1	path		string	true	"preferences token"
//	@Success		200		{object}	responses.Envelope
//	@Failure		404		{object}	responses.Envelope	"newsletter.token_invalid"
//	@Failure		410		{object}	responses.Envelope	"newsletter.token_expired"
//	@Failure		429		{object}	responses.Envelope
//	@Router			/api/v1/newsletter/preferences/{param1} [get]
func newsletterPreferencesHandler(nl newsletterPublicAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		prefs, err := nl.Preferences(c.Request.Context(), c.Param("param1"))
		if writeNewsletterError(c, err) {
			return
		}

		c.Header("Cache-Control", "no-store")
		responses.SuccessFor(c, nethttp.StatusOK, responses.ServiceNewsletter, responses.CaseSuccess,
			responses.NewsletterPreferences(prefs.Subscriber, prefs.Lists, true))
	}
}

// newsletterUpdatePreferencesHandler godoc
//
//	@Summary		Update newsletter preferences
//	@Description	Changes format and lists by preferences token. lists is the complete set to receive; chosen
//	@Description	lists are active at once, including after an earlier unsubscribe. An empty lists array or
//	@Description	unsubscribeAll stops all mail.
//	@Tags			newsletter
//	@Accept			json
//	@Produce		json
//	@Param			param1	path		string							true	"preferences token"
//	@Param			body	body		requests.NewsletterPreferences	true	"changes"
//	@Success		200		{object}	responses.Envelope
//	@Failure		400		{object}	responses.Envelope
//	@Failure		404		{object}	responses.Envelope	"newsletter.token_invalid"
//	@Failure		410		{object}	responses.Envelope	"newsletter.token_expired"
//	@Failure		429		{object}	responses.Envelope
//	@Router			/api/v1/newsletter/preferences/{param1} [patch]
func newsletterUpdatePreferencesHandler(nl newsletterPublicAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body requests.NewsletterPreferences
		if !requests.BindJSON(c, &body) {
			return
		}

		prefs, err := nl.UpdatePreferences(c.Request.Context(), c.Param("param1"), nlservice.PreferencesInput{
			Format: body.Format, Lists: body.Lists, UnsubscribeAll: body.UnsubscribeAll, IP: c.ClientIP(),
		})
		if writeNewsletterError(c, err) {
			return
		}

		c.Header("Cache-Control", "no-store")
		responses.SuccessFor(c, nethttp.StatusOK, responses.ServiceNewsletter, responses.CaseSuccess,
			responses.NewsletterPreferences(prefs.Subscriber, prefs.Lists, true))
	}
}

// newsletterWebhookHandler godoc
//
//	@Summary		Provider bounce and complaint webhook
//	@Description	For the delivery provider. The body is {"events":[{"type":"hard_bounce|soft_bounce|complaint",
//	@Description	"email":"...","occurred_at":"RFC 3339"}]} (1-100 events), signed like outgoing custom_http
//	@Description	requests: X-Newsletter-Timestamp (Unix seconds, within 5 minutes) and X-Newsletter-Signature
//	@Description	v1=hex(HMAC-SHA256(NEWSLETTER_HTTP_SECRET, timestamp + "." + body)). Hard bounces and complaints
//	@Description	suppress the address. 422 when NEWSLETTER_HTTP_SECRET is not set.
//	@Tags			newsletter
//	@Accept			json
//	@Produce		json
//	@Param			X-Newsletter-Timestamp	header		string	true	"Unix seconds"
//	@Param			X-Newsletter-Signature	header		string	true	"v1=<hex HMAC-SHA256>"
//	@Success		200						{object}	responses.Envelope
//	@Failure		400						{object}	responses.Envelope
//	@Failure		401						{object}	responses.Envelope	"newsletter.signature_invalid"
//	@Failure		413						{object}	responses.Envelope
//	@Failure		422						{object}	responses.Envelope	"newsletter.not_configured"
//	@Failure		429						{object}	responses.Envelope
//	@Router			/api/v1/newsletter/webhooks/provider [post]
func newsletterWebhookHandler(nl newsletterPublicAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		body, err := io.ReadAll(nethttp.MaxBytesReader(c.Writer, c.Request.Body, newsletterWebhookMaxBytes))
		if err != nil {
			responses.FailureFor(c, nethttp.StatusRequestEntityTooLarge, responses.FailureOpts{
				Service: responses.ServiceNewsletter, Case: responses.CaseValidation,
				Code: responses.ErrorCodeValidation, Message: "Body is too large",
			})

			return
		}

		applied, err := nl.ProviderWebhook(c.Request.Context(), nlservice.WebhookInput{
			Timestamp: c.GetHeader("X-Newsletter-Timestamp"), Signature: c.GetHeader("X-Newsletter-Signature"), Body: body,
		})
		if writeNewsletterError(c, err) {
			return
		}

		responses.SuccessFor(c, nethttp.StatusOK, responses.ServiceNewsletter, responses.CaseSuccess, gin.H{"applied": applied})
	}
}

// meNewsletterHandler godoc
//
//	@Summary		My newsletter subscription
//	@Description	The subscription for the signed-in account (linked, or matching the account email) and the
//	@Description	lists that can be chosen. subscribed is false when there is none.
//	@Tags			me
//	@Produce		json
//	@Success		200	{object}	responses.Envelope
//	@Failure		401	{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/me/newsletter/subscriptions [get]
func meNewsletterHandler(nl newsletterPublicAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := currentUser(c)
		if !ok {
			return
		}

		prefs, found, err := nl.MySubscription(c.Request.Context(), userID)
		if writeNewsletterError(c, err) {
			return
		}

		writeMyNewsletter(c, prefs, found)
	}
}

// meNewsletterSubscribeHandler godoc
//
//	@Summary		Subscribe my account email
//	@Description	Subscribes the account email to lists (the default lists when empty). A verified account
//	@Description	email joins at once unless doubleOptinRequired is set; otherwise the lists stay pending
//	@Description	until the emailed confirmation link is used (status pending_confirm).
//	@Tags			me
//	@Accept			json
//	@Produce		json
//	@Param			body	body		requests.NewsletterMeSubscribe	true	"lists and format"
//	@Success		200		{object}	responses.Envelope
//	@Failure		400		{object}	responses.Envelope
//	@Failure		401		{object}	responses.Envelope
//	@Failure		422		{object}	responses.Envelope	"newsletter.not_configured"
//	@Failure		429		{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/me/newsletter/subscribe [post]
func meNewsletterSubscribeHandler(nl newsletterPublicAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := currentUser(c)
		if !ok {
			return
		}

		var body requests.NewsletterMeSubscribe
		if !requests.BindJSON(c, &body) {
			return
		}

		prefs, err := nl.MySubscribe(c.Request.Context(), nlservice.MySubscribeInput{
			UserID: userID, Lists: body.Lists, Format: body.Format, IP: c.ClientIP(), UserAgent: c.Request.UserAgent(),
		})
		if writeNewsletterError(c, err) {
			return
		}

		writeMyNewsletter(c, prefs, true)
	}
}

// meNewsletterUnsubscribeHandler godoc
//
//	@Summary		Unsubscribe my account email
//	@Description	Leaves lists; empty lists leaves every list and stops all newsletter mail.
//	@Tags			me
//	@Accept			json
//	@Produce		json
//	@Param			body	body		requests.NewsletterMeUnsubscribe	true	"lists and optional feedback"
//	@Success		200		{object}	responses.Envelope
//	@Failure		400		{object}	responses.Envelope
//	@Failure		401		{object}	responses.Envelope
//	@Failure		404		{object}	responses.Envelope	"not subscribed"
//	@Failure		429		{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/me/newsletter/unsubscribe [post]
func meNewsletterUnsubscribeHandler(nl newsletterPublicAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := currentUser(c)
		if !ok {
			return
		}

		var body requests.NewsletterMeUnsubscribe
		if !requests.BindJSON(c, &body) {
			return
		}

		prefs, err := nl.MyUnsubscribe(c.Request.Context(), userID, body.Lists, body.ReasonCode, body.Feedback, c.ClientIP())
		if writeNewsletterError(c, err) {
			return
		}

		writeMyNewsletter(c, prefs, true)
	}
}

func writeMyNewsletter(c *gin.Context, prefs nlservice.Preferences, found bool) {
	c.Header("Cache-Control", "no-store")
	responses.SuccessFor(c, nethttp.StatusOK, responses.ServiceNewsletter, responses.CaseSuccess,
		responses.NewsletterMine(prefs.Subscriber, prefs.Lists, found))
}

func newsletterValidation(c *gin.Context, message string) {
	responses.FailureFor(c, nethttp.StatusBadRequest, responses.FailureOpts{
		Service: responses.ServiceNewsletter, Case: responses.CaseValidation, Code: responses.ErrorCodeValidation, Message: message,
	})
}

// newsletterErrorRule maps a newsletter error to a response. An empty message shows the error
// text.
type newsletterErrorRule struct {
	err     error
	status  int
	caseID  int
	code    string
	message string
}

var newsletterErrorRules = []newsletterErrorRule{
	{nldomain.ErrValidation, nethttp.StatusBadRequest, responses.CaseValidation, responses.ErrorCodeValidation, ""},
	{nldomain.ErrCaptcha, nethttp.StatusBadRequest, responses.CaseValidation, "newsletter.captcha_failed", "Captcha verification failed; please try again"},
	{nldomain.ErrTokenInvalid, nethttp.StatusNotFound, responses.CaseNotFound, "newsletter.token_invalid", "This link is not valid"},
	{nldomain.ErrTokenExpired, nethttp.StatusGone, responses.CaseUnprocessable, "newsletter.token_expired", "This link has expired; request a new one"},
	{nldomain.ErrTokenUsed, nethttp.StatusConflict, responses.CaseConflict, "newsletter.token_used", "This link was already used"},
	{nldomain.ErrNotFound, nethttp.StatusNotFound, responses.CaseNotFound, responses.ErrorCodeNotFound, "Not found"},
	{nldomain.ErrConflict, nethttp.StatusConflict, responses.CaseConflict, "newsletter.conflict", "The change is not allowed in the current state"},
	{nldomain.ErrNotConfigured, nethttp.StatusUnprocessableEntity, responses.CaseUnprocessable, "newsletter.not_configured", ""},
	{nldomain.ErrSignature, nethttp.StatusUnauthorized, responses.CaseUnauthorized, "newsletter.signature_invalid", "Webhook signature is missing, stale, or wrong"},
}

func writeNewsletterError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}

	for _, rule := range newsletterErrorRules {
		if !errors.Is(err, rule.err) {
			continue
		}

		message := rule.message
		if message == "" {
			message = strings.TrimPrefix(err.Error(), nldomain.ErrValidation.Error()+": ")
		}

		responses.FailureFor(c, rule.status, responses.FailureOpts{
			Service: responses.ServiceNewsletter, Case: rule.caseID, Code: rule.code, Message: message,
		})

		return true
	}

	responses.RecordError(c, err)
	responses.FailureFor(c, nethttp.StatusInternalServerError, responses.FailureOpts{
		Service: responses.ServiceNewsletter, Case: responses.CaseInternalError,
		Code: responses.ErrorCodeInternal, Message: "Failed to process newsletter request",
	})

	return true
}
