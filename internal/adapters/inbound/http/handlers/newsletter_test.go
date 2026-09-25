package handlers

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	nethttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/middleware"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	nldomain "github.com/turahe/blog-api/internal/core/newsletter/domain"
	nlservice "github.com/turahe/blog-api/internal/core/newsletter/service"
)

type fakeNewsletter struct {
	err         error
	subscribe   nlservice.SubscribeInput
	unsubscribe nlservice.UnsubscribeInput
	webhook     nlservice.WebhookInput
	token       string
	prefs       nlservice.Preferences
	subscribers []nldomain.Subscriber
	created     *nlservice.IssueInput
	patched     *nlservice.IssuePatch
	config      nlservice.ProviderConfig
}

func (f *fakeNewsletter) Subscribe(_ context.Context, in nlservice.SubscribeInput) error {
	f.subscribe = in
	return f.err
}

func (f *fakeNewsletter) Confirm(_ context.Context, raw, _ string) (nldomain.Subscriber, error) {
	f.token = raw
	return f.prefs.Subscriber, f.err
}

func (f *fakeNewsletter) ResendConfirm(context.Context, string, string) error { return f.err }

func (f *fakeNewsletter) Unsubscribe(_ context.Context, in nlservice.UnsubscribeInput) error {
	f.unsubscribe = in
	return f.err
}

func (f *fakeNewsletter) Preferences(_ context.Context, raw string) (nlservice.Preferences, error) {
	f.token = raw
	return f.prefs, f.err
}

func (f *fakeNewsletter) UpdatePreferences(_ context.Context, raw string, _ nlservice.PreferencesInput) (nlservice.Preferences, error) {
	f.token = raw
	return f.prefs, f.err
}

func (f *fakeNewsletter) ProviderWebhook(_ context.Context, in nlservice.WebhookInput) (int, error) {
	f.webhook = in
	return 1, f.err
}

func (f *fakeNewsletter) MySubscription(context.Context, uuid.UUID) (nlservice.Preferences, bool, error) {
	return f.prefs, f.prefs.Subscriber.UUID != uuid.Nil, f.err
}

func (f *fakeNewsletter) MySubscribe(context.Context, nlservice.MySubscribeInput) (nlservice.Preferences, error) {
	return f.prefs, f.err
}

func (f *fakeNewsletter) MyUnsubscribe(context.Context, uuid.UUID, []string, string, string, string) (nlservice.Preferences, error) {
	return f.prefs, f.err
}

func (f *fakeNewsletter) ListSubscribers(_ context.Context, filter nldomain.SubscriberFilter) (nldomain.SubscriberPage, error) {
	start := min((filter.Page-1)*filter.PerPage, len(f.subscribers))
	end := min(start+filter.PerPage, len(f.subscribers))

	return nldomain.SubscriberPage{
		Items: f.subscribers[start:end], Page: filter.Page, PerPage: filter.PerPage, Total: int64(len(f.subscribers)),
	}, f.err
}

func (f *fakeNewsletter) Subscriber(context.Context, uuid.UUID) (nlservice.SubscriberDetail, error) {
	return nlservice.SubscriberDetail{Subscriber: f.prefs.Subscriber}, f.err
}

func (f *fakeNewsletter) DeleteSubscriber(context.Context, uuid.UUID, uuid.UUID, string) (nlservice.SubscriberDetail, error) {
	return nlservice.SubscriberDetail{Subscriber: f.prefs.Subscriber}, f.err
}

func (f *fakeNewsletter) CreateIssue(_ context.Context, _ uuid.UUID, in nlservice.IssueInput) (nldomain.Issue, error) {
	f.created = &in
	return nldomain.Issue{UUID: uuid.New(), Subject: in.Subject, Status: in.Status}, f.err
}

func (f *fakeNewsletter) UpdateIssue(_ context.Context, _, id uuid.UUID, patch nlservice.IssuePatch) (nldomain.Issue, error) {
	f.patched = &patch
	return nldomain.Issue{UUID: id}, f.err
}

func (f *fakeNewsletter) Issue(_ context.Context, id uuid.UUID) (nldomain.Issue, error) {
	return nldomain.Issue{UUID: id, BodyMarkdown: "Body"}, f.err
}

func (f *fakeNewsletter) ListIssues(context.Context, nldomain.IssueFilter) (nldomain.IssuePage, error) {
	return nldomain.IssuePage{}, f.err
}

func (f *fakeNewsletter) Preview(context.Context, nldomain.Issue) (string, string, error) {
	return "<p>Body</p>", "Body", f.err
}

