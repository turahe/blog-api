package responses

import (
	"testing"
	"time"

	"github.com/google/uuid"
	commentdomain "github.com/turahe/blog-api/internal/core/comment/domain"
)

var (
	moderatorID  = uuid.MustParse("0198a1b2-0000-7000-8000-0000000c00a1")
	reporterID   = uuid.MustParse("0198a1b2-0000-7000-8000-0000000c00a2")
	moderationID = uuid.MustParse("0198a1b2-0000-7000-8000-0000000c00a3")
	moderatedAt  = time.Date(2026, 8, 2, 9, 0, 0, 0, time.UTC)
)

// memberCommentAdminJSON is the moderator view of memberComment, without its closing brace.
const memberCommentAdminJSON = `{` + memberCommentIDsJSON + `, "parentId": null, "depth": 0,
	"author": {"id": "0198a1b2-0000-7000-8000-0000000c0004", "name": "gopher", "guest": false},
	"content": "Nice *post*", "contentHtml": "<p>Nice <em>post</em></p>", "status": "approved",
	"upvoteCount": 3, "replyCount": 2, "editedAt": null, ` + memberCommentTimesJSON + `,
	"authorEmail": "gopher@example.com", "ipHash": "iphash", "userAgent": "curl/8.5.0", "flagCount": 1,
	"moderatedBy": null, "moderationReason": null, "moderatedAt": null, "deletedAt": null, "deletedBy": null`

func approveEntry() commentdomain.ModerationEntry {
	return commentdomain.ModerationEntry{
		UUID:          moderationID,
		CommentUUID:   commentID,
		ModeratorUUID: &moderatorID,
		Action:        commentdomain.ActionApprove,
		FromStatus:    commentdomain.StatusFlagged,
		ToStatus:      commentdomain.StatusApproved,
		Reason:        "false alarm",
		NotifyAuthor:  true,
		Before:        map[string]any{"status": "flagged"},
		After:         map[string]any{"status": "approved"},
		CreatedAt:     moderatedAt,
	}
}

const approveEntryJSON = `{"id": "0198a1b2-0000-7000-8000-0000000c00a3", "commentId": "0198a1b2-0000-7000-8000-0000000c0001",
	"moderatorId": "0198a1b2-0000-7000-8000-0000000c00a1", "action": "approve",
	"fromStatus": "flagged", "toStatus": "approved", "reason": "false alarm", "notifyAuthor": true,
	"before": {"status": "flagged"}, "after": {"status": "approved"}, "createdAt": "2026-08-02T09:00:00Z"}`

