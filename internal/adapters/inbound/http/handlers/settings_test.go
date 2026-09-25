package handlers

import (
	"context"
	"encoding/json"
	"errors"
	nethttp "net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	settingsdomain "github.com/turahe/blog-api/internal/core/settings/domain"
	settingsservice "github.com/turahe/blog-api/internal/core/settings/service"
)

type fakeSettings struct {
	listFilter    settingsservice.ListFilter
	actor         settingsservice.Actor
	updates       []settingsservice.Update
	historyFilter settingsdomain.HistoryFilter
	updateErr     error
}

func (f *fakeSettings) List(_ context.Context, filter settingsservice.ListFilter) ([]settingsdomain.Setting, error) {
	f.listFilter = filter
	updatedAt := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)

	return []settingsdomain.Setting{
		{
			Definition: settingsdomain.Definition{Key: "site.name", Category: settingsdomain.CategorySite, Type: settingsdomain.TypeString, Sensitivity: settingsdomain.PublicSafe, Default: "Blog"},
			Value:      "Mine", Version: 2, UpdatedAt: &updatedAt, UpdatedBy: &testUserID,
		},
		{
			Definition: settingsdomain.Definition{Key: "media.quality", Category: settingsdomain.CategoryMedia, Type: settingsdomain.TypeInteger, Sensitivity: settingsdomain.PublicSafe, Default: int64(80)},
			Value:      int64(80), Defaulted: true,
		},
	}, nil
}

func (f *fakeSettings) Update(_ context.Context, actor settingsservice.Actor, updates []settingsservice.Update) (settingsservice.Result, error) {
	f.actor, f.updates = actor, updates
	if f.updateErr != nil {
		return settingsservice.Result{}, f.updateErr
	}

	return settingsservice.Result{
		Applied:   []settingsdomain.Change{{Key: "site.name", Previous: "Blog", New: "Mine", Version: 1}},
		Unchanged: []string{"media.quality"},
	}, nil
}

func (f *fakeSettings) History(_ context.Context, filter settingsdomain.HistoryFilter) (settingsdomain.HistoryPage, error) {
	f.historyFilter = filter

	return settingsdomain.HistoryPage{
		Items: []settingsdomain.HistoryEntry{
			{UUID: uuid.New(), Key: "site.name", Previous: json.RawMessage(`"Blog"`), New: json.RawMessage(`"Mine"`), Version: 1, CreatedAt: testTime},
			{UUID: uuid.New(), Key: "retired.key", Version: 3, Redacted: true, CreatedAt: testTime},
		},
		Page: 1, PerPage: 20, Total: 2,
	}, nil
}

type permissionSet map[string]bool

func (p permissionSet) Enforce(_ context.Context, _ uuid.UUID, permission string) (bool, error) {
	return p[permission], nil
}

func settingsHandlers(svc settingsAPI, perms permissionSet) settingsRoutes {
	c := NewControllers(Deps{Settings: svc, RBAC: perms}).Settings
	return settingsRoutes{get: c.Get, put: c.Put, history: c.History}
}

type settingsRoutes struct {
	get, put, history gin.HandlerFunc
}

func allSettingsPerms() permissionSet {
	return permissionSet{permSettingsRead: true, permSettingsUpdate: true, permSettingsHistory: true}
}

func TestSettingsRoutesRequirePermissions(t *testing.T) {
	t.Parallel()

	enforcer := &recordingEnforcer{}
	c := NewControllers(Deps{Settings: &fakeSettings{}, RBAC: enforcer}).Settings
	want := map[string]string{"get": permSettingsRead, "put": permSettingsUpdate, "history": permSettingsHistory}

	for name, handler := range map[string]gin.HandlerFunc{"get": c.Get, "put": c.Put, "history": c.History} {
		enforcer.checked = nil

		w, body := runProfile(t, handler, profileRequest{method: nethttp.MethodGet, target: "/", body: `{}`, user: &testUserID})
		require.Equal(t, nethttp.StatusForbidden, w.Code, name)
		require.Equal(t, "rbac.forbidden", errorCode(body), name)
		require.Equal(t, []string{want[name]}, enforcer.checked, name)

		w, _ = runProfile(t, handler, profileRequest{method: nethttp.MethodGet, target: "/", body: `{}`})
		require.Equal(t, nethttp.StatusUnauthorized, w.Code, name)
	}

	editor := NewControllers(Deps{Settings: &fakeSettings{}, Roles: fakeRoleLookup{names: []string{"editor"}}}).Settings
	w, _ := runProfile(t, editor.Get, profileRequest{method: nethttp.MethodGet, target: "/", user: &testUserID})
	require.Equal(t, nethttp.StatusForbidden, w.Code, "without RBAC only admins pass")
}

func TestGetSettingsListsAndFilters(t *testing.T) {
	t.Parallel()

	svc := &fakeSettings{}
	h := settingsHandlers(svc, allSettingsPerms())

	w, body := runProfile(t, h.get, profileRequest{method: nethttp.MethodGet, target: "/?category=site&includeSensitiveAdmin=true", user: &testUserID})
	require.Equal(t, nethttp.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, settingsservice.ListFilter{Category: settingsdomain.CategorySite, IncludeAdminOnly: true}, svc.listFilter)

	data := dataOf(body)
	assert.Equal(t, []any{"media.quality"}, data["defaultApplied"])

	items, _ := data["settings"].([]any)
	require.Len(t, items, 2)
	first, _ := items[0].(map[string]any)
	assert.Equal(t, "site.name", first["key"])
	assert.Equal(t, "Mine", first["value"])
	assert.Equal(t, "Blog", first["default"])
	assert.Equal(t, "string", first["valueType"])
	assert.InDelta(t, 2, first["version"], 0)
	assert.Equal(t, testUserID.String(), first["updatedBy"])
}

