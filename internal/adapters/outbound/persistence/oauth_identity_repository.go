package persistence

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
	"gorm.io/gorm"
)

// OAuthIdentityModel maps user_oauth_identities.
type OAuthIdentityModel struct {
	ID         int64
	UserID     int64
	Provider   string
	Subject    string
	Email      *string
	CreatedAt  time.Time
	LastUsedAt *time.Time
}

// TableName implements gorm's tabler.
func (OAuthIdentityModel) TableName() string { return "user_oauth_identities" }

// OAuthIdentityRepository implements authports.OAuthIdentityRepository.
type OAuthIdentityRepository struct {
	db *gorm.DB
}

// NewOAuthIdentityRepository returns an OAuthIdentityRepository backed by db.
func NewOAuthIdentityRepository(db *gorm.DB) *OAuthIdentityRepository {
	return &OAuthIdentityRepository{db: db}
}

// FindUser returns the uuid of the live user linked to provider and subject.
func (r *OAuthIdentityRepository) FindUser(ctx context.Context, provider, subject string) (uuid.UUID, error) {
	var ids []uuid.UUID

	err := conn(ctx, r.db).Table("user_oauth_identities i").
		Joins("JOIN users u ON u.id = i.user_id AND u.deleted_at IS NULL").
		Where("i.provider = ? AND i.subject = ?", provider, subject).
		Limit(1).Pluck("u.uuid", &ids).Error
	if err != nil {
		return uuid.Nil, fmt.Errorf("find oauth identity: %w", err)
	}

	if len(ids) == 0 {
		return uuid.Nil, authdomain.ErrOAuthNoAccount
	}

	return ids[0], nil
}

// Link stores identity for the user.
func (r *OAuthIdentityRepository) Link(ctx context.Context, userID uuid.UUID, identity authdomain.OAuthIdentity, at time.Time) error {
	id, err := idByUUID(conn(ctx, r.db), "users", userID)
	if errors.Is(err, errUnknownReference) {
		return authdomain.ErrOAuthNoAccount
	}

	if err != nil {
		return err
	}

	model := OAuthIdentityModel{
		UserID: id, Provider: identity.Provider, Subject: identity.Subject, CreatedAt: at, LastUsedAt: &at,
	}
	if identity.Email != "" {
		model.Email = &identity.Email
	}

	// A savepoint keeps an enclosing transaction usable after a unique violation.
	err = conn(ctx, r.db).Transaction(func(tx *gorm.DB) error { return tx.Create(&model).Error })
	if err != nil {
		if IsUniqueViolation(err) {
			return authdomain.ErrOAuthLinkConflict
		}

		return fmt.Errorf("link oauth identity: %w", err)
	}

	return nil
}

// Touch records a sign-in with the identity.
func (r *OAuthIdentityRepository) Touch(ctx context.Context, provider, subject string, at time.Time) error {
	return conn(ctx, r.db).Model(&OAuthIdentityModel{}).
		Where("provider = ? AND subject = ?", provider, subject).
		Update("last_used_at", at).Error
}
