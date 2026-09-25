package persistence

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
)

func TestProfileRepositoryDefaultsUpsertAndLookup(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	ctx := t.Context()
	repo := NewProfileRepository(tx)
	userID := insertUser(t, tx)

	view, err := repo.GetView(ctx, userID)
	require.NoError(t, err)
	require.Equal(t, userdomain.DefaultProfile(), view.Profile, "a user without a profile row reads as defaults")
	require.Equal(t, userdomain.DefaultPrivacy(), view.Privacy)
	require.Nil(t, view.AvatarUUID)

	now := time.Now().UTC().Truncate(time.Microsecond)
	display, website := "Ada L.", "https://ada.dev"
	fullName := "Ada Lovelace"
	profile := userdomain.DefaultProfile()
	profile.DisplayName = &display
	profile.Bio = "Hello"
	profile.ContactWebsite = &website
	profile.Social = userdomain.SocialLinks{GitHub: "ada"}
	profile.Timezone = "Asia/Jakarta"
	profile.MarketingConsent = true
	profile.MarketingConsentUpdatedAt = &now

	require.NoError(t, repo.SaveProfile(ctx, userID, &fullName, profile, userID, now))

	view, err = repo.GetViewByUsername(ctx, strings.ToUpper(view.User.Username))
	require.NoError(t, err)
	require.Equal(t, fullName, view.User.FullName)
	require.Equal(t, display, *view.Profile.DisplayName)
	require.Equal(t, "Hello", view.Profile.Bio)
	require.Equal(t, website, *view.Profile.ContactWebsite)
	require.Equal(t, "ada", view.Profile.Social.GitHub)
	require.Equal(t, "Asia/Jakarta", view.Profile.Timezone)
	require.True(t, view.Profile.MarketingConsent)
	require.True(t, now.Equal(*view.Profile.MarketingConsentUpdatedAt))
	require.NotNil(t, view.Profile.UpdatedAt)

	profile.DisplayName = nil
	profile.Bio = ""
	require.NoError(t, repo.SaveProfile(ctx, userID, nil, profile, userID, now.Add(time.Minute)), "second save updates in place")

	view, err = repo.GetView(ctx, userID)
	require.NoError(t, err)
	require.Nil(t, view.Profile.DisplayName)
	require.Empty(t, view.Profile.Bio)
	require.Equal(t, fullName, view.User.FullName, "a nil full name leaves it alone")

	_, err = repo.GetView(ctx, uuid.New())
	require.ErrorIs(t, err, userdomain.ErrNotFound)
	require.ErrorIs(t, repo.SaveProfile(ctx, uuid.New(), nil, profile, userID, now), userdomain.ErrNotFound)
}

func TestProfileRepositorySavePrivacy(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	ctx := t.Context()
	repo := NewProfileRepository(tx)
	userID := insertUser(t, tx)
	now := time.Now().UTC()

	want := userdomain.Privacy{Visibility: userdomain.VisibilityUnlisted, ShowEmail: true}
	require.NoError(t, repo.SavePrivacy(ctx, userID, want, now))

	view, err := repo.GetView(ctx, userID)
	require.NoError(t, err)
	require.Equal(t, want, view.Privacy)

	want = userdomain.Privacy{Visibility: userdomain.VisibilityPrivate, ShowContact: true, AllowIndexing: true}
	require.NoError(t, repo.SavePrivacy(ctx, userID, want, now.Add(time.Minute)))

	view, err = repo.GetView(ctx, userID)
	require.NoError(t, err)
	require.Equal(t, want, view.Privacy, "a second save updates the row")

	require.ErrorIs(t, repo.SavePrivacy(ctx, uuid.New(), want, now), userdomain.ErrNotFound)
}

func TestProfileRepositoryDisplayNameIsUniqueCaseInsensitively(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	ctx := t.Context()
	repo := NewProfileRepository(tx)
	first, second := insertUser(t, tx), insertUser(t, tx)
	now := time.Now()

	name := uniqueSlug("Name")
	profile := userdomain.DefaultProfile()
	profile.DisplayName = &name
	require.NoError(t, repo.SaveProfile(ctx, first, nil, profile, first, now))

	upper := strings.ToUpper(name)
	profile.DisplayName = &upper
	require.ErrorIs(t, repo.SaveProfile(ctx, second, nil, profile, second, now), userdomain.ErrDisplayNameTaken)

	profile.DisplayName = nil
	require.NoError(t, repo.SaveProfile(ctx, second, nil, profile, second, now), "many users may have no display name")
}

