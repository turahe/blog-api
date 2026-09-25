package persistence

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	commentdomain "github.com/turahe/blog-api/internal/core/comment/domain"
	privacydomain "github.com/turahe/blog-api/internal/core/privacy/domain"
	"gorm.io/gorm"
)

func newPrivacyRequest(userID uuid.UUID, kind privacydomain.Kind, at time.Time) privacydomain.Request {
	return privacydomain.Request{
		UUID: uuid.New(), UserUUID: userID, Kind: kind, Status: privacydomain.StatusPending, CreatedAt: at,
	}
}

func TestPrivacyRepositoryQueue(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	ctx := t.Context()
	repo := NewPrivacyRepository(tx)
	userID := insertUser(t, tx)
	now := time.Now().UTC().Truncate(time.Microsecond)

	_, err := repo.Latest(ctx, userID, privacydomain.KindExport)
	require.ErrorIs(t, err, privacydomain.ErrNotFound)

	export := newPrivacyRequest(userID, privacydomain.KindExport, now.Add(-time.Hour))
	require.NoError(t, repo.Create(ctx, export))
	require.ErrorIs(t, repo.Create(ctx, newPrivacyRequest(userID, privacydomain.KindExport, now)), privacydomain.ErrAlreadyOpen)
	require.NoError(t, repo.Create(ctx, newPrivacyRequest(userID, privacydomain.KindErase, now)), "kinds queue independently")

	claimed, ok, err := repo.ClaimNext(ctx, now, now.Add(-15*time.Minute))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, export.UUID, claimed.UUID, "oldest first")
	require.Equal(t, userID, claimed.UserUUID)
	require.Equal(t, privacydomain.StatusRunning, claimed.Status)
	require.Equal(t, 1, claimed.Attempts)

	require.NoError(t, repo.Fail(ctx, export.UUID, "boom", false, now))
	latest, err := repo.Latest(ctx, userID, privacydomain.KindExport)
	require.NoError(t, err)
	require.Equal(t, privacydomain.StatusPending, latest.Status)
	require.Equal(t, "boom", *latest.LastError)

	key, expires := "privacy-exports/x.json", now.Add(time.Hour)
	require.NoError(t, repo.Complete(ctx, export.UUID, &key, &expires, now))

	archives, err := repo.Archives(ctx, &userID, time.Time{}, 10)
	require.NoError(t, err)
	require.Len(t, archives, 1)
	require.Equal(t, key, *archives[0].StorageKey)

	archives, err = repo.Archives(ctx, &userID, now, 10)
	require.NoError(t, err)
	require.Empty(t, archives, "not expired yet")

	require.NoError(t, repo.ClearArchive(ctx, export.UUID))
	latest, err = repo.Latest(ctx, userID, privacydomain.KindExport)
	require.NoError(t, err)
	require.Equal(t, privacydomain.StatusCompleted, latest.Status)
	require.Nil(t, latest.StorageKey)
	require.Nil(t, latest.LastError)

	require.NoError(t, repo.Create(ctx, newPrivacyRequest(userID, privacydomain.KindExport, now)), "a completed request frees the slot")
	require.ErrorIs(t, repo.ClearArchive(ctx, uuid.New()), privacydomain.ErrNotFound)
}

