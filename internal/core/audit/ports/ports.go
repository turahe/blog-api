// Package ports declares the audit module's driven ports.
package ports

import (
	"context"
	"time"

	"github.com/turahe/blog-api/internal/core/audit/domain"
)

// Repository stores and queries audit entries.
type Repository interface {
	Insert(ctx context.Context, entries []domain.Entry) error
	Activity(ctx context.Context, filter domain.ActivityFilter) (domain.ActivityPage, error)
	// Prune deletes entries that occurred before cutoff and returns how many.
	Prune(ctx context.Context, cutoff time.Time) (int64, error)
}

// Writer accepts entries without blocking the caller.
type Writer interface {
	Record(ctx context.Context, entry domain.Entry)
}
