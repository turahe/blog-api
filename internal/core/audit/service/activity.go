package service

import (
	"context"
	"errors"
	"time"

	"github.com/turahe/blog-api/internal/core/audit/domain"
	"github.com/turahe/blog-api/internal/core/audit/ports"
	"github.com/turahe/blog-api/internal/shared/pagination"
)

const (
	defaultPerPage = 20
	maxPerPage     = 100
)

// ErrInvalidRange is returned when an activity filter ends before it starts.
var ErrInvalidRange = errors.New("audit: from must not be after to")

// Activity answers activity queries and prunes old entries.
type Activity struct {
	repo ports.Repository
	now  func() time.Time
}

// NewActivity returns the activity service.
func NewActivity(repo ports.Repository) *Activity {
	return &Activity{repo: repo, now: time.Now}
}

// ForOwner lists the user's own activity: user-facing categories only.
func (a *Activity) ForOwner(ctx context.Context, filter domain.ActivityFilter) (domain.ActivityPage, error) {
	filter.CategorizedOnly = true
	return a.list(ctx, filter)
}

// ForAdmin lists every entry the user performed or that targets the user.
func (a *Activity) ForAdmin(ctx context.Context, filter domain.ActivityFilter) (domain.ActivityPage, error) {
	filter.CategorizedOnly = false
	return a.list(ctx, filter)
}

// Prune deletes entries older than retention and returns how many.
func (a *Activity) Prune(ctx context.Context, retention time.Duration) (int64, error) {
	return a.repo.Prune(ctx, a.now().Add(-retention))
}

func (a *Activity) list(ctx context.Context, filter domain.ActivityFilter) (domain.ActivityPage, error) {
	if filter.From != nil && filter.To != nil && filter.From.After(*filter.To) {
		return domain.ActivityPage{}, ErrInvalidRange
	}

	if filter.PageRequest.Cursor == "" {
		mode := filter.PageRequest.Mode
		includeTotal := filter.PageRequest.IncludeTotal
		forward := filter.PageRequest.Forward
		pr := pagination.ParseLegacy(activityCursorCfg, filter.PageRequest.Page, filter.PageRequest.Limit)
		pr.IncludeTotal = includeTotal
		pr.Mode = mode
		pr.Forward = pr.Forward || forward
		filter.PageRequest = pr
	}

	return a.repo.Activity(ctx, filter)
}

var activityCursorCfg = pagination.CursorConfig{
	Kind: "activity_logs",
	Sort: []pagination.SortField{
		{Name: "occurred_at", Column: "audit_logs.occurred_at", Dir: pagination.Desc, Type: pagination.TypeTime},
		{Name: "id", Column: "audit_logs.id", Dir: pagination.Desc, Type: pagination.TypeInt64},
	},
	TTL:            pagination.DefaultTTL,
	MaxPerPage:     maxPerPage,
	DefaultPerPage: defaultPerPage,
}
