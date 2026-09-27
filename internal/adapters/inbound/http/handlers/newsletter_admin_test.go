package handlers

import (
	"context"
	"encoding/csv"
	"errors"
	nethttp "net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	nldomain "github.com/turahe/blog-api/internal/core/newsletter/domain"
	nlservice "github.com/turahe/blog-api/internal/core/newsletter/service"
)

func TestWireNewsletterAdminFallsBackToRolesForExportAndSend(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		role   string
		status int
	}{
		{name: "editor may export and send", role: roleEditor, status: nethttp.StatusOK},
		{name: "author may do neither", role: roleAuthor, status: nethttp.StatusForbidden},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fake := &fakeNewsletter{}
			c := NewControllers(Deps{Newsletter: fake, Roles: fakeRoleLookup{names: []string{tc.role}}}).Newsletter

			// Both routes are gated on editorRoles, so an author is stopped by the gate itself.
			w := runNewsletter(t, c.AdminSubscribersList, nlRequest{method: nethttp.MethodGet, target: "/?format=csv", signedIn: true})
			require.Equal(t, tc.status, w.Code, w.Body.String())

			w = runNewsletter(t, c.AdminIssueCreate, nlRequest{
				method: nethttp.MethodPost, target: "/", signedIn: true,
				body: `{"subject":"Hi","bodyMarkdown":"Body","lists":["weekly"],"status":"queued"}`,
			})

			if tc.status == nethttp.StatusOK {
				require.Equal(t, nethttp.StatusCreated, w.Code, w.Body.String())
				require.Equal(t, nldomain.IssueQueued, fake.created.Status)
			} else {
				require.Equal(t, nethttp.StatusForbidden, w.Code)
				require.Equal(t, "forbidden", errorCodeOf(t, w))
			}
		})
	}
}

func TestAdminNewsletterSubscribersHandler(t *testing.T) {
	t.Parallel()

	sub := nldomain.Subscriber{UUID: uuid.New(), Email: "reader@example.test", Status: nldomain.StatusActive, CreatedAt: time.Now()}
	allow := func(*gin.Context) bool { return true }

	tests := []struct {
		name   string
		target string
		err    error
		status int
		code   string
	}{
		{name: "rejects unknown format", target: "/?format=xml", status: nethttp.StatusBadRequest, code: "validation_error"},
		{name: "lists as json", target: "/?status=active&list=weekly&q=reader&page=1&perPage=10", status: nethttp.StatusOK},
		{name: "maps list failure", target: "/", err: nldomain.Invalid("bad status"), status: nethttp.StatusBadRequest, code: "validation_error"},
		{name: "csv maps first page failure", target: "/?format=csv", err: errors.New("db down"), status: nethttp.StatusInternalServerError, code: "internal_error"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fake := &fakeNewsletter{err: tc.err, subscribers: []nldomain.Subscriber{sub}}
			w := runNewsletter(t, adminNewsletterSubscribersHandler(fake, allow), nlRequest{method: nethttp.MethodGet, target: tc.target, signedIn: true})
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Equal(t, tc.code, errorCodeOf(t, w))

			if tc.status == nethttp.StatusOK {
				data, ok := envelopeOf(t, w)["data"].([]any)
				require.True(t, ok)
				require.Len(t, data, 1)
			}
		})
	}
}

func TestExportNewsletterSubscribersStopsOnLaterPageFailure(t *testing.T) {
	t.Parallel()

	optedIn := time.Date(2026, 9, 1, 8, 0, 0, 0, time.FixedZone("WIB", 7*3600))
	fake := &fakeNewsletter{failPage: 2}

	for range newsletterExportPage {
		fake.subscribers = append(fake.subscribers, nldomain.Subscriber{
			UUID: uuid.New(), Email: "reader@example.test", Status: nldomain.StatusActive, OptedInAt: &optedIn,
		})
	}

	allow := func(*gin.Context) bool { return true }
	w := runNewsletter(t, adminNewsletterSubscribersHandler(fake, allow), nlRequest{method: nethttp.MethodGet, target: "/?format=csv", signedIn: true})
	require.Equal(t, nethttp.StatusOK, w.Code)

	rows, err := csv.NewReader(strings.NewReader(w.Body.String())).ReadAll()
	require.NoError(t, err)
	require.Len(t, rows, newsletterExportPage+1, "header plus the first page")
	require.Equal(t, "2026-09-01T01:00:00Z", rows[1][7], "opted_in_at is UTC")
}