func TestProfileRepositoryAvatarAndHiddenUsers(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	ctx := t.Context()
	repo := NewProfileRepository(tx)
	userID := insertUser(t, tx)
	mediaID := insertUUID(t, tx,
		`INSERT INTO media_assets (storage_key, original_filename, content_type, disk, status)
		 VALUES (?, 'a.png', 'image/png', 'minio', 'ready') RETURNING uuid`, uniqueSlug("media/avatar"))
	now := time.Now()

	require.NoError(t, repo.SetAvatar(ctx, userID, &mediaID, now))

	view, err := repo.GetView(ctx, userID)
	require.NoError(t, err)
	require.Equal(t, mediaID, *view.AvatarUUID)

	require.NoError(t, tx.Exec(`UPDATE media_assets SET deleted_at = now() WHERE uuid = ?`, mediaID).Error)

	view, err = repo.GetView(ctx, userID)
	require.NoError(t, err)
	require.Nil(t, view.AvatarUUID, "a deleted asset is not reported as the avatar")

	require.NoError(t, repo.SetAvatar(ctx, userID, nil, now))
	require.ErrorIs(t, repo.SetAvatar(ctx, uuid.New(), nil, now), userdomain.ErrNotFound)

	require.NoError(t, tx.Exec(`UPDATE users SET deleted_at = now() WHERE uuid = ?`, userID).Error)
	_, err = repo.GetView(ctx, userID)
	require.ErrorIs(t, err, userdomain.ErrNotFound, "deleted users have no profile")
}

func TestUserRepositoryUpdateEmailAndPendingTokens(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	ctx := t.Context()
	users := NewUserRepository(tx)
	resets := NewResetTokenRepository(tx)
	userID, otherID := insertUser(t, tx), insertUser(t, tx)
	now := time.Now().UTC().Truncate(time.Microsecond)

	other, err := users.FindByID(ctx, otherID)
	require.NoError(t, err)

	require.NoError(t, tx.SavePoint("email_taken").Error)
	require.ErrorIs(t, users.UpdateEmail(ctx, userID, strings.ToUpper(other.Email), now), authdomain.ErrEmailTaken)
	require.NoError(t, tx.RollbackTo("email_taken").Error)

	newEmail := uniqueSlug("new") + "@example.test"
	require.NoError(t, users.UpdateEmail(ctx, userID, newEmail, now))

	user, err := users.FindByID(ctx, userID)
	require.NoError(t, err)
	require.Equal(t, newEmail, user.Email)
	require.True(t, now.Equal(*user.EmailVerifiedAt))

	token := func(purpose, email string) authdomain.PasswordResetToken {
		raw := uuid.NewString()

		return authdomain.PasswordResetToken{
			UUID: uuid.New(), UserUUID: userID, JTI: raw, TokenHash: "hash-" + raw,
			Purpose: purpose, NewEmail: email, ExpiresAt: now.Add(time.Hour), CreatedAt: now,
		}
	}
	change, reset := token(authdomain.PurposeEmailChange, newEmail), token(authdomain.PurposePasswordReset, "")
	require.NoError(t, resets.Create(ctx, change))
	require.NoError(t, resets.Create(ctx, reset))

	found, err := resets.FindByHash(ctx, change.TokenHash)
	require.NoError(t, err)
	require.Equal(t, newEmail, found.NewEmail)
	require.Equal(t, userID, found.UserUUID)

	require.NoError(t, resets.RevokePending(ctx, userID, authdomain.PurposeEmailChange, now))

	found, err = resets.FindByHash(ctx, change.TokenHash)
	require.NoError(t, err)
	require.NotNil(t, found.UsedAt)

	found, err = resets.FindByHash(ctx, reset.TokenHash)
	require.NoError(t, err)
	require.Nil(t, found.UsedAt, "other purposes are untouched")
	require.Empty(t, found.NewEmail)
}
