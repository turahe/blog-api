package persistence

import (
	"context"
	"errors"
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

	return r.db.WithContext(ctx).Create(&PasswordResetTokenModel{
		UUID: token.UUID, UserID: userID, JTI: token.JTI, TokenHash: token.TokenHash,
		Purpose: token.Purpose, ExpiresAt: token.ExpiresAt, CreatedAt: token.CreatedAt,
	}).Error
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
		return authdomain.PasswordResetToken{}, err
	}

	return authdomain.PasswordResetToken{
		ID: model.ID, UUID: model.UUID, UserUUID: model.UserUUID, JTI: model.JTI, TokenHash: model.TokenHash,
		Purpose: model.Purpose, ExpiresAt: model.ExpiresAt, UsedAt: model.UsedAt, CreatedAt: model.CreatedAt,
	}, nil
}

// MarkUsed records that the token was consumed at the given time.
func (r *ResetTokenRepository) MarkUsed(ctx context.Context, id uuid.UUID, at time.Time) error {
	return r.db.WithContext(ctx).Model(&PasswordResetTokenModel{}).Where("uuid = ? AND used_at IS NULL", id).
		Update("used_at", at).Error
}