func TestAdminNewsletterSubscriberHandlers(t *testing.T) {
	t.Parallel()

	id := uuid.NewString()
	tests := []struct {
		name    string
		handler func(newsletterAdminAPI) gin.HandlerFunc
		req     nlRequest
		err     error
		status  int
		code    string
	}{
		{name: "get rejects invalid id", handler: adminNewsletterSubscriberHandler, req: nlRequest{method: nethttp.MethodGet, target: "/", param: "nope", signedIn: true}, status: nethttp.StatusBadRequest, code: "validation_error"},
		{name: "get maps not found", handler: adminNewsletterSubscriberHandler, err: nldomain.ErrNotFound, req: nlRequest{method: nethttp.MethodGet, target: "/", param: id, signedIn: true}, status: nethttp.StatusNotFound, code: "not_found"},
		{name: "get returns detail", handler: adminNewsletterSubscriberHandler, req: nlRequest{method: nethttp.MethodGet, target: "/", param: id, signedIn: true}, status: nethttp.StatusOK},
		{name: "delete needs sign-in", handler: adminNewsletterDeleteSubscriberHandler, req: nlRequest{method: nethttp.MethodDelete, target: "/", param: id}, status: nethttp.StatusUnauthorized, code: "unauthorized"},
		{name: "delete rejects invalid id", handler: adminNewsletterDeleteSubscriberHandler, req: nlRequest{method: nethttp.MethodDelete, target: "/", param: "nope", signedIn: true}, status: nethttp.StatusBadRequest, code: "validation_error"},
		{name: "delete maps bad mode", handler: adminNewsletterDeleteSubscriberHandler, err: nldomain.Invalid("bad mode"), req: nlRequest{method: nethttp.MethodDelete, target: "/?mode=x", param: id, signedIn: true}, status: nethttp.StatusBadRequest, code: "validation_error"},
		{name: "delete erases", handler: adminNewsletterDeleteSubscriberHandler, req: nlRequest{method: nethttp.MethodDelete, target: "/?mode=hard_delete", param: id, signedIn: true}, status: nethttp.StatusOK},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fake := &fakeNewsletter{err: tc.err, prefs: nlservice.Preferences{Subscriber: nldomain.Subscriber{UUID: uuid.New(), Email: "reader@example.test"}}}
			w := runNewsletter(t, tc.handler(fake), tc.req)
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Equal(t, tc.code, errorCodeOf(t, w))
		})
	}
}

func TestAdminNewsletterIssuesHandler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{name: "lists issues", status: nethttp.StatusOK},
		{name: "maps failure", err: errors.New("db down"), status: nethttp.StatusInternalServerError, code: "internal_error"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fake := &fakeNewsletter{err: tc.err, issues: []nldomain.Issue{{UUID: uuid.New(), Subject: "Hello", Status: nldomain.IssueDraft}}}
			w := runNewsletter(t, adminNewsletterIssuesHandler(fake), nlRequest{method: nethttp.MethodGet, target: "/?status=draft", signedIn: true})
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Equal(t, tc.code, errorCodeOf(t, w))

			if tc.status == nethttp.StatusOK {
				data, ok := envelopeOf(t, w)["data"].([]any)
				require.True(t, ok)
				require.Len(t, data, 1)
			}
		})
	}
}

