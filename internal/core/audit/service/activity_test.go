package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/core/audit/domain"
	"github.com/turahe/blog-api/internal/core/audit/service"
	"github.com/turahe/blog-api/internal/shared/pagination"
)

func TestActivityOwnerSeesCategorizedEntriesOnly(t *testing.T) {
	t.Parallel()

	repo := &memRepo{}
	activity := service.NewActivity(repo)
	user := uuid.New()

	pr := pagination.PageRequest{Mode: pagination.ModeOffset, Page: 1, Limit: 500, Offset: 0, IncludeTotal: true}
	filter := domain.ActivityFilter{UserID: user}
	filter.PageRequest = pr
	page, err := activity.ForOwner(context.Background(), filter)
	require.NoError(t, err)

	require.True(t, repo.filter.CategorizedOnly)
	require.Equal(t, user, repo.filter.UserID)
	require.Equal(t, 1, page.OffsetPage)
	require.Equal(t, 100, page.OffsetPerPage, "per_page is capped")
}

func TestActivityAdminSeesEverything(t *testing.T) {
	t.Parallel()

	repo := &memRepo{}

	filter := domain.ActivityFilter{CategorizedOnly: true}
	page, err := service.NewActivity(repo).ForAdmin(context.Background(), filter)
	require.NoError(t, err)

	require.False(t, repo.filter.CategorizedOnly)
	require.Equal(t, 1, repo.filter.PageRequest.Page)
	require.Equal(t, 20, repo.filter.PageRequest.Limit)
	require.Equal(t, 20, page.Limit)
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
