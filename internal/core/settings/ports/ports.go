// Package ports declares the settings repository interface.
package ports

import (
	"context"

	settingsdomain "github.com/turahe/blog-api/internal/core/settings/domain"
)

// Repository stores changed settings and their history.
type Repository interface {
	// List returns every stored value.
	List(ctx context.Context) ([]settingsdomain.Stored, error)
	// Lock returns the stored values of keys and locks their rows until the transaction ends.
	Lock(ctx context.Context, keys []string) ([]settingsdomain.Stored, error)
	// Save applies w and appends a history row, or returns ErrVersionConflict when the key
	// is no longer at w.ExpectedVersion.
	Save(ctx context.Context, w settingsdomain.Write) error
	// History returns recorded changes, newest first.
	History(ctx context.Context, filter settingsdomain.HistoryFilter) (settingsdomain.HistoryPage, error)
}