func (f *fakeNewsletter) ProviderConfig(context.Context) (nlservice.ProviderConfig, error) {
	return f.config, f.err
}

func (f *fakeNewsletter) SaveProviderConfig(
	_ context.Context, _ uuid.UUID, cfg nldomain.Config, lists []nldomain.ListInput,
) (nlservice.ProviderConfig, error) {
	f.config.Config = cfg
	for _, l := range lists {
		f.config.Lists = append(f.config.Lists, nldomain.List{Slug: l.Slug, Name: l.Name, IsDefault: l.IsDefault})
	}

	return f.config, f.err
}

type nlRequest struct {
	method, target, body, contentType string
	param                             string
	signedIn                          bool
}

func runNewsletter(t *testing.T, handler gin.HandlerFunc, r nlRequest) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequestWithContext(t.Context(), r.method, r.target, strings.NewReader(r.body))

	contentType := r.contentType
	if contentType == "" {
		contentType = "application/json"
	}

	c.Request.Header.Set("Content-Type", contentType)

	if r.param != "" {
		c.Params = gin.Params{{Key: "param1", Value: r.param}}
	}

	if r.signedIn {
		c.Set(middleware.ContextUserIDKey, testUserID)
	}

	handler(c)

	return w
}

func envelopeOf(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()

	var out map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out), w.Body.String())

	return out
}

func errorCodeOf(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()

	errObj, _ := envelopeOf(t, w)["error"].(map[string]any)
	code, _ := errObj["code"].(string)

	return code
}

func TestNewsletterSubscribeAlwaysAccepts(t *testing.T) {
	t.Parallel()

	fake := &fakeNewsletter{}
	w := runNewsletter(t, newsletterSubscribeHandler(fake), nlRequest{
		method: nethttp.MethodPost, target: "/api/v1/newsletter/subscribe",
		body: `{"email":"reader@example.test","lists":["weekly"],"format":"plaintext","turnstile_response":"tok"}`,
	})

	require.Equal(t, nethttp.StatusAccepted, w.Code)
	assert.Equal(t, "pending_confirmation", dataOf(envelopeOf(t, w))["status"])
	assert.Equal(t, "reader@example.test", fake.subscribe.Email)
	assert.Equal(t, []string{"weekly"}, fake.subscribe.Lists)
	assert.Equal(t, "tok", fake.subscribe.CaptchaToken)

	w = runNewsletter(t, newsletterSubscribeHandler(fake), nlRequest{
		method: nethttp.MethodPost, target: "/", body: `{"email":"reader@example.test","format":"pdf"}`,
	})
	require.Equal(t, nethttp.StatusBadRequest, w.Code)
}

func TestNewsletterErrorMapping(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		err    error
		status int
		code   string
	}{
		"invalid":        {nldomain.ErrTokenInvalid, nethttp.StatusNotFound, "newsletter.token_invalid"},
		"expired":        {nldomain.ErrTokenExpired, nethttp.StatusGone, "newsletter.token_expired"},
		"used":           {nldomain.ErrTokenUsed, nethttp.StatusConflict, "newsletter.token_used"},
		"captcha":        {nldomain.ErrCaptcha, nethttp.StatusBadRequest, "newsletter.captcha_failed"},
		"not configured": {nldomain.ErrNotConfigured, nethttp.StatusUnprocessableEntity, "newsletter.not_configured"},
		"validation":     {nldomain.Invalid("bad"), nethttp.StatusBadRequest, responses.ErrorCodeValidation},
		"unexpected":     {errors.New("db down"), nethttp.StatusInternalServerError, responses.ErrorCodeInternal},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			w := runNewsletter(t, newsletterConfirmHandler(&fakeNewsletter{err: tc.err}), nlRequest{
				method: nethttp.MethodPost, target: "/api/v1/newsletter/confirm", body: `{"token":"raw"}`,
			})
			require.Equal(t, tc.status, w.Code)
			require.Equal(t, tc.code, errorCodeOf(t, w))
		})
	}
}

