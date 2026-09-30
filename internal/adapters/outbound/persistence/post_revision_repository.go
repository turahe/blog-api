package persistence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
	"github.com/turahe/blog-api/internal/core/post/ports"
	"github.com/turahe/blog-api/internal/shared/pagination"
	"gorm.io/gorm"
)

// revisionsCfg declares the sort order for post revision listings.
// ORDER BY revision_number DESC matches the historical ordering;
// revision_number is unique per-post and provides the deterministic
// key required for keyset (cursor) pagination.
var revisionsCfg = pagination.CursorConfig{
	Kind: "post_revisions",
	Sort: []pagination.SortField{
		{Name: "revision_number", Column: "r.revision_number", Dir: pagination.Desc, Type: pagination.TypeInt64},
	},
	TTL:            pagination.DefaultTTL,
	MaxPerPage:     pagination.DefaultMaxPerPage,
	DefaultPerPage: pagination.DefaultPerPage,
}

// PostRevisionRepository stores post revisions in PostgreSQL.
type PostRevisionRepository struct {
	db *gorm.DB
}

// NewPostRevisionRepository returns a PostRevisionRepository over db.
func NewPostRevisionRepository(db *gorm.DB) *PostRevisionRepository {
	return &PostRevisionRepository{db: db}
}

type revisionRow struct {
	UUID                uuid.UUID
	PostUUID            uuid.UUID
	RevisionNumber      int
	RevisionType        string
	Title               string
	Slug                string
	Excerpt             string
	Content             string
	Status              string
	CommentPolicy       string
	CategoryUUID        *uuid.UUID
	CoverImageMediaUUID *uuid.UUID
	TagsSnapshot        string
	MediaSnapshot       string
	SEOSnapshot         string `gorm:"column:seo_snapshot"`
	ChangedFields       string
	Diff                string
	Changelog           string
	EditorNote          string
	AuthorUUID          *uuid.UUID
	RestoreFromUUID     *uuid.UUID
	RestoreFromNumber   *int
	RequestID           string
	CreatedAt           time.Time
}

// revisionSelect lists revision columns; %s is the content expression, so lists can
// skip the full text.
const revisionSelect = `
	SELECT r.uuid, p.uuid AS post_uuid, r.revision_number, r.revision_type, r.title, r.slug,
		r.excerpt, %s AS content, r.status, r.comment_policy, r.category_uuid, r.cover_image_media_uuid,
		r.tags_snapshot::text AS tags_snapshot, r.media_snapshot::text AS media_snapshot,
		r.seo_snapshot::text AS seo_snapshot, r.changed_fields::text AS changed_fields, r.diff::text AS diff,
		r.changelog, r.editor_note, u.uuid AS author_uuid, src.uuid AS restore_from_uuid,
		src.revision_number AS restore_from_number, r.request_id, r.created_at
	FROM post_revisions r
	JOIN posts p ON p.id = r.post_id
	LEFT JOIN users u ON u.id = r.author_id
	LEFT JOIN post_revisions src ON src.id = r.restore_from_revision_id`

// Latest implements ports.RevisionRepository.
func (r *PostRevisionRepository) Latest(ctx context.Context, postID uuid.UUID) (postdomain.Revision, error) {
	return r.one(ctx, `WHERE p.uuid = ? ORDER BY r.revision_number DESC LIMIT 1`, postID)
}

// Get implements ports.RevisionRepository.
func (r *PostRevisionRepository) Get(ctx context.Context, postID uuid.UUID, ref postdomain.RevisionRef) (postdomain.Revision, error) {
	if ref.UUID != nil {
		return r.one(ctx, `WHERE p.uuid = ? AND r.uuid = ?`, postID, *ref.UUID)
	}

	if ref.Number != nil {
		return r.one(ctx, `WHERE p.uuid = ? AND r.revision_number = ?`, postID, *ref.Number)
	}

	return postdomain.Revision{}, postdomain.ErrRevisionNotFound
}

func (r *PostRevisionRepository) one(ctx context.Context, where string, args ...any) (postdomain.Revision, error) {
	var rows []revisionRow
	if err := conn(ctx, r.db).Raw(fmt.Sprintf(revisionSelect, "r.content")+" "+where, args...).
		Scan(&rows).Error; err != nil {
		return postdomain.Revision{}, fmt.Errorf("get post revision: %w", err)
	}

	if len(rows) == 0 {
		return postdomain.Revision{}, postdomain.ErrRevisionNotFound
	}

	return mapRevision(rows[0])
}

