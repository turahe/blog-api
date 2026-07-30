package persistence

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type TagModel struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey"`
	Name      string
	Slug      string
	CreatedAt time.Time
}

func (TagModel) TableName() string { return "tags" }

type Tag struct {
	ID        uuid.UUID
	Name      string
	Slug      string
	CreatedAt time.Time
}

type TagRepository struct {
	db *gorm.DB
}

func NewTagRepository(db *gorm.DB) *TagRepository {
	return &TagRepository{db: db}
}

func (r *TagRepository) List(ctx context.Context) ([]Tag, error) {
	var models []TagModel
	if err := r.db.WithContext(ctx).Order("name ASC").Find(&models).Error; err != nil {
		return nil, err
	}
	out := make([]Tag, 0, len(models))
	for _, m := range models {
		out = append(out, Tag{ID: m.ID, Name: m.Name, Slug: m.Slug, CreatedAt: m.CreatedAt})
	}
	return out, nil
}
