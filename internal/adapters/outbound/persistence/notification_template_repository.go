package persistence

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/core/notification/template"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var _ template.Store = (*NotificationTemplateRepository)(nil)

// NotificationTemplateModel is the notification_templates row.
type NotificationTemplateModel struct {
	ID        int64     `gorm:"primaryKey"`
	UUID      uuid.UUID `gorm:"type:uuid;column:uuid;default:gen_random_uuid()"`
	Type      string
	Channel   string
	Subject   string
	Title     string
	Body      string
	Preview   string
	Event     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// TableName returns the notification_templates table name for GORM.
func (NotificationTemplateModel) TableName() string { return "notification_templates" }

// NotificationTemplateRepository implements template.Store.
type NotificationTemplateRepository struct {
	db *gorm.DB
}

// NewNotificationTemplateRepository returns a repository backed by db.
func NewNotificationTemplateRepository(db *gorm.DB) *NotificationTemplateRepository {
	return &NotificationTemplateRepository{db: db}
}

func mapNotificationTemplate(model NotificationTemplateModel) template.Template {
	return template.Template{
		Type:    model.Type,
		Channel: template.Channel(model.Channel),
		Subject: model.Subject,
		Title:   model.Title,
		Body:    model.Body,
		Preview: model.Preview,
		Event:   model.Event,
	}
}

// Get returns the template for typ and channel, or template.ErrNotFound.
func (r *NotificationTemplateRepository) Get(ctx context.Context, typ string, channel template.Channel) (template.Template, error) {
	var model NotificationTemplateModel

	err := conn(ctx, r.db).Where("type = ? AND channel = ?", typ, string(channel)).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return template.Template{}, template.ErrNotFound
	}

	if err != nil {
		return template.Template{}, err
	}

	return mapNotificationTemplate(model), nil
}

// List returns every stored template ordered by type then channel.
func (r *NotificationTemplateRepository) List(ctx context.Context) ([]template.Template, error) {
	var models []NotificationTemplateModel
	if err := conn(ctx, r.db).Order("type ASC, channel ASC").Find(&models).Error; err != nil {
		return nil, err
	}

	out := make([]template.Template, 0, len(models))
	for _, m := range models {
		out = append(out, mapNotificationTemplate(m))
	}

	return out, nil
}

// Save validates tpl and inserts or replaces the row for its type and channel.
func (r *NotificationTemplateRepository) Save(ctx context.Context, tpl template.Template) error {
	if err := template.Validate(tpl); err != nil {
		return err
	}

	now := time.Now().UTC()
	model := toNotificationTemplateModel(tpl, now)

	return conn(ctx, r.db).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "type"}, {Name: "channel"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"subject", "title", "body", "preview", "event", "updated_at",
		}),
	}).Create(&model).Error
}

// SeedDefaults inserts tpls, leaving rows that already exist untouched.
func (r *NotificationTemplateRepository) SeedDefaults(ctx context.Context, tpls []template.Template) error {
	if len(tpls) == 0 {
		return nil
	}

	now := time.Now().UTC()
	models := make([]NotificationTemplateModel, 0, len(tpls))

	for _, tpl := range tpls {
		if err := template.Validate(tpl); err != nil {
			return err
		}

		models = append(models, toNotificationTemplateModel(tpl, now))
	}

	return conn(ctx, r.db).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "type"}, {Name: "channel"}},
		DoNothing: true,
	}).Create(&models).Error
}

func toNotificationTemplateModel(tpl template.Template, now time.Time) NotificationTemplateModel {
	return NotificationTemplateModel{
		UUID:      uuid.New(),
		Type:      tpl.Type,
		Channel:   string(tpl.Channel),
		Subject:   tpl.Subject,
		Title:     tpl.Title,
		Body:      tpl.Body,
		Preview:   tpl.Preview,
		Event:     tpl.Event,
		CreatedAt: now,
		UpdatedAt: now,
	}
}
