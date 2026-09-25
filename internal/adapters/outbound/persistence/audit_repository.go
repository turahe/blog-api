package persistence

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"time"

	"github.com/google/uuid"
	auditdomain "github.com/turahe/blog-api/internal/core/audit/domain"
	"gorm.io/gorm"
)

// pruneBatchSize bounds each DELETE so pruning never holds long locks.
const pruneBatchSize = 5000

// Metadata keys the repository reserves inside audit_logs.metadata.
const (
	auditStatusKey  = "status"
	auditChangesKey = "changes"
)

// AuditLogModel maps audit_logs. Metadata is JSON text so it binds as jsonb
// under both the extended and simple query protocols.
type AuditLogModel struct {
	ID           int64     `gorm:"primaryKey"`
	UUID         uuid.UUID `gorm:"type:uuid;column:uuid"`
	ActorID      *int64
	Action       string
	Category     *string
	Result       string
	ResourceType *string
	ResourceID   *uuid.UUID `gorm:"type:uuid"`
	Metadata     string     `gorm:"type:jsonb"`
	IPAddress    *string    `gorm:"column:ip_address"`
	UserAgent    *string
	RequestID    *string
	OccurredAt   time.Time
	ActorUUID    *uuid.UUID `gorm:"column:actor_uuid;->"`
}

// TableName implements gorm's tabler.
func (AuditLogModel) TableName() string { return "audit_logs" }

var auditLogColumns = withRefs("audit_logs", uuidRef("users", "audit_logs.actor_id", "actor_uuid"))

// AuditRepository implements auditports.Repository.
type AuditRepository struct {
	db *gorm.DB
}

// NewAuditRepository returns an AuditRepository backed by db.
func NewAuditRepository(db *gorm.DB) *AuditRepository {
	return &AuditRepository{db: db}
}

// Insert stores entries in one statement. Actors that no longer exist are stored as NULL.
func (r *AuditRepository) Insert(ctx context.Context, entries []auditdomain.Entry) error {
	if len(entries) == 0 {
		return nil
	}

	db := r.db.WithContext(ctx)

	actors, err := r.actorIDs(db, entries)
	if err != nil {
		return err
	}

	models := make([]AuditLogModel, 0, len(entries))

	for _, entry := range entries {
		model, err := auditModel(entry, actors)
		if err != nil {
			return err
		}

		models = append(models, model)
	}

	if err := db.Omit("ID", "ActorUUID").Create(&models).Error; err != nil {
		return fmt.Errorf("insert audit logs: %w", err)
	}

	return nil
}

func (r *AuditRepository) actorIDs(db *gorm.DB, entries []auditdomain.Entry) (map[uuid.UUID]int64, error) {
	seen := map[uuid.UUID]struct{}{}
	uuids := make([]uuid.UUID, 0, len(entries))

	for _, entry := range entries {
		if entry.ActorID == nil {
			continue
		}

		if _, ok := seen[*entry.ActorID]; !ok {
			seen[*entry.ActorID] = struct{}{}
			uuids = append(uuids, *entry.ActorID)
		}
	}

	ids := map[uuid.UUID]int64{}
	if len(uuids) == 0 {
		return ids, nil
	}

	var rows []struct {
		ID   int64
		UUID uuid.UUID
	}
	if err := db.Table("users").Select("id, uuid").Where("uuid IN ?", uuids).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("resolve audit actors: %w", err)
	}

	for _, row := range rows {
		ids[row.UUID] = row.ID
	}

	return ids, nil
}

