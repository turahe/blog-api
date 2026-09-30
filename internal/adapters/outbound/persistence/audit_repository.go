package persistence

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"time"

	"github.com/google/uuid"
	auditdomain "github.com/turahe/blog-api/internal/core/audit/domain"
	"github.com/turahe/blog-api/internal/shared/pagination"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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
	ID               int64     `gorm:"primaryKey"`
	UUID             uuid.UUID `gorm:"type:uuid;column:uuid"`
	ActorID          *int64
	ImpersonatorID   *int64
	Action           string
	Category         *string
	Result           string
	ResourceType     *string
	ResourceID       *uuid.UUID `gorm:"type:uuid"`
	Metadata         string     `gorm:"type:jsonb"`
	IPAddress        *string    `gorm:"column:ip_address"`
	UserAgent        *string
	RequestID        *string
	OccurredAt       time.Time
	ActorUUID        *uuid.UUID `gorm:"column:actor_uuid;->"`
	ImpersonatorUUID *uuid.UUID `gorm:"column:impersonator_uuid;->"`
}

// TableName implements gorm's tabler.
func (AuditLogModel) TableName() string { return "audit_logs" }

var auditLogColumns = withRefs("audit_logs", uuidRef("users", "audit_logs.actor_id", "actor_uuid"),
	uuidRef("users", "audit_logs.impersonator_id", "impersonator_uuid"))

var activityListCfg = pagination.CursorConfig{
	Kind: "activity_logs",
	Sort: []pagination.SortField{
		{Name: "occurred_at", Column: "audit_logs.occurred_at", Dir: pagination.Desc, Type: pagination.TypeTime},
		{Name: "id", Column: "audit_logs.id", Dir: pagination.Desc, Type: pagination.TypeInt64},
	},
	TTL:            pagination.DefaultTTL,
	MaxPerPage:     pagination.DefaultMaxPerPage,
	DefaultPerPage: pagination.DefaultPerPage,
}

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

	db := conn(ctx, r.db)

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

	err = db.Omit("ID", "ActorUUID", "ImpersonatorUUID").
		Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "uuid"}}, DoNothing: true}).
		Create(&models).Error
	if err != nil {
		return fmt.Errorf("insert audit logs: %w", err)
	}

	return nil
}

