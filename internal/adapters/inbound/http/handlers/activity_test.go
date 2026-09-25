package handlers

import (
	"context"
	nethttp "net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	auditdomain "github.com/turahe/blog-api/internal/core/audit/domain"
	auditservice "github.com/turahe/blog-api/internal/core/audit/service"
)

type fakeActivity struct {
	filter auditdomain.ActivityFilter
	scope  string
	page   auditdomain.ActivityPage
	err    error
}

func (f *fakeActivity) ForOwner(_ context.Context, filter auditdomain.ActivityFilter) (auditdomain.ActivityPage, error) {
	f.filter, f.scope = filter, "owner"
	return f.page, f.err
}

func (f *fakeActivity) ForAdmin(_ context.Context, filter auditdomain.ActivityFilter) (auditdomain.ActivityPage, error) {
	f.filter, f.scope = filter, "admin"
	return f.page, f.err
}

func sampleActivity() auditdomain.ActivityPage {
	actor, target := uuid.New(), uuid.New()

	return auditdomain.ActivityPage{
		Page: 1, PerPage: 20, Total: 1,
		Items: []auditdomain.Entry{{
			UUID:         uuid.New(),
			Action:       "admin.users.roles.assign",
			Category:     "role_change",
			ActorID:      &actor,
			ResourceType: auditdomain.ResourceUser,
			ResourceID:   &target,
			Result:       auditdomain.ResultSuccess,
			Status:       nethttp.StatusOK,
			Changes:      map[string]auditdomain.Change{"roles": {From: []any{"author"}, To: []any{"author", "editor"}}},
			IP:           "203.0.113.77",
			UserAgent:    "Mozilla/5.0 (Macintosh; Intel Mac OS X 14_0) AppleWebKit/605.1.15 Version/17.0 Safari/605.1.15",
			RequestID:    "req-1",
			OccurredAt:   time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC),
		}},
	}
}

func TestMeActivityShowsCoarseOwnerView(t *testing.T) {
	t.Parallel()

	user := uuid.New()
	activity := &fakeActivity{page: sampleActivity()}

	w, body := runProfile(t, meActivityHandler(activity), profileRequest{
		method: nethttp.MethodGet,
		target: "/me/activity?category=login,role_change&from=2026-09-01&to=2026-09-25&page=2&perPage=5",
		user:   &user,
	})

	require.Equal(t, nethttp.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "owner", activity.scope)
	require.Equal(t, user, activity.filter.UserID)
	require.Equal(t, []string{"login", "role_change"}, activity.filter.Categories)
	require.Equal(t, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), *activity.filter.From)
	require.Equal(t, time.Date(2026, 9, 25, 23, 59, 59, 999999999, time.UTC), *activity.filter.To)
	require.Equal(t, 2, activity.filter.Page)
	require.Equal(t, 5, activity.filter.PerPage)

	items, _ := body["data"].([]any)
	require.Len(t, items, 1)
	item, _ := items[0].(map[string]any)
	require.Equal(t, "role_change", item["category"])
	require.Equal(t, "203.0.113.0/24", item["ipPrefix"])
	require.Equal(t, "Safari on macOS", item["device"])

	for _, hidden := range []string{"ip", "userAgent", "requestId", "metadata", "changes", "actorId"} {
		require.NotContains(t, item, hidden)
	}
}

func TestAdminUserActivityShowsFullEntry(t *testing.T) {
	t.Parallel()

	admin, target := uuid.New(), uuid.New()
	activity := &fakeActivity{page: sampleActivity()}

	w, body := runProfile(t, adminUserActivityHandler(activity), profileRequest{
		method: nethttp.MethodGet, target: "/admin/users/x/activity", param: target.String(), user: &admin,
	})

	require.Equal(t, nethttp.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "admin", activity.scope)
	require.Equal(t, target, activity.filter.UserID)

	items, _ := body["data"].([]any)
	item, _ := items[0].(map[string]any)
	require.Equal(t, "203.0.113.77", item["ip"])
	require.Equal(t, "req-1", item["requestId"])
	require.Contains(t, item, "changes")
	require.Contains(t, item, "actorId")
}

func TestActivityRejectsBadFilters(t *testing.T) {
	t.Parallel()

	user := uuid.New()

	cases := map[string]struct {
		target string
		err    error
	}{
		"bad from":       {target: "/me/activity?from=yesterday"},
		"bad to":         {target: "/me/activity?to=2026-13-01"},
		"inverted range": {target: "/me/activity", err: auditservice.ErrInvalidRange},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			w, body := runProfile(t, meActivityHandler(&fakeActivity{err: tc.err}), profileRequest{
				method: nethttp.MethodGet, target: tc.target, user: &user,
			})

			require.Equal(t, nethttp.StatusBadRequest, w.Code)
			require.Equal(t, "validation_error", errorCode(body))
		})
	}
}

func TestAdminUserActivityRejectsBadUserID(t *testing.T) {
	t.Parallel()

	admin := uuid.New()

	w, _ := runProfile(t, adminUserActivityHandler(&fakeActivity{}), profileRequest{
		method: nethttp.MethodGet, target: "/admin/users/nope/activity", param: "nope", user: &admin,
	})

	require.Equal(t, nethttp.StatusBadRequest, w.Code)
}

func TestMeActivityRequiresUser(t *testing.T) {
	t.Parallel()

	w, _ := runProfile(t, meActivityHandler(&fakeActivity{}), profileRequest{method: nethttp.MethodGet, target: "/me/activity"})

	require.Equal(t, nethttp.StatusUnauthorized, w.Code)
}
