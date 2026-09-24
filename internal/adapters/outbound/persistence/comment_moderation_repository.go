package persistence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	commentdomain "github.com/turahe/blog-api/internal/core/comment/domain"
	"gorm.io/gorm"
)

const statsTopPosts = 10

// ModerationLogModel is an append-only comment_moderation_log row. Snapshots are JSON text
// so they bind as jsonb under both the extended and simple query protocols.
type ModerationLogModel struct {
	ID            int64     `gorm:"primaryKey"`
	UUID          uuid.UUID `gorm:"type:uuid;column:uuid"`
	CommentID     *int64
	CommentUUID   uuid.UUID `gorm:"type:uuid;column:comment_uuid"`
	ModeratorID   *int64
	Action        string
	FromStatus    string
	ToStatus      string
	Reason        *string
	NotifyAuthor  bool
	BeforeState   *string `gorm:"type:jsonb"`
	AfterState    *string `gorm:"type:jsonb"`
	CreatedAt     time.Time
	ModeratorUUID *uuid.UUID `gorm:"column:moderator_uuid;->"`
}

// TableName returns the moderation log table name for GORM.
func (ModerationLogModel) TableName() string { return "comment_moderation_log" }

var moderationLogColumns = withRefs("comment_moderation_log",
	uuidRef("users", "comment_moderation_log.moderator_id", "moderator_uuid"),
)

// GetByIDs returns the comments that exist among ids.
func (r *CommentRepository) GetByIDs(ctx context.Context, ids []uuid.UUID) ([]commentdomain.Comment, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	var models []CommentModel
	if err := r.db.WithContext(ctx).Select(commentColumns).Where("comments.uuid IN ?", ids).Find(&models).Error; err != nil {
		return nil, err
	}

	comments := make([]commentdomain.Comment, 0, len(models))
	for _, model := range models {
		comments = append(comments, mapComment(model))
	}

	return comments, nil
}

// ApplyModerations updates each comment only while it is still in its expected status, then
// appends the log entries. Any stale comment rolls back the whole batch.
func (r *CommentRepository) ApplyModerations(ctx context.Context, changes []commentdomain.Moderation) error {
	if len(changes) == 0 {
		return nil
	}

	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		users := userIDCache{db: tx, ids: map[uuid.UUID]*int64{}}

		var stale []uuid.UUID

		logs := make([]ModerationLogModel, 0, len(changes))

		for _, change := range changes {
			comment := change.Comment

			moderatedBy, err := users.resolve(comment.ModeratedByUUID)
			if err != nil {
				return err
			}

			deletedBy, err := users.resolve(comment.DeletedByUUID)
			if err != nil {
				return err
			}

			res := tx.Model(&CommentModel{}).
				Where("uuid = ? AND status = ?", comment.UUID, string(change.From)).
				Updates(map[string]any{
					"status":            string(comment.Status),
					"flag_count":        comment.FlagCount,
					"deleted_at":        comment.DeletedAt,
					"deleted_by":        deletedBy,
					"moderated_by":      moderatedBy,
					"moderation_reason": nullableString(comment.ModerationReason),
					"moderated_at":      comment.ModeratedAt,
					"updated_at":        comment.UpdatedAt,
				})
			if res.Error != nil {
				return res.Error
			}

			if res.RowsAffected == 0 {
				stale = append(stale, comment.UUID)
				continue
			}

			entry, err := moderationLogModel(change.Entry, &comment.ID, &users)
			if err != nil {
				return err
			}

			logs = append(logs, entry)
		}

		if len(stale) > 0 {
			return &commentdomain.BatchError{Err: commentdomain.ErrInvalidTransition, IDs: stale}
		}

		return tx.CreateInBatches(logs, 100).Error
	})
}

