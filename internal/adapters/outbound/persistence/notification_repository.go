package persistence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	notificationdomain "github.com/turahe/blog-api/internal/core/notification/domain"
	"github.com/turahe/blog-api/internal/core/notification/ports"
	"github.com/turahe/blog-api/internal/shared/pagination"
	"gorm.io/gorm"
)

var (
	_ ports.Repository = (*NotificationRepository)(nil)
	_ ports.Directory  = (*NotificationRepository)(nil)
)

var notificationListCfg = pagination.CursorConfig{
	Kind: "notifications_me",
	Sort: []pagination.SortField{
		{Name: "created_at", Column: "n.created_at", Dir: pagination.Desc, Type: pagination.TypeTime},
		{Name: "id", Column: "n.id", Dir: pagination.Desc, Type: pagination.TypeInt64},
	},
	TTL:            pagination.DefaultTTL,
	MaxPerPage:     pagination.DefaultMaxPerPage,
	DefaultPerPage: pagination.DefaultPerPage,
}

// NotificationRepository stores in-app notifications and answers the lookups used to
// write their copy.
type NotificationRepository struct {
	db *gorm.DB
}

// NewNotificationRepository returns a NotificationRepository backed by db.
func NewNotificationRepository(db *gorm.DB) *NotificationRepository {
	return &NotificationRepository{db: db}
}

type notificationRow struct {
	ID        int64
	UUID      uuid.UUID
	UserUUID  uuid.UUID
	Type      string
	Title     string
	Body      string
	Preview   string
	Payload   []byte
	ActorUUID *uuid.UUID
	DedupeKey string
	ReadAt    *time.Time
	CreatedAt time.Time
}

const notificationSelect = `
	SELECT n.id, n.uuid, u.uuid AS user_uuid, n.type, n.title, n.body, n.preview, n.payload,
		a.uuid AS actor_uuid, COALESCE(n.dedupe_key, '') AS dedupe_key, n.read_at, n.created_at
	FROM notifications n
	JOIN users u ON u.id = n.user_id
	LEFT JOIN users a ON a.id = n.actor_user_id`

// Insert stores n unless the user already has a row with its dedupe key. A recipient that
// no longer exists is skipped without an error.
func (r *NotificationRepository) Insert(ctx context.Context, n notificationdomain.Notification) (notificationdomain.Notification, bool, error) {
	payload, err := json.Marshal(nonNilPayload(n.Payload))
	if err != nil {
		return notificationdomain.Notification{}, false, fmt.Errorf("encode notification payload: %w", err)
	}

	var inserted struct {
		ID   int64
		UUID uuid.UUID
	}

	res := conn(ctx, r.db).Raw(`
		INSERT INTO notifications (user_id, type, title, body, preview, payload, actor_user_id, dedupe_key, created_at)
		SELECT u.id, ?, ?, ?, ?, ?::jsonb, (SELECT a.id FROM users a WHERE a.uuid = ?), NULLIF(?, ''), ?
		FROM users u WHERE u.uuid = ?
		ON CONFLICT (user_id, dedupe_key) WHERE dedupe_key IS NOT NULL DO NOTHING
		RETURNING id, uuid`,
		n.Type, n.Title, n.Body, n.Preview, string(payload), n.ActorUUID, n.DedupeKey, n.CreatedAt, n.UserUUID,
	).Scan(&inserted)
	if res.Error != nil {
		return notificationdomain.Notification{}, false, fmt.Errorf("insert notification: %w", res.Error)
	}

	if res.RowsAffected == 0 {
		return notificationdomain.Notification{}, false, nil
	}

	n.ID, n.UUID = inserted.ID, inserted.UUID

	return n, true, nil
}

