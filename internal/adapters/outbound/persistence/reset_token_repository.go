package persistence

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
	"gorm.io/gorm"
)

type PasswordResetTokenModel struct {
	ID        uuid.UUID  `gorm:"type:uuid;primaryKey"`
	UserID    uuid.UUID  `gorm:"type:uuid;column:user_id"`
	JTI       string     `gorm:"column:jti"`
	TokenHash string     `gorm:"column:token_hash"`
	Purpose   string     `gorm:"column:purpose"`
	ExpiresAt time.Time  `gorm:"column:expires_at"`
	UsedAt    *time.Time `gorm:"column:used_at"`
	CreatedAt time.Time  `gorm:"column:created_at"`
}

func (PasswordResetTokenModel) TableName() string { return "password_reset_tokens" }

type ResetTokenRepository struct {
	db *gorm.DB
}

func NewResetTokenRepository(db *gorm.DB) *ResetTokenRepository {
	return &ResetTokenRepository{db: db}
}

func (r *ResetTokenRepository) Create(ctx context.Context, token authdomain.PasswordResetToken) error {
	return r.db.WithContext(ctx).Create(&PasswordResetTokenModel{
		ID: token.ID, UserID: token.UserID, JTI: token.JTI, TokenHash: token.TokenHash,
		Purpose: token.Purpose, ExpiresAt: token.ExpiresAt, CreatedAt: token.CreatedAt,
	}).Error
}

func (r *ResetTokenRepository) FindByHash(ctx context.Context, hash string) (authdomain.PasswordResetToken, error) {
	var model PasswordResetTokenModel
	err := r.db.WithContext(ctx).Where("token_hash = ?", hash).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return authdomain.PasswordResetToken{}, authdomain.ErrInvalidToken
	}
	if err != nil {
		return authdomain.PasswordResetToken{}, err
	}
	return authdomain.PasswordResetToken{
		ID: model.ID, UserID: model.UserID, JTI: model.JTI, TokenHash: model.TokenHash,
		Purpose: model.Purpose, ExpiresAt: model.ExpiresAt, UsedAt: model.UsedAt, CreatedAt: model.CreatedAt,
	}, nil
}

func (r *ResetTokenRepository) MarkUsed(ctx context.Context, id uuid.UUID, at time.Time) error {
	return r.db.WithContext(ctx).Model(&PasswordResetTokenModel{}).Where("id = ? AND used_at IS NULL", id).
		Update("used_at", at).Error
}
