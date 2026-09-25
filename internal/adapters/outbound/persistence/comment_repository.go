package persistence

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	commentdomain "github.com/turahe/blog-api/internal/core/comment/domain"
	"github.com/turahe/blog-api/internal/core/comment/ports"
	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
	"gorm.io/gorm"
)

var _ ports.Repository = (*CommentRepository)(nil)

// CommentModel keeps DeletedAt as a plain pointer: soft-deleted comments stay readable
// as thread placeholders, so GORM's automatic soft-delete scope must not apply.
type CommentModel struct {
	ID               int64     `gorm:"primaryKey"`
	UUID             uuid.UUID `gorm:"type:uuid;column:uuid;default:gen_random_uuid()"`
	PostID           int64
	ParentID         *int64
	AuthorID         *int64
	AuthorName       *string
	AuthorEmail      *string
	IPHash           *string
	UserAgent        *string
	Content          string
	ContentHTML      string `gorm:"column:content_html"`
	Status           string
	Depth            int
	UpvoteCount      int
	FlagCount        int
	EditedAt         *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
	DeletedAt        *time.Time
	DeletedBy        *int64
	ModeratedBy      *int64
	ModerationReason *string
	ModeratedAt      *time.Time
	PostUUID         uuid.UUID  `gorm:"column:post_uuid;->"`
	ParentUUID       *uuid.UUID `gorm:"column:parent_uuid;->"`
	AuthorUUID       *uuid.UUID `gorm:"column:author_uuid;->"`
	AuthorUsername   *string    `gorm:"column:author_username;->"`
	DeletedByUUID    *uuid.UUID `gorm:"column:deleted_by_uuid;->"`
	ModeratedByUUID  *uuid.UUID `gorm:"column:moderated_by_uuid;->"`
	ReplyCount       int        `gorm:"column:reply_count;->"`
}

// TableName returns the comments table name for GORM.
func (CommentModel) TableName() string { return "comments" }

var commentColumns = withRefs("comments",
	uuidRef("posts", "comments.post_id", "post_uuid"),
	uuidRef("comments", "comments.parent_id", "parent_uuid"),
	uuidRef("users", "comments.author_id", "author_uuid"),
	"(SELECT ref.username FROM users ref WHERE ref.id = comments.author_id) AS author_username",
	uuidRef("users", "comments.deleted_by", "deleted_by_uuid"),
	uuidRef("users", "comments.moderated_by", "moderated_by_uuid"),
	"(SELECT count(*) FROM comments ref WHERE ref.parent_id = comments.id AND ref.status IN ('approved', 'deleted')) AS reply_count",
)

// CommentRepository implements commentports.Repository.
type CommentRepository struct {
	db *gorm.DB
}

// NewCommentRepository returns a CommentRepository backed by db.
func NewCommentRepository(db *gorm.DB) *CommentRepository {
	return &CommentRepository{db: db}
}

// PostPolicy returns the comment policy of a published post, or ErrPostNotFound.
func (r *CommentRepository) PostPolicy(ctx context.Context, postID uuid.UUID) (commentdomain.Policy, error) {
	var policies []string

	err := r.db.WithContext(ctx).Table("posts").
		Where("uuid = ? AND status = ? AND deleted_at IS NULL", postID, string(postdomain.StatusPublished)).
		Limit(1).Pluck("comment_policy", &policies).Error
	if err != nil {
		return "", err
	}

	if len(policies) == 0 {
		return "", commentdomain.ErrPostNotFound
	}

	return commentdomain.Policy(policies[0]), nil
}

// GetByID returns the comment with the given UUID or ErrNotFound.
func (r *CommentRepository) GetByID(ctx context.Context, id uuid.UUID) (commentdomain.Comment, error) {
	var model CommentModel

	err := r.db.WithContext(ctx).Select(commentColumns).Where("comments.uuid = ?", id).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return commentdomain.Comment{}, commentdomain.ErrNotFound
	}

	if err != nil {
		return commentdomain.Comment{}, err
	}

	return mapComment(model), nil
}