// List returns one page of the user's notifications, newest first.
func (r *NotificationRepository) List(ctx context.Context, filter notificationdomain.ListFilter) (notificationdomain.ListResult, error) {
	db := conn(ctx, r.db)
	pr := filter.PageRequest
	if pr.Limit <= 0 {
		pr.Limit = notificationListCfg.DefaultPerPage
	}

	var counts struct {
		Total  int64
		Unread int64
	}

	if pr.IncludeTotal {
		countQuery := `
			SELECT count(*) AS total, count(*) FILTER (WHERE n.read_at IS NULL) AS unread
			FROM notifications n JOIN users u ON u.id = n.user_id
			WHERE u.uuid = ?`
		args := []any{filter.UserUUID}
		if filter.UnreadOnly {
			countQuery += ` AND n.read_at IS NULL`
		}
		if err := db.Raw(countQuery, args...).Scan(&counts).Error; err != nil {
			return notificationdomain.ListResult{}, fmt.Errorf("count notifications: %w", err)
		}
	} else {
		unreadQuery := `
			SELECT count(*) FILTER (WHERE n.read_at IS NULL) AS unread
			FROM notifications n JOIN users u ON u.id = n.user_id
			WHERE u.uuid = ?`
		if err := db.Raw(unreadQuery, filter.UserUUID).Scan(&counts).Error; err != nil {
			return notificationdomain.ListResult{}, fmt.Errorf("count unread notifications: %w", err)
		}
	}

	var cursorFields map[string]any
	if pr.Mode == pagination.ModeCursor && pr.Cursor != "" {
		decoded, _, err := pagination.DecodeCursor(notificationListCfg, pr.Cursor)
		if err != nil {
			return notificationdomain.ListResult{}, err
		}
		if err := pagination.ValidateCursor(notificationListCfg, decoded); err != nil {
			return notificationdomain.ListResult{}, err
		}
		cursorFields = decoded
	}
	seek, err := pagination.BuildSeek(notificationListCfg, pr, cursorFields)
	if err != nil {
		return notificationdomain.ListResult{}, err
	}

	query := notificationSelect + ` WHERE u.uuid = ?`
	queryArgs := make([]any, 0, len(seek.BindVars)+4)
	queryArgs = append(queryArgs, filter.UserUUID)
	if filter.UnreadOnly {
		query += ` AND n.read_at IS NULL`
	}
	if seek.WhereClause != "" {
		query += ` AND ` + seek.WhereClause
		queryArgs = append(queryArgs, seek.BindVars...)
	}
	query += ` ` + seek.OrderClause + ` LIMIT ?`
	if pr.Mode == pagination.ModeOffset {
		query += ` OFFSET ?`
		queryArgs = append(queryArgs, seek.LimitFetch, pr.Offset)
	} else {
		queryArgs = append(queryArgs, seek.LimitFetch)
	}

	var rows []notificationRow
	if err := db.Raw(query, queryArgs...).Scan(&rows).Error; err != nil {
		return notificationdomain.ListResult{}, fmt.Errorf("list notifications: %w", err)
	}

	if seek.ReverseDisplay {
		for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
			rows[i], rows[j] = rows[j], rows[i]
		}
	}

	items := make([]notificationdomain.Notification, 0, len(rows))
	for _, row := range rows {
		item, err := row.toDomain()
		if err != nil {
			return notificationdomain.ListResult{}, err
		}
		items = append(items, item)
	}

	var out notificationdomain.ListResult
	if pr.IncludeTotal {
		total := counts.Total
		if filter.UnreadOnly {
			total = counts.Unread
		}
		out.Total = &total
	}
	unread := counts.Unread
	out.UnreadTotal = &unread

	page, hasNext, hasPrev := pagination.TruncatePage(items, pr.Limit, pr.Forward, pr.Cursor != "")
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
			fields := pagination.SortValues[notificationdomain.Notification](notificationListCfg, page[len(page)-1],
				func(row notificationdomain.Notification, i int) any {
					switch i {
					case 0:
						return row.CreatedAt
					case 1:
						return row.ID
					}
					return nil
				})
			cur, err := pagination.EncodeCursor(notificationListCfg, fields)
			if err != nil {
				return notificationdomain.ListResult{}, err
			}
			out.NextCursor = cur
		}
		if out.HasPreviousPage {
			fields := pagination.SortValues[notificationdomain.Notification](notificationListCfg, page[0],
				func(row notificationdomain.Notification, i int) any {
					switch i {
					case 0:
						return row.CreatedAt
					case 1:
						return row.ID
					}
					return nil
				})
			cur, err := pagination.EncodeCursor(notificationListCfg, fields)
			if err != nil {
				return notificationdomain.ListResult{}, err
			}
			out.PreviousCursor = cur
		}
	}

	return out, nil
}