// Create implements ports.RevisionRepository.
func (r *PostRevisionRepository) Create(ctx context.Context, rev postdomain.Revision) (postdomain.Revision, error) {
	tags, err := json.Marshal(nonNil(rev.Snapshot.Tags))
	if err != nil {
		return postdomain.Revision{}, err
	}

	media, err := json.Marshal(nonNil(rev.Snapshot.Media))
	if err != nil {
		return postdomain.Revision{}, err
	}

	fields, err := json.Marshal(nonNil(rev.ChangedFields))
	if err != nil {
		return postdomain.Revision{}, err
	}

	diff, err := json.Marshal(rev.Diff)
	if err != nil {
		return postdomain.Revision{}, err
	}

	seo := "{}"
	if len(rev.Snapshot.SEO) > 0 {
		seo = string(rev.Snapshot.SEO)
	}

	err = conn(ctx, r.db).Exec(`
		INSERT INTO post_revisions (uuid, post_id, revision_number, revision_type, title, slug, excerpt, content,
			status, comment_policy, category_uuid, cover_image_media_uuid, tags_snapshot, media_snapshot, seo_snapshot,
			changed_fields, diff, changelog, editor_note, author_id, restore_from_revision_id, request_id, created_at)
		VALUES (?, `+idOf("posts")+`, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?::jsonb, ?::jsonb, ?::jsonb, ?::jsonb, ?::jsonb,
			?, ?, `+idOf("users")+`, `+idOf("post_revisions")+`, ?, ?)`,
		rev.UUID, rev.PostUUID, rev.Number, string(rev.Type), rev.Snapshot.Title, rev.Snapshot.Slug,
		rev.Snapshot.Excerpt, rev.Snapshot.Content, string(rev.Snapshot.Status), string(rev.Snapshot.CommentPolicy),
		rev.Snapshot.CategoryUUID, rev.Snapshot.CoverImageMediaUUID, string(tags), string(media), seo,
		string(fields), string(diff), rev.Changelog, rev.EditorNote, rev.AuthorUUID, rev.RestoreFromUUID,
		rev.RequestID, rev.CreatedAt).Error
	if isUniqueViolation(err) {
		return postdomain.Revision{}, fmt.Errorf("%w: revision %d written concurrently", postdomain.ErrStaleVersion, rev.Number)
	}

	if err != nil {
		return postdomain.Revision{}, fmt.Errorf("create post revision: %w", err)
	}

	return rev, nil
}

// List implements ports.RevisionRepository.
func (r *PostRevisionRepository) List(ctx context.Context, filter postdomain.RevisionFilter) (postdomain.RevisionPage, error) {
	pr := filter.PageRequest
	if pr.Limit <= 0 {
		pr.Limit = revisionsCfg.DefaultPerPage
	}
	var cursorFields map[string]any
	if pr.Mode == pagination.ModeCursor && pr.Cursor != "" {
		decoded, _, err := pagination.DecodeCursor(revisionsCfg, pr.Cursor)
		if err != nil {
			return postdomain.RevisionPage{}, err
		}
		if err := pagination.ValidateCursor(revisionsCfg, decoded); err != nil {
			return postdomain.RevisionPage{}, err
		}
		cursorFields = decoded
	}
	seek, err := pagination.BuildSeek(revisionsCfg, pr, cursorFields)
	if err != nil {
		return postdomain.RevisionPage{}, err
	}

	conds := []string{"p.uuid = ?"}
	args := []any{filter.PostUUID}

	if filter.AuthorUUID != nil {
		conds = append(conds, "u.uuid = ?")
		args = append(args, *filter.AuthorUUID)
	}

	if filter.From != nil {
		conds = append(conds, "r.created_at >= ?")
		args = append(args, *filter.From)
	}

	if filter.To != nil {
		conds = append(conds, "r.created_at <= ?")
		args = append(args, *filter.To)
	}

	where := " WHERE " + strings.Join(conds, " AND ")
	seekArgs := make([]any, 0)
	if seek.WhereClause != "" {
		where += " AND " + seek.WhereClause
		seekArgs = seek.BindVars
	}

	countWhere := " WHERE " + strings.Join(conds, " AND ")
	c := conn(ctx, r.db)

	var out postdomain.RevisionPage
	if pr.IncludeTotal {
		var total int64
		if err := c.Raw(`SELECT count(*) FROM post_revisions r JOIN posts p ON p.id = r.post_id
			LEFT JOIN users u ON u.id = r.author_id`+countWhere, args...).Scan(&total).Error; err != nil {
			return postdomain.RevisionPage{}, fmt.Errorf("count post revisions: %w", err)
		}
		out.Total = &total
	}

	baseSQL := fmt.Sprintf(revisionSelect, "''") + where + " " + seek.OrderClause
	queryArgs := append(append([]any{}, args...), seekArgs...)
	queryArgs = append(queryArgs, seek.LimitFetch)
	if pr.Mode == pagination.ModeOffset {
		baseSQL += " OFFSET ?"
		queryArgs = append(queryArgs, pr.Offset)
	}

	var rows []revisionRow
	if err := c.Raw(baseSQL, queryArgs...).Scan(&rows).Error; err != nil {
		return postdomain.RevisionPage{}, fmt.Errorf("list post revisions: %w", err)
	}

	if seek.ReverseDisplay {
		for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
			rows[i], rows[j] = rows[j], rows[i]
		}
	}

	items := make([]postdomain.Revision, 0, len(rows))
	for _, row := range rows {
		rev, err := mapRevision(row)
		if err != nil {
			return postdomain.RevisionPage{}, err
		}
		items = append(items, rev)
	}

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
			fields := pagination.SortValues[postdomain.Revision](revisionsCfg, page[len(page)-1],
				func(row postdomain.Revision, i int) any {
					switch i {
					case 0:
						return row.Number
					}
					return nil
				})
			cur, err := pagination.EncodeCursor(revisionsCfg, fields)
			if err != nil {
				return postdomain.RevisionPage{}, err
			}
			out.NextCursor = cur
		}
		if out.HasPreviousPage {
			fields := pagination.SortValues[postdomain.Revision](revisionsCfg, page[0],
				func(row postdomain.Revision, i int) any {
					switch i {
					case 0:
						return row.Number
					}
					return nil
				})
			cur, err := pagination.EncodeCursor(revisionsCfg, fields)
			if err != nil {
				return postdomain.RevisionPage{}, err
			}
			out.PreviousCursor = cur
		}
	}

	return out, nil
}

