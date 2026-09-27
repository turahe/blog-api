package responses

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
)

func TestUUIDOrNil(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("0198a1b2-0000-7000-8000-000000004009")

	tests := []struct {
		name string
		id   *uuid.UUID
		want string
	}{
		{name: "nil", id: nil, want: `null`},
		{name: "set", id: &id, want: `"0198a1b2-0000-7000-8000-000000004009"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertJSON(t, tt.want, uuidOrNil(tt.id))
		})
	}
}

func TestPostRevision(t *testing.T) {
	t.Parallel()

	author := uuid.MustParse("0198a1b2-0000-7000-8000-000000004002")
	source := uuid.MustParse("0198a1b2-0000-7000-8000-000000004003")
	category := uuid.MustParse("0198a1b2-0000-7000-8000-000000004004")
	cover := uuid.MustParse("0198a1b2-0000-7000-8000-000000004005")
	tag := uuid.MustParse("0198a1b2-0000-7000-8000-000000004006")

	restore := postdomain.Revision{
		UUID:              uuid.MustParse("0198a1b2-0000-7000-8000-000000004001"),
		PostUUID:          postID,
		Number:            4,
		Type:              postdomain.RevisionRestore,
		ChangedFields:     []string{postdomain.FieldTitle},
		Diff:              map[string]any{postdomain.FieldTitle: map[string]any{"from": "B", "to": "A"}},
		Changelog:         "Restore v2",
		EditorNote:        "rollback",
		AuthorUUID:        &author,
		RestoreFromUUID:   &source,
		RestoreFromNumber: new(2),
		RequestID:         "req-9",
		CreatedAt:         time.Date(2026, 8, 1, 17, 0, 0, 0, time.FixedZone("WIB", 7*60*60)),
		Snapshot: postdomain.Snapshot{
			Title:               "A",
			Slug:                "a",
			Excerpt:             "x",
			Content:             "# A",
			Status:              postdomain.StatusPublished,
			CommentPolicy:       postdomain.CommentPolicyOpen,
			CategoryUUID:        &category,
			CoverImageMediaUUID: &cover,
			Tags:                []postdomain.RevisionTag{{ID: tag, Name: "Go"}},
			SEO:                 json.RawMessage(`not json`),
		},
	}

	const summary = `"id": "0198a1b2-0000-7000-8000-000000004001", "postId": "0198a1b2-0000-7000-8000-000000001001",
		"revisionNumber": 4, "revisionType": "restore", "authorId": "0198a1b2-0000-7000-8000-000000004002",
		"changedFields": ["title"], "changelog": "Restore v2", "editorNote": "rollback",
		"restoreFromRevisionId": "0198a1b2-0000-7000-8000-000000004003", "restoreFromRevisionNumber": 2,
		"requestId": "req-9", "createdAt": "2026-08-01T10:00:00Z"`

	tests := []struct {
		name         string
		withDiff     bool
		withSnapshot bool
		want         string
	}{
		{name: "summary only", want: `{` + summary + `}`},
		{
			name:     "with diff",
			withDiff: true,
			want:     `{` + summary + `, "diff": {"title": {"from": "B", "to": "A"}}}`,
		},
		{
			name:         "snapshot with undecodable SEO shows no SEO",
			withSnapshot: true,
			want: `{` + summary + `, "snapshot": {"title": "A", "slug": "a", "excerpt": "x", "content": "# A",
				"status": "published", "commentPolicy": "open",
				"categoryId": "0198a1b2-0000-7000-8000-000000004004",
				"coverImageMediaId": "0198a1b2-0000-7000-8000-000000004005",
				"tags": [{"id": "0198a1b2-0000-7000-8000-000000004006", "name": "Go"}],
				"media": [], "seo": {}}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertJSON(t, tt.want, PostRevision(restore, tt.withDiff, tt.withSnapshot))
		})
	}
}
