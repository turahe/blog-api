package handlers

import (
	"context"
	nethttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	consentdomain "github.com/turahe/blog-api/internal/core/consent/domain"
	consentservice "github.com/turahe/blog-api/internal/core/consent/service"
	settingsdomain "github.com/turahe/blog-api/internal/core/settings/domain"
)

type fakeConsent struct {
	token     string
	user      *uuid.UUID
	decisions map[consentdomain.Purpose]bool
	version   string
	newToken  string
	allowed   bool
	err       error
}

func (f *fakeConsent) Store(
	_ context.Context, token string, user *uuid.UUID, decisions map[consentdomain.Purpose]bool, version string,
) (consentservice.State, error) {
	f.token, f.user, f.decisions, f.version = token, user, decisions, version

	return consentservice.State{Subject: consentdomain.Subject{UUID: uuid.New()}, Token: f.newToken, Consents: []consentdomain.Consent{
		{UUID: uuid.New(), Purpose: consentdomain.PurposeAnalytics, Status: consentdomain.StatusGranted, PolicyVersion: version},
	}}, f.err
}

func (f *fakeConsent) Current(_ context.Context, token string) (consentservice.State, error) {
	f.token = token
	return consentservice.State{Subject: consentdomain.Subject{UUID: uuid.New()}}, f.err
}

func (f *fakeConsent) Withdraw(_ context.Context, token string, user *uuid.UUID, id uuid.UUID) (consentdomain.Consent, error) {
	f.token, f.user = token, user
	return consentdomain.Consent{UUID: id, Status: consentdomain.StatusWithdrawn}, f.err
}

func (f *fakeConsent) Allowed(_ context.Context, token string, _ consentdomain.Purpose) (bool, error) {
	f.token = token
	return f.allowed, f.err
}

type fakeSettingsValues map[string]any

func (f fakeSettingsValues) Values(context.Context) (settingsdomain.Values, error) {
	return settingsdomain.NewValues(f), nil
}

func withConsentToken(handler gin.HandlerFunc, token string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request.Header.Set(ConsentTokenHeader, token)
		handler(c)
	}
}

func TestStoreConsentCreatesOrUpdates(t *testing.T) {
	t.Parallel()

	svc := &fakeConsent{newToken: "secret"}

	w, body := runProfile(t, storeConsentHandler(svc), profileRequest{
		method: nethttp.MethodPost, target: "/", contentType: "application/json", user: &testUserID,
		body: `{"purposes":{"analytics":true,"authenticated_analytics":false},"policy_version":"2026-09"}`,
	})
	require.Equal(t, nethttp.StatusCreated, w.Code, w.Body.String())
	assert.Equal(t, "secret", dataOf(body)["token"])
	assert.Equal(t, "no-store", w.Header().Get("Cache-Control"), "the response carries the subject token")
	assert.Equal(t, map[consentdomain.Purpose]bool{
		consentdomain.PurposeAnalytics: true, consentdomain.PurposeAuthenticatedAnalytics: false,
	}, svc.decisions)
	assert.Equal(t, "2026-09", svc.version)
	assert.Equal(t, &testUserID, svc.user)

	svc.newToken = ""
	w, body = runProfile(t, withConsentToken(storeConsentHandler(svc), "existing"), profileRequest{
		method: nethttp.MethodPost, target: "/", contentType: "application/json",
		body: `{"purposes":{"analytics":false},"policy_version":"2026-09"}`,
	})
	require.Equal(t, nethttp.StatusOK, w.Code)
	assert.Equal(t, "existing", svc.token)
	assert.Nil(t, svc.user)
	assert.NotContains(t, dataOf(body), "token")

	svc.err = consentdomain.ErrValidation
	w, _ = runProfile(t, storeConsentHandler(svc), profileRequest{
		method: nethttp.MethodPost, target: "/", contentType: "application/json",
		body: `{"purposes":{"x":true},"policy_version":"v"}`,
	})
	assert.Equal(t, nethttp.StatusBadRequest, w.Code)
}

func TestGetAndWithdrawConsent(t *testing.T) {
	t.Parallel()

	svc := &fakeConsent{}

	w, _ := runProfile(t, withConsentToken(getConsentHandler(svc), "tok"), profileRequest{method: nethttp.MethodGet, target: "/"})
	require.Equal(t, nethttp.StatusOK, w.Code)
	assert.Equal(t, "tok", svc.token)
	assert.Equal(t, "no-store", w.Header().Get("Cache-Control"), "state is keyed by a header shared caches ignore")

	id := uuid.New()
	w, body := runProfile(t, withConsentToken(withdrawConsentHandler(svc), "tok"), profileRequest{
		method: nethttp.MethodDelete, target: "/", param: id.String(),
	})
	require.Equal(t, nethttp.StatusOK, w.Code)
	assert.Equal(t, "withdrawn", dataOf(body)["status"])

	w, _ = runProfile(t, withdrawConsentHandler(svc), profileRequest{method: nethttp.MethodDelete, target: "/", param: "nope"})
	assert.Equal(t, nethttp.StatusBadRequest, w.Code)

	svc.err = consentdomain.ErrNotFound
	w, body = runProfile(t, getConsentHandler(svc), profileRequest{method: nethttp.MethodGet, target: "/"})
	assert.Equal(t, nethttp.StatusNotFound, w.Code)
	assert.Equal(t, responses.ErrorCodeNotFound, errorCode(body))
}

func TestAnalyticsIngestGate(t *testing.T) {
	t.Parallel()

	next := func(c *gin.Context) { c.String(nethttp.StatusAccepted, "next") }
	run := func(svc *fakeConsent, settings fakeSettingsValues, token string) *httptest.ResponseRecorder {
		gate := analyticsIngestGate(svc, settings)

		return runRaw(t, func(c *gin.Context) {
			c.Request.Header.Set(ConsentTokenHeader, token)
			gate(c)

			if !c.IsAborted() {
				next(c)
			}
		}, profileRequest{method: nethttp.MethodPost, target: "/"})
	}

	enabled := fakeSettingsValues{"analytics.enabled": true, "analytics.consent_required": true}

	assert.Equal(t, nethttp.StatusNotFound, run(&fakeConsent{allowed: true}, fakeSettingsValues{}, "tok").Code)
	assert.Equal(t, nethttp.StatusForbidden, run(&fakeConsent{}, enabled, "tok").Code)

	svc := &fakeConsent{allowed: true}
	assert.Equal(t, nethttp.StatusAccepted, run(svc, enabled, "tok").Code)
	assert.Equal(t, "tok", svc.token)

	optional := fakeSettingsValues{"analytics.enabled": true, "analytics.consent_required": false}
	assert.Equal(t, nethttp.StatusAccepted, run(&fakeConsent{}, optional, "").Code)
}
