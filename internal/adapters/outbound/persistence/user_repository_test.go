package persistence

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
	"gorm.io/gorm"
)

func newUser(mutate func(*userdomain.User)) userdomain.User {
	name := uniqueSlug("usr")
	now := time.Now().UTC()

	user := userdomain.User{
		UUID: uuid.New(), Email: name + "@Example.Test", Username: name, FullName: name,
		Status: userdomain.StatusActive, CreatedAt: now, UpdatedAt: now,
	}
	if mutate != nil {
		mutate(&user)
	}

	return user
}

func createUser(t *testing.T, repo *UserRepository, mutate func(*userdomain.User)) userdomain.User {
	t.Helper()

	user, err := repo.Create(t.Context(), newUser(mutate))
	require.NoError(t, err)

	return user
}

// inSavepoint runs fn in a savepoint rolled back afterwards, so a statement that fails
// (and aborts the transaction) leaves tx usable.
func inSavepoint(t *testing.T, tx *gorm.DB, fn func()) {
	t.Helper()

	require.NoError(t, tx.SavePoint("expect_failure").Error)
	fn()
	require.NoError(t, tx.RollbackTo("expect_failure").Error)
}

func TestUserRepositoryFinders(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewUserRepository(tx)
	ctx := t.Context()

	user := createUser(t, repo, nil)
	deleted := createUser(t, repo, nil)
	require.NoError(t, tx.Exec("UPDATE users SET deleted_at = now() WHERE uuid = ?", deleted.UUID).Error)

	tests := []struct {
		name    string
		find    func() (userdomain.User, error)
		wantErr error
	}{
		{name: "email ignores case", find: func() (userdomain.User, error) { return repo.FindByEmail(ctx, strings.ToUpper(user.Email)) }},
		{name: "id", find: func() (userdomain.User, error) { return repo.FindByID(ctx, user.UUID) }},
		{name: "identity matches username", find: func() (userdomain.User, error) {
			return repo.FindByUsernameOrEmail(ctx, "  "+strings.ToUpper(user.Username)+" ")
		}},
		{name: "identity matches email", find: func() (userdomain.User, error) { return repo.FindByUsernameOrEmail(ctx, user.Email) }},
		{name: "unknown email", find: func() (userdomain.User, error) { return repo.FindByEmail(ctx, "missing-"+user.Email) }, wantErr: userdomain.ErrNotFound},
		{name: "unknown id", find: func() (userdomain.User, error) { return repo.FindByID(ctx, uuid.New()) }, wantErr: userdomain.ErrNotFound},
		{name: "unknown identity", find: func() (userdomain.User, error) {
			return repo.FindByUsernameOrEmail(ctx, uniqueSlug("nobody"))
		}, wantErr: userdomain.ErrNotFound},
		{name: "soft-deleted", find: func() (userdomain.User, error) { return repo.FindByID(ctx, deleted.UUID) }, wantErr: userdomain.ErrNotFound},
		{name: "database error", find: func() (userdomain.User, error) {
			return repo.FindByID(canceledContext(t), user.UUID)
		}, wantErr: context.Canceled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.find()
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			require.Equal(t, user.UUID, got.UUID)
			require.Equal(t, user.Email, got.Email)
			require.Equal(t, userdomain.StatusActive, got.Status)
			require.Nil(t, got.DeletedAt)
		})
	}
}

func TestUserRepositoryRecordLogin(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewUserRepository(tx)
	user := createUser(t, repo, nil)
	at := time.Now().UTC().Truncate(time.Microsecond)

	require.NoError(t, repo.RecordLogin(t.Context(), user.UUID, at))
	require.NoError(t, repo.RecordLogin(t.Context(), user.UUID, at.Add(time.Minute)))

	got, err := repo.FindByID(t.Context(), user.UUID)
	require.NoError(t, err)
	require.Equal(t, 2, got.LoginCount)
	require.NotNil(t, got.LastLoginAt)
	require.WithinDuration(t, at.Add(time.Minute), *got.LastLoginAt, 0)
}

func TestUserRepositoryCreate(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewUserRepository(tx)
	existing := createUser(t, repo, func(u *userdomain.User) { u.PasswordHash = "argon2-hash" })

	got, err := repo.FindByID(t.Context(), existing.UUID)
	require.NoError(t, err)
	require.Equal(t, "argon2-hash", got.PasswordHash)
	require.NotZero(t, existing.ID)

	passwordless := createUser(t, repo, nil)
	got, err = repo.FindByID(t.Context(), passwordless.UUID)
	require.NoError(t, err)
	require.Empty(t, got.PasswordHash)

	tests := []struct {
		name    string
		mutate  func(*userdomain.User)
		wantErr error
	}{
		{name: "email taken ignoring case", mutate: func(u *userdomain.User) { u.Email = strings.ToUpper(existing.Email) }, wantErr: authdomain.ErrEmailTaken},
		{name: "username taken ignoring case", mutate: func(u *userdomain.User) { u.Username = strings.ToUpper(existing.Username) }, wantErr: userdomain.ErrUsernameTaken},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inSavepoint(t, tx, func() {
				_, err := repo.Create(t.Context(), newUser(tt.mutate))
				require.ErrorIs(t, err, tt.wantErr)
			})
		})
	}

	t.Run("other constraint", func(t *testing.T) {
		inSavepoint(t, tx, func() {
			_, err := repo.Create(t.Context(), newUser(func(u *userdomain.User) { u.Status = "bogus" }))
			require.Error(t, err)
			require.NotErrorIs(t, err, authdomain.ErrEmailTaken)
			require.NotErrorIs(t, err, userdomain.ErrUsernameTaken)
		})
	})
}

