package handlers

import (
	"context"
	"errors"
	nethttp "net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/adapters/inbound/routes"
	analyticsdomain "github.com/turahe/blog-api/internal/core/analytics/domain"
	analyticsservice "github.com/turahe/blog-api/internal/core/analytics/service"
)

type fakeAnalyticsExports struct {
	view  analyticsservice.ExportView
	views []analyticsservice.ExportView
	err   error
	in    analyticsservice.ExportRequest
	actor uuid.UUID
	id    uuid.UUID
}

func (f *fakeAnalyticsExports) Request(
	_ context.Context, actor uuid.UUID, in analyticsservice.ExportRequest,
) (analyticsservice.ExportView, error) {
	f.actor, f.in = actor, in

	return f.view, f.err
}

func (f *fakeAnalyticsExports) Get(_ context.Context, actor, id uuid.UUID) (analyticsservice.ExportView, error) {
	f.actor, f.id = actor, id

	return f.view, f.err
}

func (f *fakeAnalyticsExports) List(_ context.Context, actor uuid.UUID) ([]analyticsservice.ExportView, error) {
	f.actor = actor

	return f.views, f.err
}

func pendingExportView() analyticsservice.ExportView {
	return analyticsservice.ExportView{Export: analyticsdomain.Export{
		UUID: uuid.New(), Grain: analyticsdomain.GrainDay, FirstDay: "2026-08-01", LastDay: "2026-08-31",
		Timezone: "Asia/Jakarta", Status: analyticsdomain.ExportPending, CreatedAt: time.Now(),
	}}
}

const exportBody = `{"from":"2026-08-01","to":"2026-08-31","current_password":"pw","two_factor_code":"123456"}`

func TestAdminAnalyticsExportQueues(t *testing.T) {
	t.Parallel()

	fake := &fakeAnalyticsExports{view: pendingExportView()}
	w, body := runProfile(t, adminAnalyticsExportHandler(fake), profileRequest{
		method: nethttp.MethodPost, target: "/", body: exportBody, contentType: "application/json", user: &testUserID,
	})
	require.Equal(t, nethttp.StatusAccepted, w.Code, w.Body.String())
	assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	assert.Equal(t, testUserID, fake.actor)
	assert.Equal(t, "pw", fake.in.Password)
	assert.Equal(t, "123456", fake.in.Code)
	assert.Equal(t, "2026-08-01", fake.in.Query.From.Format(time.DateOnly))

	data := dataOf(body)
	assert.Equal(t, "pending", data["status"])
	assert.Equal(t, "2026-08-31", data["to"])
	assert.NotContains(t, data, "download_url")
}

func TestAdminAnalyticsExportRefusals(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		body   string
		err    error
		status int
		code   string
	}{
		"no password":  {`{"from":"2026-08-01","to":"2026-08-31"}`, nil, nethttp.StatusBadRequest, "validation_error"},
		"bad date":     {`{"from":"2026-8-1","to":"2026-08-31","current_password":"pw"}`, nil, nethttp.StatusBadRequest, "validation_error"},
		"bad grain":    {`{"from":"2026-08-01","to":"2026-08-31","grain":"year","current_password":"pw"}`, nil, nethttp.StatusBadRequest, "validation_error"},
		"window":       {exportBody, analyticsdomain.ErrValidation, nethttp.StatusBadRequest, "validation_error"},
		"step-up":      {exportBody, analyticsdomain.ErrStepUpRequired, nethttp.StatusForbidden, "analytics.step_up_required"},
		"no storage":   {exportBody, analyticsdomain.ErrExportUnavailable, nethttp.StatusServiceUnavailable, "analytics.export_unavailable"},
		"server error": {exportBody, errors.New("boom"), nethttp.StatusInternalServerError, "internal_error"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fake := &fakeAnalyticsExports{err: tc.err}
			w, body := runProfile(t, adminAnalyticsExportHandler(fake), profileRequest{
				method: nethttp.MethodPost, target: "/", body: tc.body, contentType: "application/json", user: &testUserID,
			})
			require.Equal(t, tc.status, w.Code, w.Body.String())
			assert.Equal(t, tc.code, errorCode(body))
		})
	}
}

func TestAdminAnalyticsExportConflictNamesTheOpenExport(t *testing.T) {
	t.Parallel()

	open := pendingExportView()
	fake := &fakeAnalyticsExports{view: open, err: analyticsdomain.ErrExportOpen}
	w, body := runProfile(t, adminAnalyticsExportHandler(fake), profileRequest{
		method: nethttp.MethodPost, target: "/", body: exportBody, contentType: "application/json", user: &testUserID,
	})
	require.Equal(t, nethttp.StatusConflict, w.Code)
	assert.Equal(t, "analytics.export_in_progress", errorCode(body))

	details, ok := body["error"].(map[string]any)["details"].(map[string]any)
	require.True(t, ok, body)
	assert.Equal(t, open.UUID.String(), details["id"])
}

