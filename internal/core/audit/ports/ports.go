// Package ports declares the audit module's driven ports.
package ports

import (
	"context"
	"time"

	"github.com/turahe/blog-api/internal/core/audit/domain"
)

// Inserter stores a batch of entries. Inserting an entry whose UUID is already stored is a
// no-op, so a redelivered batch is safe.
type Inserter interface {
	Insert(ctx context.Context, entries []domain.Entry) error
}

// Repository stores and queries audit entries.
type Repository interface {
	Inserter
	Activity(ctx context.Context, filter domain.ActivityFilter) (domain.ActivityPage, error)
	// Prune deletes entries that occurred before cutoff and returns how many.
	Prune(ctx context.Context, cutoff time.Time) (int64, error)
}

// Writer accepts entries without blocking the caller.
type Writer interface {
	Record(ctx context.Context, entry domain.Entry)
}
