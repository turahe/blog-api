package persistence

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// TwoFactorMethodModel maps user_two_factor_methods.
type TwoFactorMethodModel struct {
	ID               int64
	UserID           int64
	SecretCiphertext string
	ConfirmedAt      *time.Time
	LastUsedStep     int64
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// TableName implements gorm's tabler.
func (TwoFactorMethodModel) TableName() string { return "user_two_factor_methods" }

// TwoFactorBackupCodeModel maps user_two_factor_backup_codes.
type TwoFactorBackupCodeModel struct {
	ID        int64
	UserID    int64
	CodeHash  string
	UsedAt    *time.Time
	CreatedAt time.Time
}

// TableName implements gorm's tabler.
func (TwoFactorBackupCodeModel) TableName() string { return "user_two_factor_backup_codes" }

// TwoFactorRepository implements authports.TwoFactorRepository.
type TwoFactorRepository struct {
	db *gorm.DB
}

// NewTwoFactorRepository returns a TwoFactorRepository backed by db.
func NewTwoFactorRepository(db *gorm.DB) *TwoFactorRepository {
	return &TwoFactorRepository{db: db}
}

// Find returns the enrollment with its count of unused backup codes.
func (r *TwoFactorRepository) Find(ctx context.Context, userID uuid.UUID) (authdomain.TwoFactor, error) {
	var model TwoFactorMethodModel

	err := conn(ctx, r.db).Where("user_id = "+idOf("users"), userID).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return authdomain.TwoFactor{}, authdomain.ErrTwoFactorNotEnrolled
	}

	if err != nil {
		return authdomain.TwoFactor{}, fmt.Errorf("find two-factor method: %w", err)
	}

	var remaining int64
	if err := conn(ctx, r.db).Model(&TwoFactorBackupCodeModel{}).
		Where("user_id = ? AND used_at IS NULL", model.UserID).Count(&remaining).Error; err != nil {
		return authdomain.TwoFactor{}, fmt.Errorf("count backup codes: %w", err)
	}

	return authdomain.TwoFactor{
		UserUUID:             userID,
		SecretCiphertext:     model.SecretCiphertext,
		ConfirmedAt:          model.ConfirmedAt,
		LastUsedStep:         model.LastUsedStep,
		BackupCodesRemaining: int(remaining),
	}, nil
}

// SavePending upserts an unconfirmed enrollment and drops any backup codes.
// It never overwrites a confirmed enrollment.
func (r *TwoFactorRepository) SavePending(ctx context.Context, userID uuid.UUID, secretCiphertext string, at time.Time) error {
	return conn(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		id, err := idByUUID(tx, "users", userID)
		if err != nil {
			return err
		}

		model := TwoFactorMethodModel{UserID: id, SecretCiphertext: secretCiphertext, CreatedAt: at, UpdatedAt: at}

		res := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "user_id"}},
			DoUpdates: clause.Assignments(map[string]any{
				"secret_ciphertext": secretCiphertext,
				"last_used_step":    0,
				"updated_at":        at,
			}),
			Where: clause.Where{Exprs: []clause.Expression{
				clause.Expr{SQL: "user_two_factor_methods.confirmed_at IS NULL"},
			}},
		}).Create(&model)
		if res.Error != nil {
			return fmt.Errorf("save pending two-factor: %w", res.Error)
		}

		if res.RowsAffected == 0 {
			return authdomain.ErrTwoFactorAlreadyEnabled
		}

		return tx.Where("user_id = ?", id).Delete(&TwoFactorBackupCodeModel{}).Error
	})
}

// Confirm enables a pending enrollment and stores its backup codes.
func (r *TwoFactorRepository) Confirm(ctx context.Context, userID uuid.UUID, step int64, codeHashes []string, at time.Time) error {
	return conn(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		id, err := idByUUID(tx, "users", userID)
		if err != nil {
			return err
		}

		res := tx.Model(&TwoFactorMethodModel{}).
			Where("user_id = ? AND confirmed_at IS NULL AND last_used_step < ?", id, step).
			Updates(map[string]any{"confirmed_at": at, "last_used_step": step, "updated_at": at})
		if res.Error != nil {
			return fmt.Errorf("confirm two-factor: %w", res.Error)
		}

		if res.RowsAffected == 0 {
			return authdomain.ErrTwoFactorInvalidCode
		}

		return insertBackupCodes(tx, id, codeHashes, at)
	})
}

// UseStep advances last_used_step so a TOTP code is accepted at most once.
func (r *TwoFactorRepository) UseStep(ctx context.Context, userID uuid.UUID, step int64) (bool, error) {
	res := conn(ctx, r.db).Model(&TwoFactorMethodModel{}).
		Where("user_id = "+idOf("users")+" AND last_used_step < ?", userID, step).
		Updates(map[string]any{"last_used_step": step, "updated_at": gorm.Expr("now()")})
	if res.Error != nil {
		return false, fmt.Errorf("use two-factor step: %w", res.Error)
	}

	return res.RowsAffected == 1, nil
}

// UseBackupCode marks one unused matching code as used.
func (r *TwoFactorRepository) UseBackupCode(ctx context.Context, userID uuid.UUID, codeHash string, at time.Time) (bool, error) {
	res := conn(ctx, r.db).Model(&TwoFactorBackupCodeModel{}).
		Where("user_id = "+idOf("users")+" AND code_hash = ? AND used_at IS NULL", userID, codeHash).
		Update("used_at", at)
	if res.Error != nil {
		return false, fmt.Errorf("use backup code: %w", res.Error)
	}

	return res.RowsAffected == 1, nil
}

// ReplaceBackupCodes swaps every backup code for the new set.
func (r *TwoFactorRepository) ReplaceBackupCodes(ctx context.Context, userID uuid.UUID, codeHashes []string, at time.Time) error {
	return conn(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		id, err := idByUUID(tx, "users", userID)
		if err != nil {
			return err
		}

		if err := tx.Where("user_id = ?", id).Delete(&TwoFactorBackupCodeModel{}).Error; err != nil {
			return fmt.Errorf("delete backup codes: %w", err)
		}

		return insertBackupCodes(tx, id, codeHashes, at)
	})
}

// Delete removes the enrollment and its backup codes.
func (r *TwoFactorRepository) Delete(ctx context.Context, userID uuid.UUID) error {
	return conn(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		id, err := idByUUID(tx, "users", userID)
		if err != nil {
			return err
		}

		if err := tx.Where("user_id = ?", id).Delete(&TwoFactorBackupCodeModel{}).Error; err != nil {
			return fmt.Errorf("delete backup codes: %w", err)
		}

		return tx.Where("user_id = ?", id).Delete(&TwoFactorMethodModel{}).Error
	})
}

func insertBackupCodes(tx *gorm.DB, userID int64, hashes []string, at time.Time) error {
	if len(hashes) == 0 {
		return nil
	}

	rows := make([]TwoFactorBackupCodeModel, len(hashes))
	for i, h := range hashes {
		rows[i] = TwoFactorBackupCodeModel{UserID: userID, CodeHash: h, CreatedAt: at}
	}

	if err := tx.Create(&rows).Error; err != nil {
		return fmt.Errorf("insert backup codes: %w", err)
	}

	return nil
}
