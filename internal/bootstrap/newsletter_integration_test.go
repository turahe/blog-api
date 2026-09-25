package bootstrap_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	nethttp "net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	httpadapter "github.com/turahe/blog-api/internal/adapters/inbound/http"
	"github.com/turahe/blog-api/internal/adapters/outbound/markdown"
	"github.com/turahe/blog-api/internal/adapters/outbound/newslettermail"
	"github.com/turahe/blog-api/internal/adapters/outbound/newsletterprovider"
	"github.com/turahe/blog-api/internal/adapters/outbound/persistence"
	outboundrbac "github.com/turahe/blog-api/internal/adapters/outbound/rbac"
	"github.com/turahe/blog-api/internal/core/event"
	nldomain "github.com/turahe/blog-api/internal/core/newsletter/domain"
	nlports "github.com/turahe/blog-api/internal/core/newsletter/ports"
	nlservice "github.com/turahe/blog-api/internal/core/newsletter/service"
	privacyservice "github.com/turahe/blog-api/internal/core/privacy/service"
	rbacdomain "github.com/turahe/blog-api/internal/core/rbac/domain"
	"gorm.io/gorm"
)

const newsletterSecret = "newsletter-webhook-secret-0123456789"

type captured struct {
	mu       sync.Mutex
	confirms []nlports.ConfirmEmail
	welcomes []nlports.WelcomeEmail
	emails   []nlports.Email
	events   []event.Event
}

func (c *captured) SendConfirm(_ context.Context, e nlports.ConfirmEmail) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.confirms = append(c.confirms, e)

	return nil
}

func (c *captured) SendWelcome(_ context.Context, e nlports.WelcomeEmail) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.welcomes = append(c.welcomes, e)

	return nil
}

func (c *captured) Send(_ context.Context, e nlports.Email) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.emails = append(c.emails, e)

	return nil
}

func (c *captured) Record(_ context.Context, events ...event.Event) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.events = append(c.events, events...)

	return nil
}

func (c *captured) countEvents(typ string) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	n := 0

	for _, e := range c.events {
		if e.Type == typ {
			n++
		}
	}

	return n
}

type fixedLinks struct{}

func (fixedLinks) SiteURL(context.Context) string  { return "https://blog.example.test" }
func (fixedLinks) SiteName(context.Context) string { return "Example Blog" }
func (fixedLinks) APIURL() string                  { return "https://api.example.test" }

type newsletterStack struct {
	*authStack

	nl  *nlservice.Service
	out *captured
	// nlClock drives the newsletter service only, so advancing it keeps access tokens valid.
	nlClock *testClock
}

func newNewsletterStack(t *testing.T) *newsletterStack {
	t.Helper()

	out := &captured{}

	var nl *nlservice.Service

	clock := &testClock{t: time.Now().UTC()}
	s := newAuthStackWith(t, func(tx *gorm.DB, deps *httpadapter.Dependencies) {
		enforcer, err := outboundrbac.NewEnforcer(tx)
		require.NoError(t, err)

		perms := []string{
			nldomain.PermSubscribersRead, nldomain.PermSubscribersExport, nldomain.PermSubscribersErase,
			nldomain.PermIssuesRead, nldomain.PermIssuesEdit, nldomain.PermIssuesSend,
			nldomain.PermConfigRead, nldomain.PermConfigUpdate,
		}
		for _, key := range perms {
			require.NoError(t, tx.Exec("INSERT INTO permissions (key) VALUES (?) ON CONFLICT DO NOTHING", key).Error)
		}

		roles := outboundrbac.NewRoleStore(tx, enforcer)
		role := "itest_newsletter_" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
		_, err = roles.CreateRole(t.Context(), rbacdomain.Role{Name: role, Permissions: perms})
		require.NoError(t, err)

		var staff uuid.UUID
		require.NoError(t, tx.Raw("SELECT uuid FROM users ORDER BY id DESC LIMIT 1").Row().Scan(&staff))
		require.NoError(t, roles.AssignRoles(t.Context(), staff, []string{role}))

		nl = nlservice.New(nlservice.Deps{
			Repo: persistence.NewNewsletterRepository(tx), IDs: uuidGen{}, Clock: clock,
			Mailer: out, Links: fixedLinks{}, Markdown: markdown.NewNewsletter(), Sender: out,
			Webhooks: newsletterprovider.NewVerifier(newsletterSecret),
			Accounts: newslettermail.NewAccounts(persistence.NewUserRepository(tx)),
			Events:   event.Unit{Tx: persistence.NewTransactor(tx), Recorder: out},
			Logger:   slog.New(slog.DiscardHandler),
		}, nlservice.Config{})
		deps.RBAC = enforcer
		deps.Newsletter = nl
	})

	return &newsletterStack{authStack: s, nl: nl, out: out, nlClock: clock}
}