// Activity lists entries the user performed or that target the user's account, newest first.
func (r *AuditRepository) Activity(ctx context.Context, filter auditdomain.ActivityFilter) (auditdomain.ActivityPage, error) {
	query := r.db.WithContext(ctx).Model(&AuditLogModel{}).
		Where("(audit_logs.actor_id = "+idOf("users")+" OR (audit_logs.resource_type = ? AND audit_logs.resource_id = ?))",
			filter.UserID, auditdomain.ResourceUser, filter.UserID)

	if filter.CategorizedOnly {
		query = query.Where("audit_logs.category IS NOT NULL")
	}

	if len(filter.Categories) > 0 {
		query = query.Where("audit_logs.category IN ?", filter.Categories)
	}

	if filter.From != nil {
		query = query.Where("audit_logs.occurred_at >= ?", *filter.From)
	}

	if filter.To != nil {
		query = query.Where("audit_logs.occurred_at <= ?", *filter.To)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return auditdomain.ActivityPage{}, fmt.Errorf("count activity: %w", err)
	}

	var models []AuditLogModel

	err := query.Select(auditLogColumns).
		Order("audit_logs.occurred_at DESC, audit_logs.id DESC").
		Offset((filter.Page - 1) * filter.PerPage).Limit(filter.PerPage).
		Find(&models).Error
	if err != nil {
		return auditdomain.ActivityPage{}, fmt.Errorf("list activity: %w", err)
	}

	items := make([]auditdomain.Entry, 0, len(models))
	for _, model := range models {
		items = append(items, auditEntry(model))
	}

	return auditdomain.ActivityPage{Items: items, Page: filter.Page, PerPage: filter.PerPage, Total: total}, nil
}

// Prune deletes entries that occurred before cutoff in bounded batches.
func (r *AuditRepository) Prune(ctx context.Context, cutoff time.Time) (int64, error) {
	var total int64

	for {
		result := r.db.WithContext(ctx).Exec(
			`DELETE FROM audit_logs WHERE id IN (SELECT id FROM audit_logs WHERE occurred_at < ? LIMIT ?)`,
			cutoff, pruneBatchSize)
		if result.Error != nil {
			return total, fmt.Errorf("prune audit logs: %w", result.Error)
		}

		total += result.RowsAffected
		if result.RowsAffected < pruneBatchSize {
			return total, nil
		}
	}
}

func auditModel(entry auditdomain.Entry, actors map[uuid.UUID]int64) (AuditLogModel, error) {
	metadata := make(map[string]any, len(entry.Metadata)+2)
	maps.Copy(metadata, entry.Metadata)

	if entry.Status != 0 {
		metadata[auditStatusKey] = entry.Status
	}

	if len(entry.Changes) > 0 {
		metadata[auditChangesKey] = entry.Changes
	}

	encoded, err := json.Marshal(metadata)
	if err != nil {
		return AuditLogModel{}, fmt.Errorf("encode audit metadata: %w", err)
	}

	model := AuditLogModel{
		UUID:         entry.UUID,
		Action:       entry.Action,
		Category:     optionalString(entry.Category),
		Result:       entry.Result,
		ResourceType: optionalString(entry.ResourceType),
		ResourceID:   entry.ResourceID,
		Metadata:     string(encoded),
		IPAddress:    optionalString(entry.IP),
		UserAgent:    optionalString(entry.UserAgent),
		RequestID:    optionalString(entry.RequestID),
		OccurredAt:   entry.OccurredAt,
	}

	if model.Result == "" {
		model.Result = auditdomain.ResultSuccess
	}

	if entry.ActorID != nil {
		if id, ok := actors[*entry.ActorID]; ok {
			model.ActorID = &id
		}
	}

	return model, nil
}

func auditEntry(model AuditLogModel) auditdomain.Entry {
	entry := auditdomain.Entry{
		UUID:         model.UUID,
		Action:       model.Action,
		Category:     deref(model.Category),
		ActorID:      model.ActorUUID,
		ResourceType: deref(model.ResourceType),
		ResourceID:   model.ResourceID,
		Result:       model.Result,
		IP:           deref(model.IPAddress),
		UserAgent:    deref(model.UserAgent),
		RequestID:    deref(model.RequestID),
		OccurredAt:   model.OccurredAt,
	}

	var raw map[string]json.RawMessage
	if json.Unmarshal([]byte(model.Metadata), &raw) != nil {
		return entry
	}

	if status, ok := raw[auditStatusKey]; ok {
		_ = json.Unmarshal(status, &entry.Status)

		delete(raw, auditStatusKey)
	}

	if changes, ok := raw[auditChangesKey]; ok {
		_ = json.Unmarshal(changes, &entry.Changes)

		delete(raw, auditChangesKey)
	}

	if len(raw) > 0 {
		entry.Metadata = make(map[string]any, len(raw))
		for k, v := range raw {
			var value any
			if json.Unmarshal(v, &value) == nil {
				entry.Metadata[k] = value
			}
		}
	}

	return entry
}

func optionalString(s string) *string {
	if s == "" {
		return nil
	}

	return &s
}

func deref(s *string) string {
	if s == nil {
		return ""
	}

	return *s
}