func TestUserRepositoryUpdatePassword(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewUserRepository(tx)
	user := createUser(t, repo, nil)
	changed := time.Now().UTC().Truncate(time.Microsecond)

	require.NoError(t, repo.UpdatePassword(t.Context(), user.UUID, "new-hash", changed))

	got, err := repo.FindByID(t.Context(), user.UUID)
	require.NoError(t, err)
	require.Equal(t, "new-hash", got.PasswordHash)
	require.NotNil(t, got.PasswordChangedAt)
	require.WithinDuration(t, changed, *got.PasswordChangedAt, 0)
	require.WithinDuration(t, changed, got.UpdatedAt, 0)
}

func TestUserRepositoryUpdateEmail(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewUserRepository(tx)
	user, other := createUser(t, repo, nil), createUser(t, repo, nil)
	verified := time.Now().UTC().Truncate(time.Microsecond)

	inSavepoint(t, tx, func() {
		require.ErrorIs(t, repo.UpdateEmail(t.Context(), user.UUID, strings.ToUpper(other.Email), verified), authdomain.ErrEmailTaken)
	})

	email := uniqueSlug("moved") + "@example.test"
	require.NoError(t, repo.UpdateEmail(t.Context(), user.UUID, email, verified))

	got, err := repo.FindByEmail(t.Context(), email)
	require.NoError(t, err)
	require.Equal(t, user.UUID, got.UUID)
	require.NotNil(t, got.EmailVerifiedAt)
	require.WithinDuration(t, verified, *got.EmailVerifiedAt, 0)
}

func TestUserRepositoryListPaginatesNewestFirst(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewUserRepository(tx)
	ctx := t.Context()

	// Dated a century ahead so these rows lead the list whatever else the database holds.
	base := time.Now().UTC().AddDate(100, 0, 0)
	at := func(minutes int) func(*userdomain.User) {
		return func(u *userdomain.User) {
			u.CreatedAt = base.Add(time.Duration(minutes) * time.Minute)
			u.UpdatedAt = u.CreatedAt
		}
	}

	oldest, middle, newest := createUser(t, repo, at(1)), createUser(t, repo, at(2)), createUser(t, repo, at(3))
	deleted := createUser(t, repo, at(4))
	require.NoError(t, tx.Exec("UPDATE users SET deleted_at = now() WHERE uuid = ?", deleted.UUID).Error)

	first, total, err := repo.List(ctx, 1, 2)
	require.NoError(t, err)
	require.GreaterOrEqual(t, total, int64(3))
	require.Equal(t, []uuid.UUID{newest.UUID, middle.UUID}, userIDs(first), "deleted users are excluded")

	second, _, err := repo.List(ctx, 2, 2)
	require.NoError(t, err)
	require.NotEmpty(t, second)
	require.Equal(t, oldest.UUID, second[0].UUID)

	_, _, err = repo.List(canceledContext(t), 1, 2)
	require.ErrorIs(t, err, context.Canceled)
}

func userIDs(users []userdomain.User) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(users))
	for _, u := range users {
		out = append(out, u.UUID)
	}

	return out
}

func TestUserRepositoryListRoleNames(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewUserRepository(tx)
	user, roleless := createUser(t, repo, nil), createUser(t, repo, nil)

	roles := []string{uniqueSlug("role"), uniqueSlug("role")}
	for _, role := range roles {
		require.NoError(t, tx.Exec("INSERT INTO roles (name) VALUES (?)", role).Error)
		require.NoError(t, tx.Exec(
			"INSERT INTO user_roles (user_id, role_id) VALUES ("+idOf("users")+", (SELECT id FROM roles WHERE name = ?))",
			user.UUID, role,
		).Error)
	}

	names, err := repo.ListRoleNames(t.Context(), user.UUID)
	require.NoError(t, err)
	require.ElementsMatch(t, roles, names)

	names, err = repo.ListRoleNames(t.Context(), roleless.UUID)
	require.NoError(t, err)
	require.Empty(t, names)
}

func newSession(user uuid.UUID, mutate func(*authdomain.RefreshSession)) authdomain.RefreshSession {
	now := time.Now().UTC().Truncate(time.Microsecond)

	session := authdomain.RefreshSession{
		UUID: uuid.New(), UserUUID: user, FamilyID: uuid.New(), TokenHash: uniqueSlug("hash"),
		ExpiresAt: now.Add(time.Hour), CreatedAt: now,
	}
	if mutate != nil {
		mutate(&session)
	}

	return session
}

