package persistence

import (
	"context"
	"fmt"
	"time"

	"github.com/turahe/blog-api/internal/platform/messaging"
	"gorm.io/gorm"
)

// ProcessedMessageRepository records handled messages so worker consumers are idempotent.
type ProcessedMessageRepository struct {
	db *gorm.DB
	tx *Transactor
}

var _ messaging.Deduper = (*ProcessedMessageRepository)(nil)

// NewProcessedMessageRepository returns a ProcessedMessageRepository over db.
func NewProcessedMessageRepository(db *gorm.DB) *ProcessedMessageRepository {
	return &ProcessedMessageRepository{db: db, tx: NewTransactor(db)}
}

// Once claims (consumer, messageID) and runs fn in the same transaction. A message already
// claimed is skipped. When fn fails the transaction, claim included, rolls back.
func (r *ProcessedMessageRepository) Once(
	ctx context.Context, consumer, messageID string, fn func(ctx context.Context) error,
) error {
	return r.tx.InTx(ctx, func(ctx context.Context) error {
		res := conn(ctx, r.db).Exec(`
			INSERT INTO processed_messages (consumer, message_id) VALUES (?, ?)
			ON CONFLICT (consumer, message_id) DO NOTHING`, consumer, messageID)
		if res.Error != nil {
			return fmt.Errorf("claim message %s for %s: %w", messageID, consumer, res.Error)
		}

		if res.RowsAffected == 0 {
			return nil
		}

		return fn(ctx)
	})
}

// PruneBefore deletes claims older than cutoff and returns how many were removed.
func (r *ProcessedMessageRepository) PruneBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	res := r.db.WithContext(ctx).Exec(`DELETE FROM processed_messages WHERE processed_at < ?`, cutoff)
	if res.Error != nil {
		return 0, fmt.Errorf("prune processed messages: %w", res.Error)
	}

	return res.RowsAffected, nil
}