func TestAdminNewsletterIssueHandlers(t *testing.T) {
	t.Parallel()

	allow := func(*gin.Context) bool { return true }
	deny := func(*gin.Context) bool { return false }
	id := uuid.NewString()
	draft := `{"subject":"Hi","bodyMarkdown":"Body","lists":["weekly"]}`

	tests := []struct {
		name    string
		handler gin.HandlerFunc
		req     nlRequest
		status  int
		code    string
		check   func(t *testing.T, data map[string]any)
	}{
		{name: "create needs sign-in", handler: adminNewsletterCreateIssueHandler(&fakeNewsletter{}, allow), req: nlRequest{method: nethttp.MethodPost, target: "/", body: draft}, status: nethttp.StatusUnauthorized, code: "unauthorized"},
		{name: "create needs lists", handler: adminNewsletterCreateIssueHandler(&fakeNewsletter{}, allow), req: nlRequest{method: nethttp.MethodPost, target: "/", body: `{"subject":"Hi","bodyMarkdown":"B"}`, signedIn: true}, status: nethttp.StatusBadRequest, code: "validation_error"},
		{
			name: "create maps not configured", handler: adminNewsletterCreateIssueHandler(&fakeNewsletter{err: nldomain.ErrNotConfigured}, allow),
			req: nlRequest{method: nethttp.MethodPost, target: "/", body: draft, signedIn: true}, status: nethttp.StatusUnprocessableEntity, code: "newsletter.not_configured",
		},
		{name: "get rejects invalid id", handler: adminNewsletterIssueHandler(&fakeNewsletter{}), req: nlRequest{method: nethttp.MethodGet, target: "/", param: "nope", signedIn: true}, status: nethttp.StatusBadRequest, code: "validation_error"},
		{name: "get maps not found", handler: adminNewsletterIssueHandler(&fakeNewsletter{err: nldomain.ErrNotFound}), req: nlRequest{method: nethttp.MethodGet, target: "/", param: id, signedIn: true}, status: nethttp.StatusNotFound, code: "not_found"},
		{
			name: "get without preview", handler: adminNewsletterIssueHandler(&fakeNewsletter{}), req: nlRequest{method: nethttp.MethodGet, target: "/", param: id, signedIn: true}, status: nethttp.StatusOK,
			check: func(t *testing.T, data map[string]any) {
				t.Helper()
				require.Equal(t, id, data["id"])
				require.NotContains(t, data, "preview")
			},
		},
		{
			name: "get with preview", handler: adminNewsletterIssueHandler(&fakeNewsletter{}), req: nlRequest{method: nethttp.MethodGet, target: "/?preview=true", param: id, signedIn: true}, status: nethttp.StatusOK,
			check: func(t *testing.T, data map[string]any) {
				t.Helper()
				require.Equal(t, map[string]any{"html": "<p>Body</p>", "text": "Body"}, data["preview"])
			},
		},
		{name: "update needs sign-in", handler: adminNewsletterUpdateIssueHandler(&fakeNewsletter{}, allow), req: nlRequest{method: nethttp.MethodPatch, target: "/", param: id, body: `{}`}, status: nethttp.StatusUnauthorized, code: "unauthorized"},
		{name: "update rejects invalid id", handler: adminNewsletterUpdateIssueHandler(&fakeNewsletter{}, allow), req: nlRequest{method: nethttp.MethodPatch, target: "/", param: "nope", body: `{}`, signedIn: true}, status: nethttp.StatusBadRequest, code: "validation_error"},
		{name: "update rejects a bad status", handler: adminNewsletterUpdateIssueHandler(&fakeNewsletter{}, allow), req: nlRequest{method: nethttp.MethodPatch, target: "/", param: id, body: `{"status":"sent"}`, signedIn: true}, status: nethttp.StatusBadRequest, code: "validation_error"},
		{name: "update send needs permission", handler: adminNewsletterUpdateIssueHandler(&fakeNewsletter{}, deny), req: nlRequest{method: nethttp.MethodPatch, target: "/", param: id, body: `{"status":"scheduled"}`, signedIn: true}, status: nethttp.StatusForbidden, code: "rbac.forbidden"},
		{name: "update maps conflict", handler: adminNewsletterUpdateIssueHandler(&fakeNewsletter{err: nldomain.ErrConflict}, allow), req: nlRequest{method: nethttp.MethodPatch, target: "/", param: id, body: `{"subject":"New"}`, signedIn: true}, status: nethttp.StatusConflict, code: "newsletter.conflict"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w := runNewsletter(t, tc.handler, tc.req)
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Equal(t, tc.code, errorCodeOf(t, w))

			if tc.check != nil {
				tc.check(t, dataOf(envelopeOf(t, w)))
			}
		})
	}
}

func TestAdminNewsletterIssuePreviewFailure(t *testing.T) {
	t.Parallel()

	fake := &previewFailingNewsletter{fakeNewsletter: &fakeNewsletter{}}
	w := runNewsletter(t, adminNewsletterIssueHandler(fake), nlRequest{method: nethttp.MethodGet, target: "/?preview=true", param: uuid.NewString(), signedIn: true})
	require.Equal(t, nethttp.StatusInternalServerError, w.Code)
	require.Equal(t, "internal_error", errorCodeOf(t, w))
}

// previewFailingNewsletter loads issues but cannot render them.
type previewFailingNewsletter struct{ *fakeNewsletter }

func (previewFailingNewsletter) Preview(context.Context, nldomain.Issue) (string, string, error) {
	return "", "", errors.New("template broken")
}

func TestAdminNewsletterConfigHandlers(t *testing.T) {
	t.Parallel()

	var providerNone responses.NewsletterProvider

	valid := `{"confirmTtlHours":24,"doubleOptinRequired":true,"lists":[{"slug":"weekly","name":"Weekly","isDefault":true}]}`
	tests := []struct {
		name    string
		handler gin.HandlerFunc
		req     nlRequest
		status  int
		code    string
	}{
		{name: "get maps failure", handler: adminNewsletterConfigHandler(&fakeNewsletter{err: errors.New("db down")}, providerNone), req: nlRequest{method: nethttp.MethodGet, target: "/", signedIn: true}, status: nethttp.StatusInternalServerError, code: "internal_error"},
		{name: "save needs sign-in", handler: adminNewsletterSaveConfigHandler(&fakeNewsletter{}, providerNone), req: nlRequest{method: nethttp.MethodPut, target: "/", body: valid}, status: nethttp.StatusUnauthorized, code: "unauthorized"},
		{name: "save maps validation", handler: adminNewsletterSaveConfigHandler(&fakeNewsletter{err: nldomain.Invalid("one default list required")}, providerNone), req: nlRequest{method: nethttp.MethodPut, target: "/", body: valid, signedIn: true}, status: nethttp.StatusBadRequest, code: "validation_error"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w := runNewsletter(t, tc.handler, tc.req)
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Equal(t, tc.code, errorCodeOf(t, w))
		})
	}
}