// raw sends a request with a custom content type and headers.
func (s *newsletterStack) raw(t *testing.T, target, contentType, body string, headers map[string]string) reply {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), nethttp.MethodPost, target, strings.NewReader(body))
	req.Header.Set("Content-Type", contentType)

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	var envelope struct {
		Data  map[string]any `json:"data"`
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope), w.Body.String())

	return reply{status: w.Code, data: envelope.Data, code: envelope.Error.Code}
}

func TestNewsletterLifecycle(t *testing.T) {
	t.Parallel()

	s := newNewsletterStack(t)
	staff, _ := s.login(t)
	suffix := strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	weekly, news := "weekly-"+suffix, "news-"+suffix
	reader := "reader-" + suffix + "@example.test"

	r := s.do(t, nethttp.MethodPut, "/api/v1/admin/newsletter/provider-config", staff, map[string]any{
		"from_name": "Example Blog", "postal_address": "1 Example Street, Jakarta", "confirm_ttl_hours": 48,
		"double_optin_required": true, "lists": []map[string]any{
			{"slug": weekly, "name": "Weekly", "is_default": true}, {"slug": news, "name": "Product news"},
		},
	})
	require.Equal(t, nethttp.StatusOK, r.status, r.code)
	require.Equal(t, true, r.data["ready_to_send"])

	// Double opt-in: the confirmation token activates the pending list, once.
	r = s.do(t, nethttp.MethodPost, "/api/v1/newsletter/subscribe", "", map[string]any{"email": reader, "lists": []string{weekly}})
	require.Equal(t, nethttp.StatusAccepted, r.status, r.code)
	require.Len(t, s.out.confirms, 1)

	confirm := s.out.confirms[0].Token
	r = s.do(t, nethttp.MethodPost, "/api/v1/newsletter/confirm", "", map[string]any{"token": confirm})
	require.Equal(t, nethttp.StatusOK, r.status, r.code)
	require.Equal(t, "active", r.data["status"])
	require.Equal(t, []any{weekly}, r.data["lists"])

	r = s.do(t, nethttp.MethodPost, "/api/v1/newsletter/confirm", "", map[string]any{"token": confirm})
	require.Equal(t, nethttp.StatusConflict, r.status)
	require.Equal(t, "newsletter.token_used", r.code)

	// The welcome email's preferences token manages the subscription.
	require.Len(t, s.out.welcomes, 1)
	prefs := "/api/v1/newsletter/preferences/" + s.out.welcomes[0].PreferencesToken

	r = s.do(t, nethttp.MethodGet, prefs, "", nil)
	require.Equal(t, nethttp.StatusOK, r.status, r.code)
	require.NotEqual(t, reader, r.data["email"], "masked")

	r = s.do(t, nethttp.MethodPatch, prefs, "", map[string]any{"lists": []string{weekly, news}, "format": "plaintext"})
	require.Equal(t, nethttp.StatusOK, r.status, r.code)
	require.Equal(t, "plaintext", r.data["format"])

	// Send an issue now; dispatch mails the subscriber with one-click unsubscribe headers.
	r = s.do(t, nethttp.MethodPost, "/api/v1/admin/newsletter/issues", staff, map[string]any{
		"subject": "September", "body_markdown": "## Hello\n\n<script>alert(1)</script>Read [more](https://example.test).",
		"lists": []string{news}, "status": "queued",
	})
	require.Equal(t, nethttp.StatusCreated, r.status, r.code)
	require.Equal(t, "queued", r.data["status"])
	require.Equal(t, 1, s.out.countEvents(event.NewsletterIssueSendRequested))

	issueID, err := uuid.Parse(fmt.Sprint(r.data["id"]))
	require.NoError(t, err)
	require.NoError(t, s.nl.Dispatch(t.Context(), issueID))
	require.NoError(t, s.nl.Dispatch(t.Context(), issueID), "a redelivered request sends nothing twice")
	require.Len(t, s.out.emails, 1)

	mail := s.out.emails[0]
	require.Equal(t, reader, mail.To)
	require.Empty(t, mail.HTML, "plaintext subscribers get no HTML part")
	require.Contains(t, mail.Text, "1 Example Street")
	require.NotContains(t, mail.Text, "<script>")
	require.Equal(t, "List-Unsubscribe=One-Click", mail.Headers["List-Unsubscribe-Post"])

	r = s.do(t, nethttp.MethodGet, "/api/v1/admin/newsletter/issues/"+issueID.String(), staff, nil)
	require.Equal(t, "sent", r.data["status"])
	require.InDelta(t, 1, r.data["sent_count"], 0)

	// RFC 8058 one-click: the mail client posts the form to the List-Unsubscribe URL.
	header := strings.Trim(mail.Headers["List-Unsubscribe"], "<>")
	unsubscribeURL, err := url.Parse(header)
	require.NoError(t, err)
	require.Equal(t, "api.example.test", unsubscribeURL.Host)

	r = s.raw(t, unsubscribeURL.RequestURI(), "application/x-www-form-urlencoded", "List-Unsubscribe=One-Click", nil)
	require.Equal(t, nethttp.StatusOK, r.status, r.code)

	sub := s.subscriberByEmail(t, staff, "reader-"+suffix)
	require.Equal(t, "unsubscribed", sub["status"])

	// A signed complaint suppresses the address; public subscribe then sends nothing.
	body := fmt.Sprintf(`{"events":[{"type":"complaint","email":%q,"occurred_at":%q}]}`, reader, time.Now().UTC().Format(time.RFC3339))
	ts := strconv.FormatInt(time.Now().Unix(), 10)

	r = s.raw(t, "/api/v1/newsletter/webhooks/provider", "application/json", body, map[string]string{
		"X-Newsletter-Timestamp": ts, "X-Newsletter-Signature": "v1=bad",
	})
	require.Equal(t, nethttp.StatusUnauthorized, r.status)

	r = s.raw(t, "/api/v1/newsletter/webhooks/provider", "application/json", body, map[string]string{
		"X-Newsletter-Timestamp": ts, "X-Newsletter-Signature": newsletterprovider.Sign([]byte(newsletterSecret), ts, []byte(body)),
	})
	require.Equal(t, nethttp.StatusOK, r.status, r.code)
	require.InDelta(t, 1, r.data["applied"], 0)
	require.Equal(t, "complained", s.subscriberByEmail(t, staff, "reader-"+suffix)["status"])

	r = s.do(t, nethttp.MethodPost, "/api/v1/newsletter/subscribe", "", map[string]any{"email": reader})
	require.Equal(t, nethttp.StatusAccepted, r.status)
	require.Len(t, s.out.confirms, 1, "suppressed addresses get no confirmation email")

	// Scheduled issues are queued by the scheduler once send_at passes.
	r = s.do(t, nethttp.MethodPost, "/api/v1/admin/newsletter/issues", staff, map[string]any{
		"subject": "October", "body_markdown": "Soon", "lists": []string{weekly}, "status": "scheduled",
		"send_at": s.nlClock.Now().Add(time.Hour).Format(time.RFC3339),
	})
	require.Equal(t, nethttp.StatusCreated, r.status, r.code)

	released, err := s.nl.ReleaseDue(t.Context(), 10)
	require.NoError(t, err)
	require.Zero(t, released)

	s.nlClock.advance(2 * time.Hour)

	released, err = s.nl.ReleaseDue(t.Context(), 10)
	require.NoError(t, err)
	require.Equal(t, 1, released)

	// Erasure keeps the suppressed row but drops the address.
	id := fmt.Sprint(sub["id"])
	r = s.do(t, nethttp.MethodDelete, "/api/v1/admin/newsletter/subscribers/"+id+"?mode=hard_delete", staff, nil)
	require.Equal(t, nethttp.StatusOK, r.status, r.code)
	require.Equal(t, "erased", r.data["status"])
	require.Empty(t, r.data["email"])
}