// HardDelete logs entry first so its comment reference can be nulled, then deletes the
// comment. A comment with replies is scrubbed instead: parent_id cascades, so deleting it
// would silently remove the whole subtree.
func (r *CommentRepository) HardDelete(ctx context.Context, id uuid.UUID, entry commentdomain.ModerationEntry) (bool, error) {
	scrubbed := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rowIDs []int64
		if err := tx.Raw(`SELECT id FROM comments WHERE uuid = ? FOR UPDATE`, id).Scan(&rowIDs).Error; err != nil {
			return err
		}

		if len(rowIDs) == 0 {
			return commentdomain.ErrNotFound
		}

		rowID := rowIDs[0]
		users := userIDCache{db: tx, ids: map[uuid.UUID]*int64{}}

		log, err := moderationLogModel(entry, &rowID, &users)
		if err != nil {
			return err
		}

		if err := tx.Create(&log).Error; err != nil {
			return err
		}

		var hasReplies bool
		if err := tx.Raw(`SELECT EXISTS (SELECT 1 FROM comments WHERE parent_id = ?)`, rowID).Scan(&hasReplies).Error; err != nil {
			return err
		}

		if !hasReplies {
			return tx.Exec(`DELETE FROM comments WHERE id = ?`, rowID).Error
		}

		scrubbed = true

		for _, table := range []string{"comment_flags", "comment_upvotes"} {
			if err := tx.Exec(`DELETE FROM `+table+` WHERE comment_id = ?`, rowID).Error; err != nil {
				return err
			}
		}

		return tx.Exec(`
			UPDATE comments
			SET content = '', author_id = NULL, author_name = NULL, author_email = NULL,
			    ip_hash = NULL, user_agent = NULL, flag_count = 0, upvote_count = 0,
			    status = ?, deleted_at = COALESCE(deleted_at, ?), deleted_by = ?,
			    moderated_by = ?, moderation_reason = ?, moderated_at = ?, updated_at = ?
			WHERE id = ?`,
			string(commentdomain.StatusDeleted), entry.CreatedAt, log.ModeratorID,
			log.ModeratorID, log.Reason, entry.CreatedAt, entry.CreatedAt, rowID).Error
	})

	return scrubbed, err
}

// ListFlags returns the comment's flags, newest first.
func (r *CommentRepository) ListFlags(ctx context.Context, id uuid.UUID) ([]commentdomain.Flag, error) {
	var rows []struct {
		ReporterUUID   *uuid.UUID
		ReporterIPHash *string
		ReasonCode     string
		Details        *string
		CreatedAt      time.Time
	}

	err := r.db.WithContext(ctx).Table("comment_flags").
		Select("(SELECT ref.uuid FROM users ref WHERE ref.id = comment_flags.reporter_user_id) AS reporter_uuid, "+
			"reporter_ip_hash, reason_code, details, created_at").
		Where("comment_id = "+idOf("comments"), id).
		Order("created_at DESC, id DESC").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	flags := make([]commentdomain.Flag, 0, len(rows))
	for _, row := range rows {
		flags = append(flags, commentdomain.Flag{
			CommentUUID:    id,
			ReporterUUID:   row.ReporterUUID,
			ReporterIPHash: derefString(row.ReporterIPHash),
			Reason:         row.ReasonCode,
			Details:        derefString(row.Details),
			CreatedAt:      row.CreatedAt,
		})
	}

	return flags, nil
}

// ListModerationLog returns the comment's moderation history, oldest first.
func (r *CommentRepository) ListModerationLog(ctx context.Context, id uuid.UUID) ([]commentdomain.ModerationEntry, error) {
	var models []ModerationLogModel

	err := r.db.WithContext(ctx).Select(moderationLogColumns).
		Where("comment_moderation_log.comment_uuid = ?", id).
		Order("comment_moderation_log.created_at ASC, comment_moderation_log.id ASC").
		Find(&models).Error
	if err != nil {
		return nil, err
	}

	entries := make([]commentdomain.ModerationEntry, 0, len(models))

	for _, model := range models {
		entry, err := mapModerationLog(model)
		if err != nil {
			return nil, err
		}

		entries = append(entries, entry)
	}

	return entries, nil
}

