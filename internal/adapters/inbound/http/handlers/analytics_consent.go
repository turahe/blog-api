package handlers

import (
	"context"
	"errors"
	nethttp "net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/middleware"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/requests"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	consentdomain "github.com/turahe/blog-api/internal/core/consent/domain"
	consentservice "github.com/turahe/blog-api/internal/core/consent/service"
	settingsdomain "github.com/turahe/blog-api/internal/core/settings/domain"
)

// ConsentTokenHeader carries the consent subject's bearer token.
const ConsentTokenHeader = "X-Consent-Token"

// consentStorePerMinute bounds anonymous subject creation per IP.
const consentStorePerMinute = 20

type consentAPI interface {
	Store(ctx context.Context, token string, userID *uuid.UUID, decisions map[consentdomain.Purpose]bool, policyVersion string) (consentservice.State, error)
	Current(ctx context.Context, token string) (consentservice.State, error)
	Withdraw(ctx context.Context, token string, userID *uuid.UUID, consentID uuid.UUID) (consentdomain.Consent, error)
	Decision(ctx context.Context, token string, purpose consentdomain.Purpose) (consentservice.Decision, error)
}

type settingsValues interface {
	Values(ctx context.Context) (settingsdomain.Values, error)
}

// storeConsentHandler godoc
//
//	@Summary		Store analytics consent
//	@Description	Records a decision per purpose (analytics, authenticated_analytics) with the policy version the visitor saw. Send the X-Consent-Token from an earlier response to update that subject; without it (or with an unknown token) a new subject is created and its token is returned once, with 201. Refusing a granted purpose withdraws it; withdrawing analytics deletes the analytics events already stored for the subject. authenticated_analytics needs a signed-in user and granted analytics, and links the subject to that user.
//	@Tags			analytics
//	@Accept			json
//	@Produce		json
//	@Param			X-Consent-Token	header		string					false	"consent subject token"
//	@Param			body			body		requests.StoreConsent	true	"decisions"
//	@Success		200				{object}	responses.Envelope
//	@Success		201				{object}	responses.Envelope
//	@Failure		400				{object}	responses.Envelope
//	@Failure		429				{object}	responses.Envelope
//	@Router			/api/v1/analytics/consent [post]
func storeConsentHandler(consent consentAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req requests.StoreConsent
		if !requests.BindJSON(c, &req) {
			return
		}

		decisions := make(map[consentdomain.Purpose]bool, len(req.Purposes))
		for purpose, granted := range req.Purposes {
			decisions[consentdomain.Purpose(purpose)] = granted
		}

		state, err := consent.Store(c.Request.Context(), consentToken(c), optionalUserID(c), decisions, req.PolicyVersion)
		if mapConsentError(c, err) {
			return
		}

		status := nethttp.StatusOK
		if state.Token != "" {
			status = nethttp.StatusCreated
		}

		c.Header("Cache-Control", "no-store")
		responses.SuccessFor(c, status, responses.ServiceAnalytics, responses.CaseSuccess, responses.ConsentState(state))
	}
}

// getConsentHandler godoc
//
//	@Summary		Get analytics consent
//	@Description	The current decisions of the subject identified by X-Consent-Token. An unknown or missing token is 404: treat it as no consent.
//	@Tags			analytics
//	@Produce		json
//	@Param			X-Consent-Token	header		string	true	"consent subject token"
//	@Success		200				{object}	responses.Envelope
//	@Failure		404				{object}	responses.Envelope
//	@Router			/api/v1/analytics/consent [get]
func getConsentHandler(consent consentAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")

		state, err := consent.Current(c.Request.Context(), consentToken(c))
		if mapConsentError(c, err) {
			return
		}

		responses.SuccessFor(c, nethttp.StatusOK, responses.ServiceAnalytics, responses.CaseSuccess, responses.ConsentState(state))
	}
}