func TestAdminAnalyticsExportGetAndList(t *testing.T) {
	t.Parallel()

	view := pendingExportView()
	key := "analytics-exports/x.zip"
	size := int64(42)
	expires := time.Now().Add(time.Hour)
	link := time.Now().Add(15 * time.Minute)
	view.Status, view.StorageKey, view.SizeBytes, view.ExpiresAt = analyticsdomain.ExportCompleted, &key, &size, &expires
	view.DownloadURL, view.DownloadExpiresAt = "https://storage.example/x", &link

	fake := &fakeAnalyticsExports{view: view, views: []analyticsservice.ExportView{view}}

	w, body := runProfile(t, adminAnalyticsExportGetHandler(fake), profileRequest{
		method: nethttp.MethodGet, target: "/", param: view.UUID.String(), user: &testUserID,
	})
	require.Equal(t, nethttp.StatusOK, w.Code)
	assert.Equal(t, view.UUID, fake.id)

	data := dataOf(body)
	assert.Equal(t, "completed", data["status"])
	assert.Equal(t, "https://storage.example/x", data["download_url"])
	assert.InDelta(t, 42, data["size_bytes"], 0)

	w, body = runProfile(t, adminAnalyticsExportsListHandler(fake), profileRequest{method: nethttp.MethodGet, target: "/", user: &testUserID})
	require.Equal(t, nethttp.StatusOK, w.Code)
	assert.Len(t, body["data"], 1)

	w, body = runProfile(t, adminAnalyticsExportGetHandler(fake), profileRequest{
		method: nethttp.MethodGet, target: "/", param: "nope", user: &testUserID,
	})
	require.Equal(t, nethttp.StatusBadRequest, w.Code)
	assert.Equal(t, "validation_error", errorCode(body))

	missing := &fakeAnalyticsExports{err: analyticsdomain.ErrExportNotFound}
	w, body = runProfile(t, adminAnalyticsExportGetHandler(missing), profileRequest{
		method: nethttp.MethodGet, target: "/", param: uuid.NewString(), user: &testUserID,
	})
	require.Equal(t, nethttp.StatusNotFound, w.Code)
	assert.Equal(t, "not_found", errorCode(body))
}

func analyticsExportOps() map[string]gatedOp {
	return map[string]gatedOp{
		"export": {func(c routes.Controllers) gin.HandlerFunc { return c.Exports.Create }, analyticsdomain.PermExport, adminRoles},
		"list":   {func(c routes.Controllers) gin.HandlerFunc { return c.Exports.List }, analyticsdomain.PermExport, adminRoles},
		"get":    {func(c routes.Controllers) gin.HandlerFunc { return c.Exports.Get }, analyticsdomain.PermExport, adminRoles},
	}
}

func TestAdminAnalyticsExportsAreAdminOnly(t *testing.T) {
	t.Parallel()

	for op, tc := range analyticsExportOps() {
		enforcer := &recordingEnforcer{}
		handler := tc.handler(NewControllers(Deps{AnalyticsExports: &fakeAnalyticsExports{}, RBAC: enforcer}))

		w, body := runProfile(t, handler, profileRequest{method: nethttp.MethodPost, target: "/", body: `{}`, user: &testUserID})
		require.Equal(t, nethttp.StatusForbidden, w.Code, op)
		require.Equal(t, "rbac.forbidden", errorCode(body), op)
		require.Equal(t, []string{tc.permission}, enforcer.checked, op)

		w, _ = runProfile(t, handler, profileRequest{method: nethttp.MethodPost, target: "/", body: `{}`})
		require.Equal(t, nethttp.StatusUnauthorized, w.Code, op)

		deps := Deps{AnalyticsExports: &fakeAnalyticsExports{}, Roles: fakeRoleLookup{names: []string{roleEditor}}}
		w, body = runProfile(t, tc.handler(NewControllers(deps)), profileRequest{method: nethttp.MethodPost, target: "/", body: `{}`, user: &testUserID})
		require.Equal(t, nethttp.StatusForbidden, w.Code, op)
		require.Equal(t, "forbidden", errorCode(body), op, "editors cannot export")
	}

	c := NewControllers(Deps{})
	assert.Nil(t, c.Exports.Create, "a stub without the service")
}