// Stats counts comments by status and ranks the posts with the deepest moderation queue.
func (r *CommentRepository) Stats(ctx context.Context) (commentdomain.Stats, error) {
	db := r.db.WithContext(ctx)
	queue := []string{string(commentdomain.StatusPending), string(commentdomain.StatusFlagged)}

	var counts []struct {
		Status string
		Total  int64
	}
	if err := db.Raw(`SELECT status, count(*) AS total FROM comments GROUP BY status`).Scan(&counts).Error; err != nil {
		return commentdomain.Stats{}, err
	}

	stats := commentdomain.Stats{ByStatus: map[commentdomain.Status]int64{}}
	for _, count := range counts {
		stats.ByStatus[commentdomain.Status(count.Status)] = count.Total
	}

	stats.QueueDepth = stats.ByStatus[commentdomain.StatusPending] + stats.ByStatus[commentdomain.StatusFlagged]

	var oldest []time.Time
	if err := db.Raw(`SELECT min(created_at) FROM comments WHERE status IN ? HAVING count(*) > 0`, queue).
		Scan(&oldest).Error; err != nil {
		return commentdomain.Stats{}, err
	}

	if len(oldest) > 0 {
		stats.OldestQueuedAt = &oldest[0]
	}

	var top []struct {
		PostUUID  uuid.UUID
		PostTitle string
		Pending   int64
		Flagged   int64
	}

	err := db.Raw(`
		SELECT p.uuid AS post_uuid, p.title AS post_title,
		       count(*) FILTER (WHERE c.status = ?) AS pending,
		       count(*) FILTER (WHERE c.status = ?) AS flagged
		FROM comments c
		JOIN posts p ON p.id = c.post_id
		WHERE c.status IN ?
		GROUP BY p.id, p.uuid, p.title
		ORDER BY count(*) DESC, p.id
		LIMIT ?`,
		string(commentdomain.StatusPending), string(commentdomain.StatusFlagged), queue, statsTopPosts).
		Scan(&top).Error
	if err != nil {
		return commentdomain.Stats{}, err
	}

	stats.TopPosts = make([]commentdomain.PostQueue, 0, len(top))
	for _, post := range top {
		stats.TopPosts = append(stats.TopPosts, commentdomain.PostQueue(post))
	}

	return stats, nil
}

// userIDCache resolves user uuids once per transaction; bulk moderation repeats the moderator.
type userIDCache struct {
	db  *gorm.DB
	ids map[uuid.UUID]*int64
}

func (c *userIDCache) resolve(id *uuid.UUID) (*int64, error) {
	if id == nil {
		return nil, nil
	}

	if rowID, ok := c.ids[*id]; ok {
		return rowID, nil
	}

	rowID, err := optionalIDByUUID(c.db, "users", id)
	if errors.Is(err, errUnknownReference) {
		rowID, err = nil, nil
	}

	if err != nil {
		return nil, err
	}

	c.ids[*id] = rowID

	return rowID, nil
}

func moderationLogModel(entry commentdomain.ModerationEntry, commentRowID *int64, users *userIDCache) (ModerationLogModel, error) {
	moderatorID, err := users.resolve(entry.ModeratorUUID)
	if err != nil {
		return ModerationLogModel{}, err
	}

	before, err := snapshotJSON(entry.Before)
	if err != nil {
		return ModerationLogModel{}, err
	}

	after, err := snapshotJSON(entry.After)
	if err != nil {
		return ModerationLogModel{}, err
	}

	return ModerationLogModel{
		UUID:         entry.UUID,
		CommentID:    commentRowID,
		CommentUUID:  entry.CommentUUID,
		ModeratorID:  moderatorID,
		Action:       string(entry.Action),
		FromStatus:   string(entry.FromStatus),
		ToStatus:     string(entry.ToStatus),
		Reason:       nullableString(entry.Reason),
		NotifyAuthor: entry.NotifyAuthor,
		BeforeState:  before,
		AfterState:   after,
		CreatedAt:    entry.CreatedAt,
	}, nil
}

func mapModerationLog(model ModerationLogModel) (commentdomain.ModerationEntry, error) {
	before, err := parseSnapshot(model.BeforeState)
	if err != nil {
		return commentdomain.ModerationEntry{}, err
	}

	after, err := parseSnapshot(model.AfterState)
	if err != nil {
		return commentdomain.ModerationEntry{}, err
	}

	return commentdomain.ModerationEntry{
		UUID:          model.UUID,
		CommentUUID:   model.CommentUUID,
		ModeratorUUID: model.ModeratorUUID,
		Action:        commentdomain.Action(model.Action),
		FromStatus:    commentdomain.Status(model.FromStatus),
		ToStatus:      commentdomain.Status(model.ToStatus),
		Reason:        derefString(model.Reason),
		NotifyAuthor:  model.NotifyAuthor,
		Before:        before,
		After:         after,
		CreatedAt:     model.CreatedAt,
	}, nil
}

func snapshotJSON(snapshot map[string]any) (*string, error) {
	if snapshot == nil {
		return nil, nil
	}

	raw, err := json.Marshal(snapshot)
	if err != nil {
		return nil, fmt.Errorf("encode moderation snapshot: %w", err)
	}

	encoded := string(raw)

	return &encoded, nil
}

func parseSnapshot(raw *string) (map[string]any, error) {
	if raw == nil {
		return nil, nil
	}

	var snapshot map[string]any
	if err := json.Unmarshal([]byte(*raw), &snapshot); err != nil {
		return nil, fmt.Errorf("decode moderation snapshot: %w", err)
	}

	return snapshot, nil
}