// withdrawConsentHandler godoc
//
//	@Summary		Withdraw analytics consent
//	@Description	Withdraws one consent. Prove ownership with the subject's X-Consent-Token, or as the signed-in user the subject is linked to; otherwise 404. Withdrawing analytics also withdraws authenticated_analytics, unlinks the user, and deletes the analytics events already stored for the subject. Withdrawing a consent that is not granted changes nothing.
//	@Tags			analytics
//	@Produce		json
//	@Param			param1			path		string	true	"consent UUID"
//	@Param			X-Consent-Token	header		string	false	"consent subject token"
//	@Success		200				{object}	responses.Envelope
//	@Failure		400				{object}	responses.Envelope
//	@Failure		404				{object}	responses.Envelope
//	@Router			/api/v1/analytics/consent/{param1} [delete]
func withdrawConsentHandler(consent consentAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(strings.TrimSpace(c.Param("param1")))
		if err != nil {
			responses.Failure(c, nethttp.StatusBadRequest, responses.ErrorCodeValidation, "Invalid consent id")
			return
		}

		withdrawn, err := consent.Withdraw(c.Request.Context(), consentToken(c), optionalUserID(c), id)
		if mapConsentError(c, err) {
			return
		}

		c.Header("Cache-Control", "no-store")
		responses.SuccessFor(c, nethttp.StatusOK, responses.ServiceAnalytics, responses.CaseSuccess, responses.Consent(withdrawn))
	}
}

// analyticsIngestGate enforces the analytics settings and consent on ingestion routes:
// with analytics.enabled off every request is 404, and with analytics.consent_required
// on the X-Consent-Token subject must have granted analytics (403 otherwise). It passes the
// granted subject, or the fact that the subject refused, on to the ingest handlers.
func analyticsIngestGate(consent consentAPI, settings settingsValues) gin.HandlerFunc {
	return func(c *gin.Context) {
		values, err := settings.Values(c.Request.Context())
		if err != nil {
			responses.Internal(c, err, "Failed to load settings")
			c.Abort()

			return
		}

		if !values.Bool("analytics.enabled") {
			responses.Failure(c, nethttp.StatusNotFound, "analytics.disabled", "Analytics is disabled")
			c.Abort()

			return
		}

		decision, err := consent.Decision(c.Request.Context(), consentToken(c), consentdomain.PurposeAnalytics)
		if err != nil {
			responses.Internal(c, err, "Failed to check consent")
			c.Abort()

			return
		}

		if values.Bool("analytics.consent_required") && !decision.Granted() {
			responses.Failure(c, nethttp.StatusForbidden, "analytics.consent_required", "Analytics consent has not been granted")
			c.Abort()

			return
		}

		switch {
		case decision.Granted():
			c.Set(contextAnalyticsSubject, decision.Subject)
		case decision.Refused():
			c.Set(contextAnalyticsRefused, true)
		}

		c.Next()
	}
}

func consentControllers(deps Deps) (store, get, withdraw, gate gin.HandlerFunc) {
	if deps.Consent == nil {
		return nil, nil, nil, nil
	}

	limit := middleware.RateLimit(deps.RateLimiter, deps.Logger, "analytics.consent.store", consentStorePerMinute, time.Minute)
	store = chain(limit, storeConsentHandler(deps.Consent))

	if deps.SettingsValues != nil {
		gate = analyticsIngestGate(deps.Consent, deps.SettingsValues)
	}

	return store, getConsentHandler(deps.Consent), withdrawConsentHandler(deps.Consent), gate
}

func consentToken(c *gin.Context) string {
	return strings.TrimSpace(c.GetHeader(ConsentTokenHeader))
}

func optionalUserID(c *gin.Context) *uuid.UUID {
	if id, ok := middleware.CurrentUserID(c); ok {
		return &id
	}

	return nil
}

func mapConsentError(c *gin.Context, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, consentdomain.ErrValidation):
		responses.Failure(c, nethttp.StatusBadRequest, responses.ErrorCodeValidation, err.Error())
	case errors.Is(err, consentdomain.ErrNotFound):
		responses.Failure(c, nethttp.StatusNotFound, responses.ErrorCodeNotFound, "Consent not found")
	default:
		responses.Internal(c, err, "Failed to process consent")
	}

	return true
}