func TestAdminComment(t *testing.T) {
	t.Parallel()

	deleted := memberComment()
	deleted.Status = commentdomain.StatusDeleted
	deleted.AuthorEmail, deleted.IPHash, deleted.UserAgent = "", "", ""
	deleted.ModeratedByUUID = &moderatorID
	deleted.ModerationReason = "spam link"
	deleted.ModeratedAt = &moderatedAt
	deleted.DeletedAt = &moderatedAt
	deleted.DeletedByUUID = &moderatorID

	tests := []struct {
		name    string
		comment commentdomain.Comment
		want    string
	}{
		{
			name:    "adds identity and moderation fields",
			comment: memberComment(),
			want:    memberCommentAdminJSON + `}`,
		},
		{
			name:    "deleted comment keeps content and author",
			comment: deleted,
			want: `{` + memberCommentIDsJSON + `, "parentId": null, "depth": 0,
				"author": {"id": "0198a1b2-0000-7000-8000-0000000c0004", "name": "gopher", "guest": false},
				"content": "Nice *post*", "contentHtml": "<p>Nice <em>post</em></p>", "status": "deleted",
				"upvoteCount": 3, "replyCount": 2, "editedAt": null, ` + memberCommentTimesJSON + `,
				"authorEmail": null, "ipHash": null, "userAgent": null, "flagCount": 1,
				"moderatedBy": "0198a1b2-0000-7000-8000-0000000c00a1", "moderationReason": "spam link",
				"moderatedAt": "2026-08-02T09:00:00Z", "deletedAt": "2026-08-02T09:00:00Z",
				"deletedBy": "0198a1b2-0000-7000-8000-0000000c00a1"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertJSON(t, tt.want, AdminComment(tt.comment))
		})
	}
}

func TestCommentReview(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		review commentdomain.Review
		want   string
	}{
		{
			name:   "no flags or history",
			review: commentdomain.Review{Comment: memberComment()},
			want:   memberCommentAdminJSON + `, "flags": [], "moderationLog": []}`,
		},
		{
			name: "member and guest flags with history",
			review: commentdomain.Review{
				Comment: memberComment(),
				Flags: []commentdomain.Flag{
					{CommentUUID: commentID, ReporterUUID: &reporterID, Reason: "spam", Details: "link farm", CreatedAt: moderatedAt},
					{CommentUUID: commentID, ReporterIPHash: "guesthash", Reason: "abuse", CreatedAt: moderatedAt.Add(time.Hour)},
				},
				History: []commentdomain.ModerationEntry{approveEntry()},
			},
			want: memberCommentAdminJSON + `,
				"flags": [
					{"reporterId": "0198a1b2-0000-7000-8000-0000000c00a2", "guest": false, "reasonCode": "spam",
					 "details": "link farm", "createdAt": "2026-08-02T09:00:00Z"},
					{"reporterId": null, "guest": true, "reasonCode": "abuse", "details": null,
					 "createdAt": "2026-08-02T10:00:00Z"}
				],
				"moderationLog": [` + approveEntryJSON + `]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertJSON(t, tt.want, CommentReview(tt.review))
		})
	}
}

func TestModerationEntry(t *testing.T) {
	t.Parallel()

	system := commentdomain.ModerationEntry{
		UUID:        moderationID,
		CommentUUID: commentID,
		Action:      commentdomain.ActionSpam,
		FromStatus:  commentdomain.StatusPending,
		ToStatus:    commentdomain.StatusSpam,
		CreatedAt:   moderatedAt,
	}

	tests := []struct {
		name  string
		entry commentdomain.ModerationEntry
		want  string
	}{
		{name: "moderator action", entry: approveEntry(), want: approveEntryJSON},
		{
			name:  "automatic action without reason",
			entry: system,
			want: `{"id": "0198a1b2-0000-7000-8000-0000000c00a3", "commentId": "0198a1b2-0000-7000-8000-0000000c0001",
				"moderatorId": null, "action": "spam", "fromStatus": "pending", "toStatus": "spam", "reason": null,
				"notifyAuthor": false, "before": null, "after": null, "createdAt": "2026-08-02T09:00:00Z"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertJSON(t, tt.want, ModerationEntry(tt.entry))
		})
	}
}

func TestCommentStats(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		stats commentdomain.Stats
		want  string
	}{
		{
			name:  "empty queue lists every status",
			stats: commentdomain.Stats{},
			want: `{"byStatus": {"pending": 0, "approved": 0, "flagged": 0, "spam": 0, "rejected": 0, "deleted": 0},
				"queueDepth": 0, "oldestQueuedAt": null, "topPosts": []}`,
		},
		{
			name: "busy queue",
			stats: commentdomain.Stats{
				ByStatus: map[commentdomain.Status]int64{
					commentdomain.StatusPending: 4, commentdomain.StatusFlagged: 1, commentdomain.StatusApproved: 40,
				},
				QueueDepth:     5,
				OldestQueuedAt: &moderatedAt,
				TopPosts:       []commentdomain.PostQueue{{PostUUID: commentPostID, PostTitle: "Hello", Pending: 3, Flagged: 1}},
			},
			want: `{"byStatus": {"pending": 4, "approved": 40, "flagged": 1, "spam": 0, "rejected": 0, "deleted": 0},
				"queueDepth": 5, "oldestQueuedAt": "2026-08-02T09:00:00Z",
				"topPosts": [{"postId": "0198a1b2-0000-7000-8000-0000000c0002", "title": "Hello", "pending": 3, "flagged": 1}]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertJSON(t, tt.want, CommentStats(tt.stats))
		})
	}
}
