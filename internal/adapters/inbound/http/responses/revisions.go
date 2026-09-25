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
	fields := camelNames(rev.ChangedFields)

	out := gin.H{
		"id":                        rev.UUID.String(),
		"postId":                    rev.PostUUID.String(),
		"revisionNumber":            rev.Number,
		"revisionType":              string(rev.Type),
		"authorId":                  uuidOrNil(rev.AuthorUUID),
		"changedFields":             fields,
		"changelog":                 rev.Changelog,
		"editorNote":                rev.EditorNote,
		"restoreFromRevisionId":     uuidOrNil(rev.RestoreFromUUID),
		"restoreFromRevisionNumber": rev.RestoreFromNumber,
		"requestId":                 rev.RequestID,
		"createdAt":                 rev.CreatedAt.UTC().Format(time.RFC3339),
	}

	if withDiff {
		diff := camelTree(rev.Diff)
		if rev.Diff == nil {
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

	media := make([]gin.H, len(snap.Media))
	for i, item := range snap.Media {
		media[i] = gin.H{"mediaAssetId": item.MediaAssetID, "kind": item.Kind, "sortOrder": item.SortOrder}
	}

	seo := map[string]any{}
	if len(snap.SEO) > 0 {
		// A snapshot that fails to decode shows no SEO rather than failing the whole revision.
		_ = json.Unmarshal(snap.SEO, &seo)
	}

	return gin.H{
		"title":             snap.Title,
		"slug":              snap.Slug,
		"excerpt":           snap.Excerpt,
		"content":           snap.Content,
		"status":            string(snap.Status),
		"commentPolicy":     string(snap.CommentPolicy),
		"categoryId":        uuidOrNil(snap.CategoryUUID),
		"coverImageMediaId": uuidOrNil(snap.CoverImageMediaUUID),
		"tags":              tags,
		"media":             media,
		"seo":               camelKeys(seo),
	}
}
