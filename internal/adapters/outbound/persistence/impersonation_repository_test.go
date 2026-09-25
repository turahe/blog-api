package persistence

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	impdomain "github.com/turahe/blog-api/internal/core/impersonation/domain"
)

func TestImpersonationRepositoryLifecycle(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	ctx := t.Context()
	repo := NewImpersonationRepository(tx)
	actor, target := insertUser(t, tx), insertUser(t, tx)
	now := time.Now().UTC().Truncate(time.Microsecond)

	session := impdomain.Session{
		UUID: uuid.New(), ActorUUID: actor, TargetUUID: target, State: impdomain.StateActive,
		Reason: "ticket #1 cannot publish", IP: "203.0.113.9", StartedAt: now, ExpiresAt: now.Add(time.Hour),
	}
	require.NoError(t, repo.Create(ctx, session))

	second := session
	second.UUID = uuid.New()
	require.ErrorIs(t, repo.Create(ctx, second), impdomain.ErrAlreadyActive, "one active session per actor")

	got, err := repo.Get(ctx, session.UUID)
	require.NoError(t, err)
	assert.Equal(t, actor, got.ActorUUID)
	assert.Equal(t, target, got.TargetUUID)
	assert.Equal(t, "203.0.113.9", got.IP)
	assert.Empty(t, got.UserAgent)
	assert.True(t, got.ParticipantsActive)
	assert.WithinDuration(t, session.ExpiresAt, got.ExpiresAt, 0)

	active, ok, err := repo.ActiveForActor(ctx, actor)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, session.UUID, active.UUID)

	expired, err := repo.Expired(ctx, now.Add(30*time.Minute), 10)
	require.NoError(t, err)
	assert.Empty(t, expired)

	expired, err = repo.Expired(ctx, now.Add(time.Hour), 10)
	require.NoError(t, err)
	require.Len(t, expired, 1)

	require.NoError(t, tx.Exec(`UPDATE users SET status = 'suspended' WHERE uuid = ?`, target).Error)

	got, err = repo.Get(ctx, session.UUID)
	require.NoError(t, err)
	assert.False(t, got.ParticipantsActive)

	changed, err := repo.End(ctx, session.UUID, impdomain.StateExited, impdomain.EndManual, now.Add(time.Minute))
	require.NoError(t, err)
	assert.True(t, changed)

	changed, err = repo.End(ctx, session.UUID, impdomain.StateExpired, impdomain.EndExpired, now.Add(2*time.Minute))
	require.NoError(t, err)
	assert.False(t, changed, "an ended session stays ended")

	got, err = repo.Get(ctx, session.UUID)
	require.NoError(t, err)
	assert.Equal(t, impdomain.StateExited, got.State)
	require.NotNil(t, got.EndReason)
	assert.Equal(t, impdomain.EndManual, *got.EndReason)
	require.NotNil(t, got.EndedAt)

	_, ok, err = repo.ActiveForActor(ctx, actor)
	require.NoError(t, err)
	assert.False(t, ok)

	require.NoError(t, repo.Create(ctx, second), "a new session after the previous one ended")

	_, err = repo.Get(ctx, uuid.New())
	require.ErrorIs(t, err, impdomain.ErrNotFound)
}
