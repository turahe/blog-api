package persistence

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	auditdomain "github.com/turahe/blog-api/internal/core/audit/domain"
	"github.com/turahe/blog-api/internal/shared/pagination"
)

func auditAt(action, category string, actor *uuid.UUID, resourceType string, resourceID *uuid.UUID, at time.Time) auditdomain.Entry {
	return auditdomain.Entry{
		UUID: uuid.New(), Action: action, Category: category, ActorID: actor,
		ResourceType: resourceType, ResourceID: resourceID, Result: auditdomain.ResultSuccess,
		OccurredAt: at,
	}
}

func TestAuditRepositoryRoundTripsEntry(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewAuditRepository(tx)
	user := insertUser(t, tx)
	at := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)

	entry := auditAt("admin.users.roles.assign", "role_change", &user, auditdomain.ResourceUser, &user, at)
	entry.Status = 200
	entry.IP, entry.UserAgent, entry.RequestID = "203.0.113.7", "curl/8.0", "req-1"
	entry.Changes = map[string]auditdomain.Change{"roles": {From: []any{"author"}, To: []any{"author", "editor"}}}
	entry.Metadata = map[string]any{"fields": []any{"bio"}}

	require.NoError(t, repo.Insert(t.Context(), []auditdomain.Entry{entry}))

	f := auditdomain.ActivityFilter{UserID: user}
	f.PageRequest = pagination.ParseLegacy(pagination.CursorConfig{}, 1, 10)
	f.PageRequest.IncludeTotal = true
	page, err := repo.Activity(t.Context(), f)
	require.NoError(t, err)
	require.NotNil(t, page.Total)
	require.Equal(t, int64(1), *page.Total)

	got := page.Items[0]
	require.Equal(t, entry.UUID, got.UUID)
	require.Equal(t, &user, got.ActorID)
	require.Equal(t, 200, got.Status)
	require.Equal(t, entry.Changes, got.Changes)
	require.Equal(t, entry.Metadata, got.Metadata)
	require.Equal(t, "203.0.113.7", got.IP)
	require.Equal(t, "curl/8.0", got.UserAgent)
	require.Equal(t, "req-1", got.RequestID)
	require.True(t, at.Equal(got.OccurredAt))
	require.Nil(t, got.ImpersonatorID)
}

func TestAuditRepositoryIgnoresRedeliveredEntries(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewAuditRepository(tx)
	user := insertUser(t, tx)
	first := auditAt("me.profile.update", "profile_update", &user, "", nil, time.Now().UTC())
	second := auditAt("me.password.change", "password_change", &user, "", nil, time.Now().UTC())

	require.NoError(t, repo.Insert(t.Context(), []auditdomain.Entry{first}))
	require.NoError(t, repo.Insert(t.Context(), []auditdomain.Entry{first, second}))

	f := auditdomain.ActivityFilter{UserID: user}
	f.PageRequest = pagination.ParseLegacy(pagination.CursorConfig{}, 1, 10)
	f.PageRequest.IncludeTotal = true
	page, err := repo.Activity(t.Context(), f)
	require.NoError(t, err)
	require.NotNil(t, page.Total)
	require.Equal(t, int64(2), *page.Total)
}

func TestAuditRepositoryStoresImpersonator(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewAuditRepository(tx)
	target, staff := insertUser(t, tx), insertUser(t, tx)

	entry := auditAt("me.profile.update", "profile_update", &target, "", nil, time.Now().UTC())
	entry.ImpersonatorID = &staff
	require.NoError(t, repo.Insert(t.Context(), []auditdomain.Entry{entry}))

	f := auditdomain.ActivityFilter{UserID: target}
	f.PageRequest = pagination.ParseLegacy(pagination.CursorConfig{}, 1, 10)
	page, err := repo.Activity(t.Context(), f)
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Equal(t, &target, page.Items[0].ActorID)
	require.Equal(t, &staff, page.Items[0].ImpersonatorID)
}