type passwordOK struct{}

func (passwordOK) VerifyPassword(context.Context, uuid.UUID, string) (bool, error) { return true, nil }

func TestAccountErasureErasesTheNewsletterSubscriber(t *testing.T) {
	t.Parallel()

	s := newNewsletterStack(t)
	staff, _ := s.login(t)
	weekly := "weekly-" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")

	r := s.do(t, nethttp.MethodPut, "/api/v1/admin/newsletter/provider-config", staff, map[string]any{
		"from_name": "Example Blog", "postal_address": "1 Example Street, Jakarta", "confirm_ttl_hours": 48,
		"double_optin_required": true, "lists": []map[string]any{{"slug": weekly, "name": "Weekly", "is_default": true}},
	})
	require.Equal(t, nethttp.StatusOK, r.status, r.code)

	r = s.do(t, nethttp.MethodPost, "/api/v1/me/newsletter/subscribe", staff, map[string]any{})
	require.Less(t, r.status, 300, r.code)

	var userID uuid.UUID
	require.NoError(t, s.tx.Raw("SELECT uuid FROM users WHERE email = ?", s.email).Row().Scan(&userID))

	data := persistence.NewPrivacyData(s.tx)
	privacy := privacyservice.New(persistence.NewPrivacyRepository(s.tx), data, data, passwordOK{}, uuidGen{}, s.nlClock,
		privacyservice.Config{ExportRetention: time.Hour, DownloadTTL: time.Minute}).
		WithEvents(event.Unit{Tx: persistence.NewTransactor(s.tx), Recorder: s.out}).
		WithModuleErasers(s.nl)

	_, _, err := privacy.RequestErase(t.Context(), userID, "pw")
	require.NoError(t, err)

	changed := s.out.countEvents(event.NewsletterSubscriberChanged)
	done, err := privacy.ProcessPending(t.Context(), 10)
	require.NoError(t, err)
	require.Equal(t, 1, done)

	var sub struct {
		Status string
		Email  *string
		UserID *int64
		Source *string
	}
	require.NoError(t, s.tx.Raw(`SELECT s.status, s.email, s.user_id,
			(SELECT a.source FROM newsletter_consent_audit a WHERE a.subscriber_id = s.id AND a.event = 'erased') AS source
		FROM newsletter_subscribers s WHERE s.normalized_email IS NULL AND s.erased_at IS NOT NULL
		ORDER BY s.id DESC LIMIT 1`).Scan(&sub).Error)
	require.Equal(t, "erased", sub.Status)
	require.Nil(t, sub.Email)
	require.Nil(t, sub.UserID, "the erased row no longer points at the account")
	require.NotNil(t, sub.Source)
	require.Equal(t, nldomain.ConsentSourcePrivacy, *sub.Source)
	require.Equal(t, changed+1, s.out.countEvents(event.NewsletterSubscriberChanged), "provider sync hears of the erasure")

	var remaining int64
	require.NoError(t, s.tx.Raw("SELECT count(*) FROM newsletter_subscribers WHERE normalized_email = lower(?)", s.email).
		Row().Scan(&remaining))
	require.Zero(t, remaining, "the account address is gone from the newsletter")
}

func (s *newsletterStack) subscriberByEmail(t *testing.T, staff, prefix string) map[string]any {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), nethttp.MethodGet, "/api/v1/admin/newsletter/subscribers?q="+prefix, nil)
	req.Header.Set("Authorization", "Bearer "+staff)

	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)
	require.Equal(t, nethttp.StatusOK, w.Code, w.Body.String())

	var page struct {
		Data []map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page), w.Body.String())
	require.Len(t, page.Data, 1)

	return page.Data[0]
}
