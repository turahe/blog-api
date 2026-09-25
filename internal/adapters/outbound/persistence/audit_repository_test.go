package persistence

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	auditdomain "github.com/turahe/blog-api/internal/core/audit/domain"
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

	page, err := repo.Activity(t.Context(), auditdomain.ActivityFilter{UserID: user, Page: 1, PerPage: 10})
	require.NoError(t, err)
	require.Equal(t, int64(1), page.Total)

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

	all, err := repo.Activity(t.Context(), auditdomain.ActivityFilter{UserID: user, Page: 1, PerPage: 10})
	require.NoError(t, err)
	require.Equal(t, int64(3), all.Total)
	require.Equal(t, "admin.posts.delete", all.Items[0].Action, "newest first")

	owner, err := repo.Activity(t.Context(), auditdomain.ActivityFilter{UserID: user, CategorizedOnly: true, Page: 1, PerPage: 10})
	require.NoError(t, err)
	require.Equal(t, int64(2), owner.Total)

	logins, err := repo.Activity(t.Context(), auditdomain.ActivityFilter{UserID: user, Categories: []string{"login"}, Page: 1, PerPage: 10})
	require.NoError(t, err)
	require.Equal(t, int64(1), logins.Total)

	from, to := base.Add(30*time.Minute), base.Add(90*time.Minute)
	window, err := repo.Activity(t.Context(), auditdomain.ActivityFilter{UserID: user, From: &from, To: &to, Page: 1, PerPage: 10})
	require.NoError(t, err)
	require.Equal(t, int64(1), window.Total)
	require.Equal(t, "admin.users.roles.assign", window.Items[0].Action)

	paged, err := repo.Activity(t.Context(), auditdomain.ActivityFilter{UserID: user, Page: 2, PerPage: 2})
	require.NoError(t, err)
	require.Equal(t, int64(3), paged.Total)
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

	page, err := repo.Activity(t.Context(), auditdomain.ActivityFilter{UserID: ghost, Page: 1, PerPage: 10})
	require.NoError(t, err)
	require.Equal(t, int64(1), page.Total, "still found as the subject")
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

	page, err := repo.Activity(t.Context(), auditdomain.ActivityFilter{UserID: user, Page: 1, PerPage: 10})
	require.NoError(t, err)
	require.Equal(t, int64(1), page.Total)
}