func TestPrivacyDataExportAndErase(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	ctx := t.Context()
	data := NewPrivacyData(tx)
	userID := insertUser(t, tx)
	now := time.Now().UTC()

	seedPersonalData(t, tx, userID)

	raw, err := data.ExportUser(ctx, userID, now)
	require.NoError(t, err)

	var doc struct {
		Account struct {
			ID    uuid.UUID `json:"id"`
			Email string    `json:"email"`
		} `json:"account"`
		Profile  map[string]any   `json:"profile"`
		Activity []map[string]any `json:"activity"`
		Sessions []map[string]any `json:"sessions"`
		Comments []map[string]any `json:"comments"`
		Consents []map[string]any `json:"analytics_consents"`
	}
	require.NoError(t, json.Unmarshal(raw, &doc))
	require.Equal(t, userID, doc.Account.ID)
	require.Contains(t, doc.Account.Email, "@example.test")
	require.Equal(t, "Hello", doc.Profile["bio"])
	require.Len(t, doc.Activity, 2)
	require.Len(t, doc.Sessions, 1)
	require.Len(t, doc.Comments, 1)
	require.Len(t, doc.Consents, 1)

	require.NoError(t, data.EraseUser(ctx, userID, now))
	require.NoError(t, data.EraseUser(ctx, userID, now), "erasing again is harmless")

	var user struct {
		Email, Username, FullName, Status string
		PasswordHash                      *string
	}
	require.NoError(t, tx.Raw(`SELECT email, username, full_name, status, password_hash FROM users WHERE uuid = ?`, userID).
		Scan(&user).Error)
	require.Equal(t, "erased+"+userID.String()+"@invalid", user.Email)
	require.Equal(t, ErasedFullName, user.FullName)
	require.Equal(t, "deleted", user.Status)
	require.Nil(t, user.PasswordHash)

	for query, want := range map[string]int64{
		`SELECT count(*) FROM audit_logs WHERE actor_id = ` + idOf("users"):                                 1,
		`SELECT count(*) FROM audit_logs WHERE actor_id = ` + idOf("users") + ` AND ip_address IS NOT NULL`: 0,
		`SELECT count(*) FROM refresh_sessions WHERE user_id = ` + idOf("users"):                            0,
		`SELECT count(*) FROM user_profiles WHERE user_id = ` + idOf("users"):                               0,
		`SELECT count(*) FROM consent_subjects WHERE user_id = ` + idOf("users"):                            0,
		`SELECT count(*) FROM comments WHERE author_id = ` + idOf("users"):                                  1,
		`SELECT count(*) FROM comments WHERE author_id = ` + idOf("users") + ` AND user_agent IS NOT NULL`:  0,
	} {
		var got int64
		require.NoError(t, tx.Raw(query, userID).Row().Scan(&got))
		require.Equal(t, want, got, query)
	}

	comments, err := NewCommentRepository(tx).List(ctx, commentdomain.ListFilter{AuthorUUID: &userID, Page: 1, PerPage: 10})
	require.NoError(t, err)
	require.Len(t, comments.Items, 1)
	require.Equal(t, ErasedFullName, comments.Items[0].AuthorUsername, "erased authors show as Deleted user")

	_, err = data.ExportUser(ctx, uuid.New(), now)
	require.ErrorIs(t, err, errUserGone)
	require.ErrorIs(t, data.EraseUser(ctx, uuid.New(), now), errUserGone)
}

// seedPersonalData gives the user a profile, activity, an admin audit row, a session, a
// comment, and a consent.
func seedPersonalData(t *testing.T, tx *gorm.DB, userID uuid.UUID) {
	t.Helper()

	postID := insertPublishedPost(t, tx)
	statements := []string{
		`INSERT INTO user_profiles (user_id, bio) VALUES (` + idOf("users") + `, 'Hello')`,
		`INSERT INTO audit_logs (actor_id, action, category, ip_address) VALUES (` + idOf("users") + `, 'auth.login', 'auth', '203.0.113.9')`,
		`INSERT INTO audit_logs (actor_id, action, ip_address) VALUES (` + idOf("users") + `, 'admin.settings.update', '203.0.113.9')`,
		`INSERT INTO refresh_sessions (user_id, family_id, token_hash, expires_at, ip_address)
			VALUES (` + idOf("users") + `, gen_random_uuid(), md5(random()::text), now() + interval '1 day', '203.0.113.9')`,
		`INSERT INTO consent_subjects (token_hash, user_id) VALUES (encode(sha256(random()::text::bytea), 'hex'), ` + idOf("users") + `)`,
	}

	for _, statement := range statements {
		require.NoError(t, tx.Exec(statement, userID).Error, statement)
	}

	require.NoError(t, tx.Exec(`INSERT INTO analytics_consents (subject_id, purpose, status, policy_version, decided_at)
		SELECT id, 'analytics', 'granted', 'v1', now() FROM consent_subjects WHERE user_id = `+idOf("users"), userID).Error)
	require.NoError(t, tx.Exec(`INSERT INTO comments (post_id, author_id, content, status, user_agent)
		VALUES (`+idOf("posts")+`, `+idOf("users")+`, 'Nice post', 'approved', 'Firefox')`, postID, userID).Error)
}
