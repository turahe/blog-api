package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/core/audit/domain"
	"github.com/turahe/blog-api/internal/core/audit/service"
)

func TestActivityOwnerSeesCategorizedEntriesOnly(t *testing.T) {
	t.Parallel()

	repo := &memRepo{}
	activity := service.NewActivity(repo)
	user := uuid.New()

	page, err := activity.ForOwner(context.Background(), domain.ActivityFilter{UserID: user, PerPage: 500})
	require.NoError(t, err)

	require.True(t, repo.filter.CategorizedOnly)
	require.Equal(t, user, repo.filter.UserID)
	require.Equal(t, 1, page.Page)
	require.Equal(t, 100, page.PerPage, "per_page is capped")
}

func TestActivityAdminSeesEverything(t *testing.T) {
	t.Parallel()

	repo := &memRepo{}

	_, err := service.NewActivity(repo).ForAdmin(context.Background(), domain.ActivityFilter{CategorizedOnly: true})
	require.NoError(t, err)

	require.False(t, repo.filter.CategorizedOnly)
	require.Equal(t, 20, repo.filter.PerPage)
}

func TestActivityRejectsInvertedRange(t *testing.T) {
	t.Parallel()

	from := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)
	to := from.Add(-time.Hour)

	_, err := service.NewActivity(&memRepo{}).ForOwner(context.Background(), domain.ActivityFilter{From: &from, To: &to})
	require.ErrorIs(t, err, service.ErrInvalidRange)
}

func TestActivityPruneUsesRetentionCutoff(t *testing.T) {
	t.Parallel()

	repo := &memRepo{}
	before := time.Now()

	deleted, err := service.NewActivity(repo).Prune(context.Background(), 395*24*time.Hour)
	require.NoError(t, err)
	require.Equal(t, int64(3), deleted)
	require.WithinDuration(t, before.Add(-395*24*time.Hour), repo.cutoff, time.Minute)
}
