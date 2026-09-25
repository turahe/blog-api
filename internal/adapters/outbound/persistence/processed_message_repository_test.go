package persistence

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestProcessedMessagesRunHandlerOncePerConsumer(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewProcessedMessageRepository(tx)
	messageID := uuid.NewString()
	failed := errors.New("smtp down")

	var calls int

	handle := func(fail bool) func(context.Context) error {
		return func(context.Context) error {
			calls++

			if fail {
				return failed
			}

			return nil
		}
	}

	require.ErrorIs(t, repo.Once(t.Context(), "email", messageID, handle(true)), failed)
	require.NoError(t, repo.Once(t.Context(), "email", messageID, handle(false)), "a failed attempt releases the claim")
	require.NoError(t, repo.Once(t.Context(), "email", messageID, handle(false)))
	require.NoError(t, repo.Once(t.Context(), "audit", messageID, handle(false)), "claims are per consumer")
	require.Equal(t, 3, calls)
}

func TestProcessedMessagesPruneBefore(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewProcessedMessageRepository(tx)
	old, fresh := uuid.NewString(), uuid.NewString()

	require.NoError(t, tx.Exec(`INSERT INTO processed_messages (consumer, message_id, processed_at) VALUES
		('email', ?, now() - interval '10 days'), ('email', ?, now())`, old, fresh).Error)

	removed, err := repo.PruneBefore(t.Context(), time.Now().Add(-7*24*time.Hour))
	require.NoError(t, err)
	require.GreaterOrEqual(t, removed, int64(1))

	var left []string
	require.NoError(t, tx.Raw(`SELECT message_id FROM processed_messages WHERE message_id IN (?, ?)`, old, fresh).Scan(&left).Error)
	require.Equal(t, []string{fresh}, left)
}
