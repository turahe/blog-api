package responses

import (
	"encoding/json"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
)

func uuidOrNil(id *uuid.UUID) any {
	if id == nil {
		return nil
	}

	return id.String()
}

// PostRevision serializes a revision summary; withDiff adds the per-field diff and
// withSnapshot the full post state the revision captured.
func PostRevision(rev postdomain.Revision, withDiff, withSnapshot bool) gin.H {
	fields := rev.ChangedFields
	if fields == nil {
		fields = []string{}
	}

	out := gin.H{
		"id":                           rev.UUID.String(),
		"post_id":                      rev.PostUUID.String(),
		"revision_number":              rev.Number,
		"revision_type":                string(rev.Type),
		"author_id":                    uuidOrNil(rev.AuthorUUID),
		"changed_fields":               fields,
		"changelog":                    rev.Changelog,
		"editor_note":                  rev.EditorNote,
		"restore_from_revision_id":     uuidOrNil(rev.RestoreFromUUID),
		"restore_from_revision_number": rev.RestoreFromNumber,
		"request_id":                   rev.RequestID,
		"created_at":                   rev.CreatedAt.UTC().Format(time.RFC3339),
	}

	if withDiff {
		diff := rev.Diff
		if diff == nil {
			diff = map[string]any{}
		}

		out["diff"] = diff
	}

	if withSnapshot {
		out["snapshot"] = revisionSnapshot(rev.Snapshot)
	}

	return out
}

func revisionSnapshot(snap postdomain.Snapshot) gin.H {
	tags := snap.Tags
	if tags == nil {
		tags = []postdomain.RevisionTag{}
	}

	media := snap.Media
	if media == nil {
		media = []postdomain.RevisionMedia{}
	}

	seo := json.RawMessage(`{}`)
	if len(snap.SEO) > 0 {
		seo = snap.SEO
	}

	return gin.H{
		"title":                snap.Title,
		"slug":                 snap.Slug,
		"excerpt":              snap.Excerpt,
		"content":              snap.Content,
		"status":               string(snap.Status),
		"comment_policy":       string(snap.CommentPolicy),
		"category_id":          uuidOrNil(snap.CategoryUUID),
		"cover_image_media_id": uuidOrNil(snap.CoverImageMediaUUID),
		"tags":                 tags,
		"media":                media,
		"seo":                  seo,
	}
}