func joinAnd(parts ...string) string {
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += " AND "
		}
		result += p
	}
	return result
}

// MarkRead sets read_at once and returns the row; other users' ids are ErrNotFound.
func (r *NotificationRepository) MarkRead(ctx context.Context, userID, id uuid.UUID, at time.Time) (notificationdomain.Notification, error) {
	db := conn(ctx, r.db)

	res := db.Exec(`
		UPDATE notifications n SET read_at = COALESCE(n.read_at, ?)
		FROM users u
		WHERE u.id = n.user_id AND u.uuid = ? AND n.uuid = ?`, at, userID, id)
	if res.Error != nil {
		return notificationdomain.Notification{}, fmt.Errorf("mark notification read: %w", res.Error)
	}

	if res.RowsAffected == 0 {
		return notificationdomain.Notification{}, notificationdomain.ErrNotFound
	}

	var row notificationRow
	if err := db.Raw(notificationSelect+` WHERE n.uuid = ?`, id).Take(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return notificationdomain.Notification{}, notificationdomain.ErrNotFound
		}

		return notificationdomain.Notification{}, fmt.Errorf("get notification: %w", err)
	}

	return row.toDomain()
}

// DisplayName returns the user's full name, or the username when it is blank.
func (r *NotificationRepository) DisplayName(ctx context.Context, userID uuid.UUID) (string, error) {
	var names []string

	err := conn(ctx, r.db).Table("users").Where("uuid = ?", userID).
		Limit(1).Pluck("COALESCE(NULLIF(btrim(full_name), ''), username)", &names).Error
	if err != nil {
		return "", fmt.Errorf("user display name: %w", err)
	}

	if len(names) == 0 {
		return "", notificationdomain.ErrNotFound
	}

	return names[0], nil
}

// PostSummary returns the post's title, slug, and author, including unpublished posts.
func (r *NotificationRepository) PostSummary(ctx context.Context, postID uuid.UUID) (ports.PostSummary, error) {
	var summary ports.PostSummary

	res := conn(ctx, r.db).Raw(`
		SELECT p.uuid, a.uuid AS author_uuid, p.title, p.slug
		FROM posts p JOIN users a ON a.id = p.author_id
		WHERE p.uuid = ?`, postID).Scan(&summary)
	if res.Error != nil {
		return ports.PostSummary{}, fmt.Errorf("post summary: %w", res.Error)
	}

	if res.RowsAffected == 0 {
		return ports.PostSummary{}, notificationdomain.ErrNotFound
	}

	return summary, nil
}

// PostCommenters returns the live users with an approved comment on the post.
func (r *NotificationRepository) PostCommenters(ctx context.Context, postID uuid.UUID) ([]uuid.UUID, error) {
	var ids []uuid.UUID

	err := conn(ctx, r.db).Raw(`
		SELECT DISTINCT u.uuid
		FROM comments c
		JOIN posts p ON p.id = c.post_id
		JOIN users u ON u.id = c.author_id AND u.deleted_at IS NULL
		WHERE p.uuid = ? AND c.status = 'approved' AND c.deleted_at IS NULL`, postID).Scan(&ids).Error
	if err != nil {
		return nil, fmt.Errorf("post commenters: %w", err)
	}

	return ids, nil
}

func (row notificationRow) toDomain() (notificationdomain.Notification, error) {
	payload := map[string]string{}
	if len(row.Payload) > 0 {
		if err := json.Unmarshal(row.Payload, &payload); err != nil {
			return notificationdomain.Notification{}, fmt.Errorf("decode notification payload: %w", err)
		}
	}

	return notificationdomain.Notification{
		ID:        row.ID,
		UUID:      row.UUID,
		UserUUID:  row.UserUUID,
		Type:      row.Type,
		Title:     row.Title,
		Body:      row.Body,
		Preview:   row.Preview,
		Payload:   payload,
		ActorUUID: row.ActorUUID,
		DedupeKey: row.DedupeKey,
		ReadAt:    row.ReadAt,
		CreatedAt: row.CreatedAt,
	}, nil
}

func nonNilPayload(payload map[string]string) map[string]string {
	if payload == nil {
		return map[string]string{}
	}

	return payload
}
