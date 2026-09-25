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

// RegistrationModel is a registrations row.
type RegistrationModel struct {
	ID           int64     `gorm:"primaryKey"`
	UUID         uuid.UUID `gorm:"type:uuid;column:uuid"`
	Email        string    `gorm:"column:email"`
	Username     string    `gorm:"column:username"`
	FullName     string    `gorm:"column:full_name"`
	PasswordHash string    `gorm:"column:password_hash"`
	TokenHash    string    `gorm:"column:token_hash"`
	ExpiresAt    time.Time `gorm:"column:expires_at"`
	CreatedAt    time.Time `gorm:"column:created_at"`
}

// TableName returns the registrations table name for GORM.
func (RegistrationModel) TableName() string { return "registrations" }

// RegistrationRepository implements authports.RegistrationRepository.
type RegistrationRepository struct {
	db *gorm.DB
}

// NewRegistrationRepository returns a RegistrationRepository backed by db.
func NewRegistrationRepository(db *gorm.DB) *RegistrationRepository {
	return &RegistrationRepository{db: db}
}

// createRegistration inserts the row only while the address has fewer than @max unexpired
// registrations. Concurrent sign-ups can overshoot the cap by a row, which is harmless.
const createRegistration = `
INSERT INTO registrations (uuid, email, username, full_name, password_hash, token_hash, expires_at, created_at)
SELECT @uuid, @email, @username, @full_name, @password_hash, @token_hash, @expires_at, @created_at
WHERE (SELECT count(*) FROM registrations WHERE lower(email) = lower(@email) AND expires_at > @created_at) < @max`

// Create stores registration unless its address already has maxLive unexpired ones.
func (r *RegistrationRepository) Create(ctx context.Context, registration authdomain.Registration, maxLive int) (bool, error) {
	res := conn(ctx, r.db).Exec(createRegistration, map[string]any{
		"uuid": registration.UUID, "email": registration.Email, "username": registration.Username,
		"full_name": registration.FullName, "password_hash": registration.PasswordHash,
		"token_hash": registration.TokenHash, "expires_at": registration.ExpiresAt,
		"created_at": registration.CreatedAt, "max": maxLive,
	})
	if res.Error != nil {
		return false, fmt.Errorf("create registration: %w", res.Error)
	}

	return res.RowsAffected == 1, nil
}

// FindByTokenHash returns the registration with hash or authdomain.ErrRegistrationTokenInvalid.
func (r *RegistrationRepository) FindByTokenHash(ctx context.Context, hash string) (authdomain.Registration, error) {
	var model RegistrationModel

	err := conn(ctx, r.db).Where("token_hash = ?", hash).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return authdomain.Registration{}, authdomain.ErrRegistrationTokenInvalid
	}

	if err != nil {
		return authdomain.Registration{}, fmt.Errorf("find registration: %w", err)
	}

	return authdomain.Registration{
		UUID: model.UUID, Email: model.Email, Username: model.Username, FullName: model.FullName,
		PasswordHash: model.PasswordHash, TokenHash: model.TokenHash, ExpiresAt: model.ExpiresAt.UTC(), CreatedAt: model.CreatedAt.UTC(),
	}, nil
}

// consumeRegistration deletes the used registration and the other ones for its address, and
// counts the used one so a concurrent verification of the same token sees zero.
const consumeRegistration = `
WITH hit AS (
    DELETE FROM registrations WHERE token_hash = @hash RETURNING email
), others AS (
    DELETE FROM registrations
    WHERE lower(email) IN (SELECT lower(email) FROM hit) AND token_hash <> @hash
)
SELECT count(*) FROM hit`

// Consume deletes the registration with tokenHash and every other one for the same address.
func (r *RegistrationRepository) Consume(ctx context.Context, tokenHash string) (bool, error) {
	var hits int64
	if err := conn(ctx, r.db).Raw(consumeRegistration, map[string]any{"hash": tokenHash}).Scan(&hits).Error; err != nil {
		return false, fmt.Errorf("consume registration: %w", err)
	}

	return hits == 1, nil
}

// PruneExpired deletes registrations whose token expired before now and returns how many.
func (r *RegistrationRepository) PruneExpired(ctx context.Context, now time.Time) (int64, error) {
	res := r.db.WithContext(ctx).Exec(`DELETE FROM registrations WHERE expires_at <= ?`, now)
	if res.Error != nil {
		return 0, fmt.Errorf("prune registrations: %w", res.Error)
	}

	return res.RowsAffected, nil
}
