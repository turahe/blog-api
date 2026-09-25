package persistence

import (
	"context"

	"gorm.io/gorm"
)

// pruneSalts deletes the visitor salts of days before the bound date.
const pruneSalts = `DELETE FROM analytics_salts WHERE day < CAST(? AS date)`

// AnalyticsSaltRepository stores the daily salts of anonymous visitor hashes.
type AnalyticsSaltRepository struct {
	db *gorm.DB
}

// NewAnalyticsSaltRepository returns an AnalyticsSaltRepository over db.
func NewAnalyticsSaltRepository(db *gorm.DB) *AnalyticsSaltRepository {
	return &AnalyticsSaltRepository{db: db}
}

// DailySalt returns the salt of day, storing fresh when another replica has not stored one
// first, and deletes the salts of days before keepFrom.
func (r *AnalyticsSaltRepository) DailySalt(ctx context.Context, day string, fresh []byte, keepFrom string) ([]byte, error) {
	db := conn(ctx, r.db)

	if err := db.Exec(pruneSalts, keepFrom).Error; err != nil {
		return nil, err
	}

	err := db.Exec(`INSERT INTO analytics_salts (day, salt) VALUES (CAST(? AS date), ?) ON CONFLICT (day) DO NOTHING`,
		day, fresh).Error
	if err != nil {
		return nil, err
	}

	var salt []byte

	err = db.Raw(`SELECT salt FROM analytics_salts WHERE day = CAST(? AS date)`, day).Row().Scan(&salt)

	return salt, err
}
