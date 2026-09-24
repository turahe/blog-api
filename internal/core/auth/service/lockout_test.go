package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
)

type memAttempts struct {
	max      int
	lockFor  time.Duration
	failures map[string]int
	locked   map[string]bool
	err      error
}

func newMemAttempts(max int) *memAttempts {
	return &memAttempts{max: max, lockFor: time.Minute, failures: map[string]int{}, locked: map[string]bool{}}
}

func (m *memAttempts) Locked(_ context.Context, key string) (time.Duration, error) {
	if m.err != nil {
		return 0, m.err
	}

	if m.locked[key] {
		return m.lockFor, nil
	}

	return 0, nil
}

func (m *memAttempts) Fail(_ context.Context, key string) (time.Duration, error) {
	if m.err != nil {
		return 0, m.err
	}

	m.failures[key]++
	if m.failures[key] >= m.max {
		m.locked[key] = true
		m.failures[key] = 0

		return m.lockFor, nil
	}

	return 0, nil
}

func (m *memAttempts) Reset(_ context.Context, key string) error {
	delete(m.failures, key)
	return nil
}

func lockoutFixture(t *testing.T, attempts *memAttempts) (*memUsers, func(email, password string) error) {
	t.Helper()

	user := userdomain.User{
		UUID: uuid.New(), Email: "a@example.com", Username: "a",
		PasswordHash: "hash:secret", Status: userdomain.StatusActive,
	}
	users := &memUsers{byEmail: map[string]userdomain.User{user.Email: user}, byID: map[uuid.UUID]userdomain.User{user.UUID: user}}
	sessions := &memSessions{byHash: map[string]authdomain.RefreshSession{}, byID: map[uuid.UUID]authdomain.RefreshSession{}}
	resets := &memResets{byHash: map[string]authdomain.PasswordResetToken{}, byID: map[uuid.UUID]authdomain.PasswordResetToken{}}
	svc := newService(users, sessions, resets, nil).WithLoginAttempts(attempts)

	return users, func(email, password string) error {
		_, err := svc.Login(t.Context(), email, password, "ua", "127.0.0.1", false)
		return err
	}
}

func TestLoginLocksAfterRepeatedFailures(t *testing.T) {
	t.Parallel()

	attempts := newMemAttempts(3)
	_, login := lockoutFixture(t, attempts)

	require.ErrorIs(t, login("a@example.com", "wrong"), authdomain.ErrInvalidCredentials)
	require.ErrorIs(t, login("A@example.com ", "wrong"), authdomain.ErrInvalidCredentials)

	err := login("a@example.com", "wrong")
	require.ErrorIs(t, err, authdomain.ErrAccountLocked)

	var locked authdomain.LockedError
	require.ErrorAs(t, err, &locked)
	require.Equal(t, time.Minute, locked.RetryAfter)

	require.ErrorIs(t, login("a@example.com", "secret"), authdomain.ErrAccountLocked, "correct password is refused while locked")
}

func TestLoginLocksUnknownEmailsToo(t *testing.T) {
	t.Parallel()

	attempts := newMemAttempts(2)
	_, login := lockoutFixture(t, attempts)

	require.ErrorIs(t, login("ghost@example.com", "x"), authdomain.ErrInvalidCredentials)
	require.ErrorIs(t, login("ghost@example.com", "x"), authdomain.ErrAccountLocked)
}

func TestLoginSuccessResetsFailures(t *testing.T) {
	t.Parallel()

	attempts := newMemAttempts(3)
	_, login := lockoutFixture(t, attempts)

	require.Error(t, login("a@example.com", "wrong"))
	require.Error(t, login("a@example.com", "wrong"))
	require.NoError(t, login("a@example.com", "secret"))
	require.Zero(t, attempts.failures["email:a@example.com"])
	require.ErrorIs(t, login("a@example.com", "wrong"), authdomain.ErrInvalidCredentials)
}

func TestLoginLockoutFailsOpen(t *testing.T) {
	t.Parallel()

	attempts := newMemAttempts(1)
	attempts.err = errors.New("redis down")
	_, login := lockoutFixture(t, attempts)

	require.ErrorIs(t, login("a@example.com", "wrong"), authdomain.ErrInvalidCredentials)
	require.NoError(t, login("a@example.com", "secret"))
}