func TestNewsletterUnsubscribeOneClick(t *testing.T) {
	t.Parallel()

	fake := &fakeNewsletter{}
	w := runNewsletter(t, newsletterUnsubscribeHandler(fake), nlRequest{
		method: nethttp.MethodPost, target: "/api/v1/newsletter/unsubscribe?token=from-query",
		body: "List-Unsubscribe=One-Click", contentType: "application/x-www-form-urlencoded",
	})
	require.Equal(t, nethttp.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "from-query", fake.unsubscribe.Token)

	w = runNewsletter(t, newsletterUnsubscribeHandler(fake), nlRequest{
		method: nethttp.MethodPost, target: "/api/v1/newsletter/unsubscribe",
		body: `{"token":"from-body","reason_code":"not_relevant","feedback":"thanks"}`,
	})
	require.Equal(t, nethttp.StatusOK, w.Code)
	assert.Equal(t, "from-body", fake.unsubscribe.Token)
	assert.Equal(t, "not_relevant", fake.unsubscribe.ReasonCode)

	w = runNewsletter(t, newsletterUnsubscribeHandler(fake), nlRequest{
		method: nethttp.MethodPost, target: "/api/v1/newsletter/unsubscribe", body: `{}`,
	})
	require.Equal(t, nethttp.StatusBadRequest, w.Code)
}

func TestNewsletterPreferencesMasksAddress(t *testing.T) {
	t.Parallel()

	fake := &fakeNewsletter{prefs: nlservice.Preferences{
		Subscriber: nldomain.Subscriber{UUID: uuid.New(), Email: "reader@example.test", Status: nldomain.StatusActive},
		Lists:      []nldomain.List{{Slug: "weekly", Name: "Weekly", IsDefault: true}},
	}}
	w := runNewsletter(t, newsletterPreferencesHandler(fake), nlRequest{
		method: nethttp.MethodGet, target: "/api/v1/newsletter/preferences/tok", param: "tok",
	})

	require.Equal(t, nethttp.StatusOK, w.Code)
	assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	assert.Equal(t, "tok", fake.token)

	data := dataOf(envelopeOf(t, w))
	assert.Equal(t, "r*****@example.test", data["email"])
	assert.NotContains(t, w.Body.String(), "reader@example.test")
}

func TestNewsletterWebhookPassesSignedBody(t *testing.T) {
	t.Parallel()

	fake := &fakeNewsletter{}

	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequestWithContext(t.Context(), nethttp.MethodPost, "/", strings.NewReader(`{"events":[]}`))
	c.Request.Header.Set("X-Newsletter-Timestamp", "123")
	c.Request.Header.Set("X-Newsletter-Signature", "v1=abc")
	newsletterWebhookHandler(fake)(c)

	require.Equal(t, nethttp.StatusOK, w.Code)
	assert.Equal(t, "123", fake.webhook.Timestamp)
	assert.Equal(t, "v1=abc", fake.webhook.Signature)
	assert.JSONEq(t, `{"events":[]}`, string(fake.webhook.Body))

	w = runNewsletter(t, newsletterWebhookHandler(&fakeNewsletter{err: nldomain.ErrSignature}), nlRequest{
		method: nethttp.MethodPost, target: "/", body: `{}`,
	})
	require.Equal(t, nethttp.StatusUnauthorized, w.Code)
	require.Equal(t, "newsletter.signature_invalid", errorCodeOf(t, w))
}

func TestNewsletterSubscribersCSVExport(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	fake := &fakeNewsletter{}

	for i := range newsletterExportPage + 1 {
		fake.subscribers = append(fake.subscribers, nldomain.Subscriber{
			UUID: uuid.New(), Email: "reader@example.test", DisplayName: "=HYPERLINK(\"x\")",
			Status: nldomain.StatusActive, CreatedAt: now.Add(time.Duration(i) * time.Second),
		})
	}

	allow := func(*gin.Context) bool { return true }
	w := runNewsletter(t, adminNewsletterSubscribersHandler(fake, allow), nlRequest{
		method: nethttp.MethodGet, target: "/api/v1/admin/newsletter/subscribers?format=csv", signedIn: true,
	})

	require.Equal(t, nethttp.StatusOK, w.Code)
	require.True(t, strings.HasPrefix(w.Header().Get("Content-Type"), "text/csv"))

	rows, err := csv.NewReader(strings.NewReader(w.Body.String())).ReadAll()
	require.NoError(t, err)
	require.Len(t, rows, newsletterExportPage+2, "header plus every page")
	assert.Equal(t, `'=HYPERLINK("x")`, rows[1][2], "formula cells are neutralised")

	deny := func(*gin.Context) bool { return false }
	w = runNewsletter(t, adminNewsletterSubscribersHandler(fake, deny), nlRequest{
		method: nethttp.MethodGet, target: "/api/v1/admin/newsletter/subscribers?format=csv", signedIn: true,
	})
	require.Equal(t, nethttp.StatusForbidden, w.Code)
}

