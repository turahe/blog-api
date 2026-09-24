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

// PasswordResetTokenModel is the password_reset_tokens row.
type PasswordResetTokenModel struct {
	ID        int64      `gorm:"primaryKey"`
	UUID      uuid.UUID  `gorm:"type:uuid;column:uuid;default:gen_random_uuid()"`
	UserID    int64      `gorm:"column:user_id"`
	JTI       string     `gorm:"column:jti"`
	TokenHash string     `gorm:"column:token_hash"`
	Purpose   string     `gorm:"column:purpose"`
	NewEmail  *string    `gorm:"column:new_email"`
	ExpiresAt time.Time  `gorm:"column:expires_at"`
	UsedAt    *time.Time `gorm:"column:used_at"`
	CreatedAt time.Time  `gorm:"column:created_at"`
	UserUUID  uuid.UUID  `gorm:"column:user_uuid;->"`
}

// TableName returns the password_reset_tokens table name for GORM.
func (PasswordResetTokenModel) TableName() string { return "password_reset_tokens" }

// ResetTokenRepository implements authports.ResetTokenRepository.
type ResetTokenRepository struct {
	db *gorm.DB
}

// NewResetTokenRepository returns a ResetTokenRepository backed by db.
func NewResetTokenRepository(db *gorm.DB) *ResetTokenRepository {
	return &ResetTokenRepository{db: db}
}

// Create stores a hashed password reset token.
func (r *ResetTokenRepository) Create(ctx context.Context, token authdomain.PasswordResetToken) error {
	userID, err := idByUUID(r.db.WithContext(ctx), "users", token.UserUUID)
	if err != nil {
		return err
	}

	model := PasswordResetTokenModel{
		UUID: token.UUID, UserID: userID, JTI: token.JTI, TokenHash: token.TokenHash,
		Purpose: token.Purpose, ExpiresAt: token.ExpiresAt, CreatedAt: token.CreatedAt,
	}
	if token.NewEmail != "" {
		model.NewEmail = &token.NewEmail
	}

	return r.db.WithContext(ctx).Create(&model).Error
}

// FindByHash returns the token with the hash or authdomain.ErrInvalidToken.
func (r *ResetTokenRepository) FindByHash(ctx context.Context, hash string) (authdomain.PasswordResetToken, error) {
	var model PasswordResetTokenModel

	err := r.db.WithContext(ctx).
		Select(withRefs("password_reset_tokens", uuidRef("users", "password_reset_tokens.user_id", "user_uuid"))).
		Where("token_hash = ?", hash).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return authdomain.PasswordResetToken{}, authdomain.ErrInvalidToken
	}

	if err != nil {
		return authdomain.PasswordResetToken{}, fmt.Errorf("find reset token by hash: %w", err)
	}

	token := authdomain.PasswordResetToken{
		ID: model.ID, UUID: model.UUID, UserUUID: model.UserUUID, JTI: model.JTI, TokenHash: model.TokenHash,
		Purpose: model.Purpose, ExpiresAt: model.ExpiresAt, UsedAt: model.UsedAt, CreatedAt: model.CreatedAt,
	}
	if model.NewEmail != nil {
		token.NewEmail = *model.NewEmail
	}

	return token, nil
}

// MarkUsed records that the token was consumed at the given time.
func (r *ResetTokenRepository) MarkUsed(ctx context.Context, id uuid.UUID, at time.Time) error {
	return r.db.WithContext(ctx).Model(&PasswordResetTokenModel{}).Where("uuid = ? AND used_at IS NULL", id).
		Update("used_at", at).Error
}

// RevokePending marks every unused token of purpose for the user as used.
func (r *ResetTokenRepository) RevokePending(ctx context.Context, userID uuid.UUID, purpose string, at time.Time) error {
	return r.db.WithContext(ctx).Model(&PasswordResetTokenModel{}).
		Where("user_id = "+idOf("users")+" AND purpose = ? AND used_at IS NULL", userID, purpose).
		Update("used_at", at).Error
}