func TestAuditRepositoryActivityCoversActorAndSubject(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewAuditRepository(tx)
	user, admin, other := insertUser(t, tx), insertUser(t, tx), insertUser(t, tx)
	post := uuid.New()
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

	entries := []auditdomain.Entry{
		auditAt("auth.login", "login", &user, auditdomain.ResourceUser, &user, base),
		auditAt("admin.users.roles.assign", "role_change", &admin, auditdomain.ResourceUser, &user, base.Add(time.Hour)),
		auditAt("admin.posts.delete", "", &user, "post", &post, base.Add(2*time.Hour)),
		auditAt("auth.login", "login", &other, auditdomain.ResourceUser, &other, base.Add(3*time.Hour)),
		auditAt("auth.login", "login", nil, "", nil, base.Add(4*time.Hour)),
	}
	require.NoError(t, repo.Insert(t.Context(), entries))

	fAll := auditdomain.ActivityFilter{UserID: user}
	fAll.PageRequest = pagination.ParseLegacy(pagination.CursorConfig{}, 1, 10)
	fAll.PageRequest.IncludeTotal = true
	all, err := repo.Activity(t.Context(), fAll)
	require.NoError(t, err)
	require.NotNil(t, all.Total)
	require.Equal(t, int64(3), *all.Total)
	require.Equal(t, "admin.posts.delete", all.Items[0].Action, "newest first")

	fOwner := auditdomain.ActivityFilter{UserID: user, CategorizedOnly: true}
	fOwner.PageRequest = pagination.ParseLegacy(pagination.CursorConfig{}, 1, 10)
	fOwner.PageRequest.IncludeTotal = true
	owner, err := repo.Activity(t.Context(), fOwner)
	require.NoError(t, err)
	require.NotNil(t, owner.Total)
	require.Equal(t, int64(2), *owner.Total)

	fLogins := auditdomain.ActivityFilter{UserID: user, Categories: []string{"login"}}
	fLogins.PageRequest = pagination.ParseLegacy(pagination.CursorConfig{}, 1, 10)
	fLogins.PageRequest.IncludeTotal = true
	logins, err := repo.Activity(t.Context(), fLogins)
	require.NoError(t, err)
	require.NotNil(t, logins.Total)
	require.Equal(t, int64(1), *logins.Total)

	from, to := base.Add(30*time.Minute), base.Add(90*time.Minute)
	fWindow := auditdomain.ActivityFilter{UserID: user, From: &from, To: &to}
	fWindow.PageRequest = pagination.ParseLegacy(pagination.CursorConfig{}, 1, 10)
	fWindow.PageRequest.IncludeTotal = true
	window, err := repo.Activity(t.Context(), fWindow)
	require.NoError(t, err)
	require.NotNil(t, window.Total)
	require.Equal(t, int64(1), *window.Total)
	require.Equal(t, "admin.users.roles.assign", window.Items[0].Action)

	fPaged := auditdomain.ActivityFilter{UserID: user}
	fPaged.PageRequest = pagination.ParseLegacy(pagination.CursorConfig{}, 2, 2)
	fPaged.PageRequest.IncludeTotal = true
	paged, err := repo.Activity(t.Context(), fPaged)
	require.NoError(t, err)
	require.NotNil(t, paged.Total)
	require.Equal(t, int64(3), *paged.Total)
	require.Len(t, paged.Items, 1)
}

func TestAuditRepositoryStoresUnknownActorAsNull(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewAuditRepository(tx)
	ghost := uuid.New()

	require.NoError(t, repo.Insert(t.Context(), []auditdomain.Entry{
		auditAt("auth.logout", "logout", &ghost, auditdomain.ResourceUser, &ghost, time.Now()),
	}))

	f := auditdomain.ActivityFilter{UserID: ghost}
	f.PageRequest = pagination.ParseLegacy(pagination.CursorConfig{}, 1, 10)
	f.PageRequest.IncludeTotal = true
	page, err := repo.Activity(t.Context(), f)
	require.NoError(t, err)
	require.NotNil(t, page.Total)
	require.Equal(t, int64(1), *page.Total, "still found as the subject")
	require.Nil(t, page.Items[0].ActorID)
}

func TestAuditRepositoryPrunesOldEntries(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewAuditRepository(tx)
	user := insertUser(t, tx)
	now := time.Now().UTC()

	require.NoError(t, repo.Insert(t.Context(), []auditdomain.Entry{
		auditAt("auth.login", "login", &user, auditdomain.ResourceUser, &user, now.AddDate(-2, 0, 0)),
		auditAt("auth.login", "login", &user, auditdomain.ResourceUser, &user, now),
	}))

	deleted, err := repo.Prune(t.Context(), now.AddDate(-1, 0, 0))
	require.NoError(t, err)
	require.GreaterOrEqual(t, deleted, int64(1))

	f := auditdomain.ActivityFilter{UserID: user}
	f.PageRequest = pagination.ParseLegacy(pagination.CursorConfig{}, 1, 10)
	f.PageRequest.IncludeTotal = true
	page, err := repo.Activity(t.Context(), f)
	require.NoError(t, err)
	require.NotNil(t, page.Total)
	require.Equal(t, int64(1), *page.Total)
}