// Existing implements ports.RevisionRepository.
func (r *PostRevisionRepository) Existing(ctx context.Context, refs ports.References) (ports.References, error) {
	c := conn(ctx, r.db)
	out := ports.References{Categories: []uuid.UUID{}, Tags: []uuid.UUID{}, Media: []uuid.UUID{}}

	lookups := []struct {
		ids   []uuid.UUID
		query string
		into  *[]uuid.UUID
	}{
		{refs.Categories, `SELECT uuid FROM categories WHERE uuid IN ?`, &out.Categories},
		{refs.Tags, `SELECT uuid FROM tags WHERE uuid IN ?`, &out.Tags},
		{refs.Media, `SELECT uuid FROM media_assets WHERE uuid IN ? AND status = 'ready' AND deleted_at IS NULL`, &out.Media},
	}

	for _, lookup := range lookups {
		if len(lookup.ids) == 0 {
			continue
		}

		if err := c.Raw(lookup.query, lookup.ids).Scan(lookup.into).Error; err != nil {
			return ports.References{}, fmt.Errorf("check revision references: %w", err)
		}
	}

	return out, nil
}

// Prune implements ports.RevisionRepository. A post's newest revision is always kept.
func (r *PostRevisionRepository) Prune(ctx context.Context, keep int) (int64, error) {
	if keep < 1 {
		return 0, errors.New("prune post revisions: keep must be at least 1")
	}

	res := conn(ctx, r.db).Exec(`
		DELETE FROM post_revisions r
		USING (
			SELECT id, row_number() OVER (PARTITION BY post_id ORDER BY revision_number DESC) AS rank
			FROM post_revisions
		) ranked
		WHERE r.id = ranked.id AND ranked.rank > ?`, keep)
	if res.Error != nil {
		return 0, fmt.Errorf("prune post revisions: %w", res.Error)
	}

	return res.RowsAffected, nil
}

func mapRevision(row revisionRow) (postdomain.Revision, error) {
	rev := postdomain.Revision{
		UUID:              row.UUID,
		PostUUID:          row.PostUUID,
		Number:            row.RevisionNumber,
		Type:              postdomain.RevisionType(row.RevisionType),
		Changelog:         row.Changelog,
		EditorNote:        row.EditorNote,
		AuthorUUID:        row.AuthorUUID,
		RestoreFromUUID:   row.RestoreFromUUID,
		RestoreFromNumber: row.RestoreFromNumber,
		RequestID:         row.RequestID,
		CreatedAt:         row.CreatedAt,
		Snapshot: postdomain.Snapshot{
			Title:               row.Title,
			Slug:                row.Slug,
			Excerpt:             row.Excerpt,
			Content:             row.Content,
			Status:              postdomain.Status(row.Status),
			CommentPolicy:       postdomain.CommentPolicy(row.CommentPolicy),
			CategoryUUID:        row.CategoryUUID,
			CoverImageMediaUUID: row.CoverImageMediaUUID,
			SEO:                 json.RawMessage(row.SEOSnapshot),
		},
	}

	decode := []struct {
		raw  string
		into any
	}{
		{row.TagsSnapshot, &rev.Snapshot.Tags},
		{row.MediaSnapshot, &rev.Snapshot.Media},
		{row.ChangedFields, &rev.ChangedFields},
		{row.Diff, &rev.Diff},
	}

	for _, d := range decode {
		if err := json.Unmarshal([]byte(d.raw), d.into); err != nil {
			return postdomain.Revision{}, fmt.Errorf("decode post revision %s: %w", row.UUID, err)
		}
	}

	return rev, nil
}

func nonNil[T any](items []T) []T {
	if items == nil {
		return []T{}
	}

	return items
}