func TestNewsletterIssueSendNeedsPermission(t *testing.T) {
	t.Parallel()

	body := `{"subject":"Hello","body_markdown":"Body","lists":["weekly"],"status":"queued"}`
	deny := func(*gin.Context) bool { return false }

	fake := &fakeNewsletter{}
	w := runNewsletter(t, adminNewsletterCreateIssueHandler(fake, deny), nlRequest{
		method: nethttp.MethodPost, target: "/api/v1/admin/newsletter/issues", body: body, signedIn: true,
	})
	require.Equal(t, nethttp.StatusForbidden, w.Code)
	require.Nil(t, fake.created, "the service is not called")

	w = runNewsletter(t, adminNewsletterCreateIssueHandler(fake, deny), nlRequest{
		method: nethttp.MethodPost, target: "/api/v1/admin/newsletter/issues", signedIn: true,
		body: `{"subject":"Hello","body_markdown":"Body","lists":["weekly"]}`,
	})
	require.Equal(t, nethttp.StatusCreated, w.Code, "drafts need only issues.edit")

	w = runNewsletter(t, adminNewsletterUpdateIssueHandler(fake, deny), nlRequest{
		method: nethttp.MethodPatch, target: "/", param: uuid.NewString(), body: `{"status":"cancelled"}`, signedIn: true,
	})
	require.Equal(t, nethttp.StatusOK, w.Code, "cancelling needs only issues.edit")
	require.NotNil(t, fake.patched)
	require.Equal(t, nldomain.IssueCancelled, *fake.patched.Status)

	allow := func(*gin.Context) bool { return true }
	w = runNewsletter(t, adminNewsletterCreateIssueHandler(fake, allow), nlRequest{
		method: nethttp.MethodPost, target: "/api/v1/admin/newsletter/issues", body: body, signedIn: true,
	})
	require.Equal(t, nethttp.StatusCreated, w.Code)
	require.Equal(t, nldomain.IssueQueued, fake.created.Status)
}

func TestNewsletterProviderConfigHidesSecret(t *testing.T) {
	t.Parallel()

	fake := &fakeNewsletter{config: nlservice.ProviderConfig{Config: nldomain.DefaultConfig()}}
	provider := responses.NewsletterProvider{Name: "custom_http", Endpoint: "https://gw.test/send", SecretConfigured: true}

	w := runNewsletter(t, adminNewsletterConfigHandler(fake, provider), nlRequest{
		method: nethttp.MethodGet, target: "/api/v1/admin/newsletter/provider-config", signedIn: true,
	})
	require.Equal(t, nethttp.StatusOK, w.Code)

	data := dataOf(envelopeOf(t, w))
	assert.Equal(t, false, data["ready_to_send"])
	assert.InDelta(t, 48, data["confirm_ttl_hours"], 0)
	assert.Equal(t, true, objectOf(t, data["provider"])["secret_configured"])

	w = runNewsletter(t, adminNewsletterSaveConfigHandler(fake, provider), nlRequest{
		method: nethttp.MethodPut, target: "/api/v1/admin/newsletter/provider-config", signedIn: true,
		body: `{"postal_address":"1 Example Street","confirm_ttl_hours":24,"double_optin_required":false,` +
			`"lists":[{"slug":"weekly","name":"Weekly","is_default":true}]}`,
	})
	require.Equal(t, nethttp.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, 24*time.Hour, fake.config.Config.ConfirmTTL)
	assert.False(t, fake.config.Config.DoubleOptInRequired)
	assert.Equal(t, true, dataOf(envelopeOf(t, w))["ready_to_send"])

	w = runNewsletter(t, adminNewsletterSaveConfigHandler(fake, provider), nlRequest{
		method: nethttp.MethodPut, target: "/", signedIn: true, body: `{"confirm_ttl_hours":24,"lists":[]}`,
	})
	require.Equal(t, nethttp.StatusBadRequest, w.Code, "double_optin_required and a list are required")
}

func TestMyNewsletterWithoutSubscription(t *testing.T) {
	t.Parallel()

	fake := &fakeNewsletter{prefs: nlservice.Preferences{Lists: []nldomain.List{{Slug: "weekly", Name: "Weekly"}}}}
	w := runNewsletter(t, meNewsletterHandler(fake), nlRequest{method: nethttp.MethodGet, target: "/", signedIn: true})

	require.Equal(t, nethttp.StatusOK, w.Code)

	data := dataOf(envelopeOf(t, w))
	assert.Equal(t, false, data["subscribed"])
	assert.Len(t, data["available_lists"], 1)
}
