package service

import (
	"context"
	"log/slog"
	"time"
)

// PruneEvery prunes entries older than retention now and then every interval
// until ctx ends. Failures are logged and retried on the next tick.
func (a *Activity) PruneEvery(ctx context.Context, retention, interval time.Duration, logger *slog.Logger) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		deleted, err := a.Prune(ctx, retention)

		switch {
		case err != nil && ctx.Err() == nil:
			logger.Error("audit prune failed", "error", err)
		case deleted > 0:
			logger.Info("audit entries pruned", "deleted", deleted)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
