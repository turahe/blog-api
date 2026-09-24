package persistence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
	"gorm.io/gorm"
)

// ProfileRepository implements userports.ProfileRepository.
type ProfileRepository struct {
	db *gorm.DB
}

// NewProfileRepository returns a ProfileRepository backed by db.
func NewProfileRepository(db *gorm.DB) *ProfileRepository {
	return &ProfileRepository{db: db}
}

// profileRow is a live user left-joined with profile, privacy, and avatar; joined
// columns are pointers because the rows may not exist yet.
type profileRow struct {
	UserModel

	AvatarUUID                *uuid.UUID
	DisplayName               *string
	Bio                       *string
	ContactWebsite            *string
	ContactLocation           *string
	SocialLinks               *string
	Locale                    *string
	Timezone                  *string
	MarketingConsent          *bool
	MarketingConsentUpdatedAt *time.Time
	ProfileUpdatedAt          *time.Time
	VisibilityProfile         *string
	VisibilityEmail           *bool
	VisibilityContact         *bool
	SearchAllowIndexing       *bool
}

const profileViewSelect = `SELECT u.*,
	a.uuid AS avatar_uuid,
	p.display_name, p.bio, p.contact_website, p.contact_location,
	p.social_links::text AS social_links, p.locale, p.timezone,
	p.marketing_consent, p.marketing_consent_updated_at, p.updated_at AS profile_updated_at,
	s.visibility_profile, s.visibility_email, s.visibility_contact, s.search_allow_indexing
FROM users u
LEFT JOIN user_profiles p ON p.user_id = u.id
LEFT JOIN user_privacy_settings s ON s.user_id = u.id
LEFT JOIN media_assets a ON a.id = u.avatar_id AND a.deleted_at IS NULL
WHERE u.deleted_at IS NULL AND `

// GetView returns the live user with the UUID, or userdomain.ErrNotFound.
func (r *ProfileRepository) GetView(ctx context.Context, userID uuid.UUID) (userdomain.ProfileView, error) {
	return r.view(ctx, "u.uuid = ?", userID)
}

// GetViewByUsername returns the live user with the username (case-insensitive), or userdomain.ErrNotFound.
func (r *ProfileRepository) GetViewByUsername(ctx context.Context, username string) (userdomain.ProfileView, error) {
	return r.view(ctx, "lower(u.username) = ?", strings.ToLower(strings.TrimSpace(username)))
}

func (r *ProfileRepository) view(ctx context.Context, where string, arg any) (userdomain.ProfileView, error) {
	var rows []profileRow
	if err := r.db.WithContext(ctx).Raw(profileViewSelect+where+" LIMIT 1", arg).Scan(&rows).Error; err != nil {
		return userdomain.ProfileView{}, fmt.Errorf("load profile: %w", err)
	}

	if len(rows) == 0 {
		return userdomain.ProfileView{}, userdomain.ErrNotFound
	}

	return mapProfileRow(rows[0])
}

// SaveProfile upserts the profile row and optionally the user's full name in one transaction.
func (r *ProfileRepository) SaveProfile(
	ctx context.Context,
	userID uuid.UUID,
	fullName *string,
	profile userdomain.Profile,
	updatedBy uuid.UUID,
	at time.Time,
) error {
	social, err := json.Marshal(profile.Social)
	if err != nil {
		return fmt.Errorf("encode social links: %w", err)
	}

	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		id, err := idByUUID(tx, "users", userID)
		if errors.Is(err, errUnknownReference) {
			return userdomain.ErrNotFound
		}

		if err != nil {
			return err
		}

		if fullName != nil {
			if err := tx.Exec(`UPDATE users SET full_name = ?, updated_at = ? WHERE id = ?`, *fullName, at, id).Error; err != nil {
				return err
			}
		}

		return tx.Exec(`INSERT INTO user_profiles (user_id, display_name, bio, contact_website, contact_location,
	social_links, locale, timezone, marketing_consent, marketing_consent_updated_at, updated_by, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?::jsonb, ?, ?, ?, ?, `+idOf("users")+`, ?, ?)
ON CONFLICT (user_id) DO UPDATE SET
	display_name = EXCLUDED.display_name, bio = EXCLUDED.bio,
	contact_website = EXCLUDED.contact_website, contact_location = EXCLUDED.contact_location,
	social_links = EXCLUDED.social_links, locale = EXCLUDED.locale, timezone = EXCLUDED.timezone,
	marketing_consent = EXCLUDED.marketing_consent,
	marketing_consent_updated_at = EXCLUDED.marketing_consent_updated_at,
	updated_by = EXCLUDED.updated_by, updated_at = EXCLUDED.updated_at`,
			id, profile.DisplayName, profile.Bio, profile.ContactWebsite, profile.ContactLocation,
			string(social), profile.Locale, profile.Timezone, profile.MarketingConsent,
			profile.MarketingConsentUpdatedAt, updatedBy, at, at).Error
	})
	if isUniqueViolation(err) {
		return userdomain.ErrDisplayNameTaken
	}

	return err
}

// SetAvatar points the live user at the media asset, or clears the avatar when mediaID is nil.
func (r *ProfileRepository) SetAvatar(ctx context.Context, userID uuid.UUID, mediaID *uuid.UUID, at time.Time) error {
	avatar := any(nil)
	if mediaID != nil {
		avatar = gorm.Expr(idOf("media_assets"), *mediaID)
	}

	result := r.db.WithContext(ctx).Model(&UserModel{}).Where("uuid = ?", userID).
		Updates(map[string]any{"avatar_id": avatar, "updated_at": at})
	if result.Error != nil {
		return fmt.Errorf("set avatar: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return userdomain.ErrNotFound
	}

	return nil
}

func mapProfileRow(row profileRow) (userdomain.ProfileView, error) {
	profile := userdomain.DefaultProfile()
	profile.DisplayName = row.DisplayName
	profile.ContactWebsite = row.ContactWebsite
	profile.ContactLocation = row.ContactLocation
	profile.MarketingConsentUpdatedAt = row.MarketingConsentUpdatedAt
	profile.UpdatedAt = row.ProfileUpdatedAt
	assign(&profile.Bio, row.Bio)
	assign(&profile.Locale, row.Locale)
	assign(&profile.Timezone, row.Timezone)
	assign(&profile.MarketingConsent, row.MarketingConsent)

	if row.SocialLinks != nil {
		if err := json.Unmarshal([]byte(*row.SocialLinks), &profile.Social); err != nil {
			return userdomain.ProfileView{}, fmt.Errorf("decode social links: %w", err)
		}
	}

	privacy := userdomain.DefaultPrivacy()
	if row.VisibilityProfile != nil {
		privacy.Visibility = userdomain.Visibility(*row.VisibilityProfile)
	}

	assign(&privacy.ShowEmail, row.VisibilityEmail)
	assign(&privacy.ShowContact, row.VisibilityContact)
	assign(&privacy.AllowIndexing, row.SearchAllowIndexing)

	return userdomain.ProfileView{
		User:       mapUser(row.UserModel),
		AvatarUUID: row.AvatarUUID,
		Profile:    profile,
		Privacy:    privacy,
	}, nil
}

func assign[T any](dst, src *T) {
	if src != nil {
		*dst = *src
	}
}