// List returns a page of comments matching the filter with reply counts.
func (r *CommentRepository) List(ctx context.Context, filter commentdomain.ListFilter) (commentdomain.ListResult, error) {
	q := r.db.WithContext(ctx).Model(&CommentModel{})
	if filter.PostUUID != nil {
		q = q.Where("comments.post_id = "+idOf("posts"), *filter.PostUUID)
	}

	switch {
	case filter.ParentUUID != nil:
		q = q.Where("comments.parent_id = "+idOf("comments"), *filter.ParentUUID)
	case filter.RootsOnly:
		q = q.Where("comments.parent_id IS NULL")
	}

	if filter.AuthorUUID != nil {
		q = q.Where("comments.author_id = "+idOf("users"), *filter.AuthorUUID)
	}

	if len(filter.Statuses) > 0 {
		statuses := make([]string, 0, len(filter.Statuses))
		for _, status := range filter.Statuses {
			statuses = append(statuses, string(status))
		}

		q = q.Where("comments.status IN ?", statuses)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return commentdomain.ListResult{}, err
	}

	order := "comments.created_at ASC, comments.id ASC"
	if filter.NewestFirst {
		order = "comments.created_at DESC, comments.id DESC"
	}

	var models []CommentModel

	err := q.Select(commentColumns).Order(order).
		Offset((filter.Page - 1) * filter.PerPage).Limit(filter.PerPage).
		Find(&models).Error
	if err != nil {
		return commentdomain.ListResult{}, err
	}

	items := make([]commentdomain.Comment, 0, len(models))
	for _, model := range models {
		items = append(items, mapComment(model))
	}

	return commentdomain.ListResult{Items: items, Total: total, Page: filter.Page, PerPage: filter.PerPage}, nil
}

// Create inserts a comment and returns it with resolved references.
func (r *CommentRepository) Create(ctx context.Context, comment commentdomain.Comment) (commentdomain.Comment, error) {
	db := r.db.WithContext(ctx)

	postRowID, err := idByUUID(db, "posts", comment.PostUUID)
	if errors.Is(err, errUnknownReference) {
		return commentdomain.Comment{}, commentdomain.ErrPostNotFound
	}

	if err != nil {
		return commentdomain.Comment{}, err
	}

	parentRowID, err := optionalIDByUUID(db, "comments", comment.ParentUUID)
	if errors.Is(err, errUnknownReference) {
		return commentdomain.Comment{}, commentdomain.ErrParentInvalid
	}

	if err != nil {
		return commentdomain.Comment{}, err
	}

	authorRowID, err := optionalIDByUUID(db, "users", comment.AuthorUUID)
	if err != nil {
		return commentdomain.Comment{}, err
	}

	model := CommentModel{
		UUID:        comment.UUID,
		PostID:      postRowID,
		ParentID:    parentRowID,
		AuthorID:    authorRowID,
		AuthorName:  nullableString(comment.AuthorName),
		AuthorEmail: nullableString(comment.AuthorEmail),
		IPHash:      nullableString(comment.IPHash),
		UserAgent:   nullableString(comment.UserAgent),
		Content:     comment.Content,
		ContentHTML: comment.ContentHTML,
		Status:      string(comment.Status),
		Depth:       comment.Depth,
		CreatedAt:   comment.CreatedAt,
		UpdatedAt:   comment.UpdatedAt,
	}
	if err := db.Create(&model).Error; err != nil {
		return commentdomain.Comment{}, err
	}

	return r.GetByID(ctx, model.UUID)
}

// Update persists content, status, and soft-delete changes.
func (r *CommentRepository) Update(ctx context.Context, comment commentdomain.Comment) (commentdomain.Comment, error) {
	db := r.db.WithContext(ctx)

	deletedBy, err := optionalIDByUUID(db, "users", comment.DeletedByUUID)
	if err != nil {
		return commentdomain.Comment{}, err
	}

	res := db.Model(&CommentModel{}).Where("uuid = ?", comment.UUID).Updates(map[string]any{
		"content":      comment.Content,
		"content_html": comment.ContentHTML,
		"status":       string(comment.Status),
		"edited_at":    comment.EditedAt,
		"deleted_at":   comment.DeletedAt,
		"deleted_by":   deletedBy,
		"updated_at":   comment.UpdatedAt,
	})
	if res.Error != nil {
		return commentdomain.Comment{}, res.Error
	}

	if res.RowsAffected == 0 {
		return commentdomain.Comment{}, commentdomain.ErrNotFound
	}

	return r.GetByID(ctx, comment.UUID)
}