func (r *AuditRepository) actorIDs(db *gorm.DB, entries []auditdomain.Entry) (map[uuid.UUID]int64, error) {
	seen := map[uuid.UUID]struct{}{}
	uuids := make([]uuid.UUID, 0, len(entries))

	for _, entry := range entries {
		for _, id := range []*uuid.UUID{entry.ActorID, entry.ImpersonatorID} {
			if id == nil {
				continue
			}

			if _, ok := seen[*id]; !ok {
				seen[*id] = struct{}{}
				uuids = append(uuids, *id)
			}
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
	pr := filter.PageRequest
	if pr.Limit <= 0 {
		pr.Limit = activityListCfg.DefaultPerPage
	}

	var cursorFields map[string]any
	if pr.Mode == pagination.ModeCursor && pr.Cursor != "" {
		decoded, _, err := pagination.DecodeCursor(activityListCfg, pr.Cursor)
		if err != nil {
			return auditdomain.ActivityPage{}, err
		}
		if err := pagination.ValidateCursor(activityListCfg, decoded); err != nil {
			return auditdomain.ActivityPage{}, err
		}
		cursorFields = decoded
	}
	seek, err := pagination.BuildSeek(activityListCfg, pr, cursorFields)
	if err != nil {
		return auditdomain.ActivityPage{}, err
	}

	query := conn(ctx, r.db).Model(&AuditLogModel{}).
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

	if seek.WhereClause != "" {
		query = query.Where(seek.WhereClause, seek.BindVars...)
	}

	var out auditdomain.ActivityPage
	if pr.IncludeTotal {
		var total int64
		if err := query.Count(&total).Error; err != nil {
			return auditdomain.ActivityPage{}, fmt.Errorf("count activity: %w", err)
		}
		out.Total = &total
	}

	var models []AuditLogModel
	db := query.Select(auditLogColumns).Order(seek.OrderClause).Limit(seek.LimitFetch)
	if pr.Mode == pagination.ModeOffset {
		db = db.Offset(pr.Offset)
	}
	if err := db.Find(&models).Error; err != nil {
		return auditdomain.ActivityPage{}, fmt.Errorf("list activity: %w", err)
	}

	if seek.ReverseDisplay {
		for i, j := 0, len(models)-1; i < j; i, j = i+1, j-1 {
			models[i], models[j] = models[j], models[i]
		}
	}

	items := make([]auditdomain.Entry, 0, len(models))
	for _, model := range models {
		items = append(items, auditEntry(model))
	}

	page, hasNext, hasPrev := pagination.TruncatePage(items, pr.Limit, pr.Forward, pr.Cursor != "")
	pageModels := computePageModels(models, len(page), pr.Forward, len(items) > pr.Limit)

	out.Items = page
	out.HasNextPage = hasNext
	out.HasPreviousPage = hasPrev
	out.Limit = pr.Limit
	if pr.Mode == pagination.ModeOffset {
		out.OffsetPage = pr.Page
		out.OffsetPerPage = pr.Limit
	}
	if len(page) > 0 {
		if out.HasNextPage {
			lastModel := pageModels[len(pageModels)-1]
			fields := pagination.SortValues[auditdomain.Entry](activityListCfg, page[len(page)-1],
				func(row auditdomain.Entry, i int) any {
					switch i {
					case 0:
						return row.OccurredAt
					case 1:
						return lastModel.ID
					}
					return nil
				})
			cur, err := pagination.EncodeCursor(activityListCfg, fields)
			if err != nil {
				return auditdomain.ActivityPage{}, err
			}
			out.NextCursor = cur
		}
		if out.HasPreviousPage {
			firstModel := pageModels[0]
			fields := pagination.SortValues[auditdomain.Entry](activityListCfg, page[0],
				func(row auditdomain.Entry, i int) any {
					switch i {
					case 0:
						return row.OccurredAt
					case 1:
						return firstModel.ID
					}
					return nil
				})
			cur, err := pagination.EncodeCursor(activityListCfg, fields)
			if err != nil {
				return auditdomain.ActivityPage{}, err
			}
			out.PreviousCursor = cur
		}
	}

	return out, nil
}

func computePageModels(models []AuditLogModel, pageLen int, forward bool, excess bool) []AuditLogModel {
	if !excess {
		return models
	}
	if forward {
		return models[:pageLen]
	}
	return models[1 : pageLen+1]
}

// Prune deletes entries that occurred before cutoff in bounded batches.
func (r *AuditRepository) Prune(ctx context.Context, cutoff time.Time) (int64, error) {
	var total int64

	for {
		result := conn(ctx, r.db).Exec(
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

	model.ActorID = actorRef(actors, entry.ActorID)
	model.ImpersonatorID = actorRef(actors, entry.ImpersonatorID)

	return model, nil
}

func actorRef(actors map[uuid.UUID]int64, id *uuid.UUID) *int64 {
	if id == nil {
		return nil
	}

	if resolved, ok := actors[*id]; ok {
		return &resolved
	}

	return nil
}

func auditEntry(model AuditLogModel) auditdomain.Entry {
	entry := auditdomain.Entry{
		UUID:           model.UUID,
		Action:         model.Action,
		Category:       deref(model.Category),
		ActorID:        model.ActorUUID,
		ImpersonatorID: model.ImpersonatorUUID,
		ResourceType:   deref(model.ResourceType),
		ResourceID:     model.ResourceID,
		Result:         model.Result,
		IP:             deref(model.IPAddress),
		UserAgent:      deref(model.UserAgent),
		RequestID:      deref(model.RequestID),
		OccurredAt:     model.OccurredAt,
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