func TestGetSettingsAdminOnlyNeedsUpdatePermission(t *testing.T) {
	t.Parallel()

	svc := &fakeSettings{}
	h := settingsHandlers(svc, permissionSet{permSettingsRead: true})

	w, _ := runProfile(t, h.get, profileRequest{method: nethttp.MethodGet, target: "/?includeSensitiveAdmin=1", user: &testUserID})
	require.Equal(t, nethttp.StatusOK, w.Code)
	assert.False(t, svc.listFilter.IncludeAdminOnly)

	w, body := runProfile(t, h.get, profileRequest{method: nethttp.MethodGet, target: "/?includeSensitiveAdmin=maybe", user: &testUserID})
	require.Equal(t, nethttp.StatusBadRequest, w.Code)
	assert.Equal(t, responses.ErrorCodeValidation, errorCode(body))
}

func TestPutSettingsPassesUpdatesAndActor(t *testing.T) {
	t.Parallel()

	svc := &fakeSettings{}
	h := settingsHandlers(svc, allSettingsPerms())

	w, body := runProfile(t, h.put, profileRequest{
		method: nethttp.MethodPut, target: "/", contentType: "application/json", user: &testUserID,
		body: `{"updates":[{"key":"site.name","value":"Mine","version":0},{"key":"media.quality","value":80}]}`,
	})
	require.Equal(t, nethttp.StatusOK, w.Code, w.Body.String())

	require.Len(t, svc.updates, 2)
	assert.Equal(t, "site.name", svc.updates[0].Key)
	assert.JSONEq(t, `"Mine"`, string(svc.updates[0].Value))
	require.NotNil(t, svc.updates[0].Version)
	assert.Zero(t, *svc.updates[0].Version)
	assert.Nil(t, svc.updates[1].Version)
	assert.Equal(t, &testUserID, svc.actor.UserID)

	data := dataOf(body)
	assert.Equal(t, []any{"media.quality"}, data["unchanged"])
	applied, _ := data["applied"].([]any)
	require.Len(t, applied, 1)
	assert.Equal(t, map[string]any{"key": "site.name", "previousValue": "Blog", "newValue": "Mine", "version": float64(1)}, applied[0])
}

func TestPutSettingsErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		body   string
		err    error
		status int
		code   string
	}{
		{name: "missing updates", body: `{}`, status: nethttp.StatusBadRequest, code: responses.ErrorCodeValidation},
		{name: "empty updates", body: `{"updates":[]}`, status: nethttp.StatusBadRequest, code: responses.ErrorCodeValidation},
		{
			name: "invalid values", body: `{"updates":[{"key":"x","value":1}]}`, status: nethttp.StatusUnprocessableEntity, code: responses.ErrorCodeValidation,
			err: &settingsdomain.ValidationError{Violations: []settingsdomain.Violation{{Key: "x", Reason: settingsdomain.ReasonUnknownKey, Message: "unknown setting"}}},
		},
		{name: "stale version", body: `{"updates":[{"key":"x","value":1}]}`, err: settingsdomain.ErrVersionConflict, status: nethttp.StatusConflict, code: errorCodeSettingsVersionConflict},
		{name: "batch rule", body: `{"updates":[{"key":"x","value":1}]}`, err: settingsservice.ErrValidation, status: nethttp.StatusBadRequest, code: responses.ErrorCodeValidation},
		{name: "store failure", body: `{"updates":[{"key":"x","value":1}]}`, err: errors.New("db down"), status: nethttp.StatusInternalServerError, code: responses.ErrorCodeInternal},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := settingsHandlers(&fakeSettings{updateErr: tc.err}, allSettingsPerms())

			w, body := runProfile(t, h.put, profileRequest{method: nethttp.MethodPut, target: "/", contentType: "application/json", body: tc.body, user: &testUserID})
			require.Equal(t, tc.status, w.Code, w.Body.String())
			assert.Equal(t, tc.code, errorCode(body))
			assert.NotContains(t, w.Body.String(), "db down")

			if tc.status == nethttp.StatusUnprocessableEntity {
				errObj, _ := body["error"].(map[string]any)
				details, _ := errObj["details"].(map[string]any)
				assert.Equal(t, []any{map[string]any{"key": "x", "reason": "unknown_key", "message": "unknown setting"}}, details["violations"])
			}
		})
	}
}

func TestSettingsHistoryPaginatesAndShowsRedaction(t *testing.T) {
	t.Parallel()

	svc := &fakeSettings{}
	h := settingsHandlers(svc, allSettingsPerms())

	w, body := runProfile(t, h.history, profileRequest{method: nethttp.MethodGet, target: "/?key=site.name&page=2&perPage=5", user: &testUserID})
	require.Equal(t, nethttp.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, settingsdomain.HistoryFilter{Key: "site.name", Page: 2, PerPage: 5}, svc.historyFilter)

	items, _ := body["data"].([]any)
	require.Len(t, items, 2)
	first, _ := items[0].(map[string]any)
	assert.Equal(t, "Blog", first["previousValue"])
	assert.Equal(t, "Mine", first["newValue"])

	second, _ := items[1].(map[string]any)
	assert.Equal(t, true, second["redacted"])
	assert.Nil(t, second["newValue"])

	meta, _ := body["meta"].(map[string]any)
	assert.InDelta(t, 2, meta["total"], 0)
}