// AddFlag records a flag once per reporter and moves an approved comment to flagged at threshold.
// It reports whether the flag was newly recorded.
func (r *CommentRepository) AddFlag(ctx context.Context, flag commentdomain.Flag, threshold int) (bool, error) {
	added := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		commentRowID, err := idByUUID(tx, "comments", flag.CommentUUID)
		if errors.Is(err, errUnknownReference) {
			return commentdomain.ErrNotFound
		}

		if err != nil {
			return err
		}

		reporterRowID, err := optionalIDByUUID(tx, "users", flag.ReporterUUID)
		if err != nil {
			return err
		}

		res := tx.Exec(`
			INSERT INTO comment_flags (comment_id, reporter_user_id, reporter_ip_hash, reason_code, details, created_at)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT DO NOTHING`,
			commentRowID, reporterRowID, nullableString(flag.ReporterIPHash), flag.Reason,
			nullableString(flag.Details), flag.CreatedAt)
		if res.Error != nil {
			return res.Error
		}

		if res.RowsAffected == 0 {
			return nil
		}

		added = true

		return tx.Exec(`
			UPDATE comments
			SET flag_count = flag_count + 1,
			    status = CASE WHEN status = ? AND flag_count + 1 >= ? THEN ? ELSE status END
			WHERE id = ?`,
			string(commentdomain.StatusApproved), threshold, string(commentdomain.StatusFlagged), commentRowID).Error
	})

	return added, err
}

// ToggleUpvote adds or removes the voter's upvote and returns the new state and count.
func (r *CommentRepository) ToggleUpvote(ctx context.Context, commentID, voterID uuid.UUID, at time.Time) (bool, int, error) {
	var (
		upvoted bool
		count   int
	)

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		commentRowID, err := idByUUID(tx, "comments", commentID)
		if errors.Is(err, errUnknownReference) {
			return commentdomain.ErrNotFound
		}

		if err != nil {
			return err
		}

		voterRowID, err := idByUUID(tx, "users", voterID)
		if err != nil {
			return err
		}

		removed := tx.Exec(`DELETE FROM comment_upvotes WHERE comment_id = ? AND voter_user_id = ?`,
			commentRowID, voterRowID)
		if removed.Error != nil {
			return removed.Error
		}

		delta := -1

		if removed.RowsAffected == 0 {
			inserted := tx.Exec(`
				INSERT INTO comment_upvotes (comment_id, voter_user_id, created_at)
				VALUES (?, ?, ?)
				ON CONFLICT DO NOTHING`, commentRowID, voterRowID, at)
			if inserted.Error != nil {
				return inserted.Error
			}

			upvoted = true
			delta = int(inserted.RowsAffected)
		}

		return tx.Raw(`
			UPDATE comments SET upvote_count = GREATEST(upvote_count + ?, 0)
			WHERE id = ? RETURNING upvote_count`, delta, commentRowID).Row().Scan(&count)
	})

	return upvoted, count, err
}

func mapComment(model CommentModel) commentdomain.Comment {
	return commentdomain.Comment{
		ID:             model.ID,
		UUID:           model.UUID,
		PostUUID:       model.PostUUID,
		ParentUUID:     model.ParentUUID,
		AuthorUUID:     model.AuthorUUID,
		AuthorUsername: derefString(model.AuthorUsername),
		AuthorName:     derefString(model.AuthorName),
		AuthorEmail:    derefString(model.AuthorEmail),
		IPHash:         derefString(model.IPHash),
		UserAgent:      derefString(model.UserAgent),
		Content:        model.Content,
		ContentHTML:    model.ContentHTML,
		Status:         commentdomain.Status(model.Status),
		Depth:          model.Depth,
		UpvoteCount:    model.UpvoteCount,
		FlagCount:      model.FlagCount,
		ReplyCount:     model.ReplyCount,
		EditedAt:       model.EditedAt,
		CreatedAt:      model.CreatedAt,
		UpdatedAt:      model.UpdatedAt,
		DeletedAt:      model.DeletedAt,
		DeletedByUUID:  model.DeletedByUUID,

		ModeratedByUUID:  model.ModeratedByUUID,
		ModerationReason: derefString(model.ModerationReason),
		ModeratedAt:      model.ModeratedAt,
	}
}