func createSession(t *testing.T, repo *SessionRepository, user uuid.UUID, mutate func(*authdomain.RefreshSession)) authdomain.RefreshSession {
	t.Helper()

	session, err := repo.Create(t.Context(), newSession(user, mutate))
	require.NoError(t, err)

	return session
}

func findSession(t *testing.T, repo *SessionRepository, session authdomain.RefreshSession) authdomain.RefreshSession {
	t.Helper()

	got, err := repo.FindByTokenHash(t.Context(), session.TokenHash)
	require.NoError(t, err)

	return got
}

func TestSessionRepositoryCreateAndFindByTokenHash(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewSessionRepository(tx)
	ctx := t.Context()
	user := insertUser(t, tx)

	withClient := createSession(t, repo, user, func(s *authdomain.RefreshSession) {
		s.UserAgent, s.IPAddress = "Firefox", "203.0.113.7"
	})
	require.NotZero(t, withClient.ID)

	got := findSession(t, repo, withClient)
	require.Equal(t, withClient.UUID, got.UUID)
	require.Equal(t, user, got.UserUUID)
	require.Equal(t, withClient.FamilyID, got.FamilyID)
	require.Equal(t, "Firefox", got.UserAgent)
	require.Equal(t, "203.0.113.7", got.IPAddress)
	require.WithinDuration(t, withClient.ExpiresAt, got.ExpiresAt, 0)
	require.Nil(t, got.RevokedAt)
	require.Nil(t, got.ReplacedByUUID)

	bare := findSession(t, repo, createSession(t, repo, user, nil))
	require.Empty(t, bare.UserAgent)
	require.Empty(t, bare.IPAddress)

	_, err := repo.FindByTokenHash(ctx, uniqueSlug("missing"))
	require.ErrorIs(t, err, authdomain.ErrInvalidToken)

	_, err = repo.FindByTokenHash(canceledContext(t), withClient.TokenHash)
	require.ErrorIs(t, err, context.Canceled)

	_, err = repo.Create(ctx, newSession(uuid.New(), nil))
	require.ErrorIs(t, err, errUnknownReference)

	inSavepoint(t, tx, func() {
		_, err := repo.Create(ctx, newSession(user, func(s *authdomain.RefreshSession) { s.TokenHash = withClient.TokenHash }))
		require.True(t, IsUniqueViolation(err), "token hashes are unique: %v", err)
	})
}

func TestSessionRepositoryRevocation(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewSessionRepository(tx)
	ctx := t.Context()
	user, other := insertUser(t, tx), insertUser(t, tx)
	family := uuid.New()
	inFamily := func(s *authdomain.RefreshSession) { s.FamilyID = family }
	at := time.Now().UTC().Truncate(time.Microsecond)

	sibling1, sibling2 := createSession(t, repo, user, inFamily), createSession(t, repo, user, inFamily)
	single := createSession(t, repo, user, nil)
	rotated, successor := createSession(t, repo, user, nil), createSession(t, repo, user, nil)
	othersFamily := createSession(t, repo, other, inFamily)

	require.NoError(t, repo.Revoke(ctx, single.UUID, at))
	require.NotNil(t, findSession(t, repo, single).RevokedAt)

	require.NoError(t, repo.RevokeFamily(ctx, user, family, at))

	for _, s := range []authdomain.RefreshSession{sibling1, sibling2} {
		got := findSession(t, repo, s)
		require.NotNil(t, got.RevokedAt)
		require.WithinDuration(t, at, *got.RevokedAt, 0)
	}

	require.Nil(t, findSession(t, repo, othersFamily).RevokedAt, "a family is scoped to its user")

	require.NoError(t, repo.Replace(ctx, rotated.UUID, successor.UUID, at))
	got := findSession(t, repo, rotated)
	require.NotNil(t, got.RevokedAt)
	require.Equal(t, &successor.UUID, got.ReplacedByUUID)
	require.Nil(t, findSession(t, repo, successor).RevokedAt)

	require.NoError(t, repo.RevokeAllForUser(ctx, user, at))
	require.NotNil(t, findSession(t, repo, successor).RevokedAt)
	require.Nil(t, findSession(t, repo, othersFamily).RevokedAt)
}

func TestSessionRepositoryPruneExpiredBefore(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewSessionRepository(tx)
	user := insertUser(t, tx)

	// A century back, so no other row can fall before the cutoff.
	expiry := time.Now().UTC().AddDate(-100, 0, 0).Truncate(time.Microsecond)
	expired := createSession(t, repo, user, func(s *authdomain.RefreshSession) {
		s.CreatedAt, s.ExpiresAt = expiry.Add(-time.Hour), expiry
	})
	live := createSession(t, repo, user, nil)

	pruned, err := repo.PruneExpiredBefore(t.Context(), expiry.Add(time.Second))
	require.NoError(t, err)
	require.Equal(t, int64(1), pruned)

	_, err = repo.FindByTokenHash(t.Context(), expired.TokenHash)
	require.ErrorIs(t, err, authdomain.ErrInvalidToken)
	findSession(t, repo, live)

	_, err = repo.PruneExpiredBefore(canceledContext(t), expiry)
	require.ErrorIs(t, err, context.Canceled)
}
