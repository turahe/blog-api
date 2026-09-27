package responses

import (
	"testing"
	"time"

	"github.com/google/uuid"
	commentdomain "github.com/turahe/blog-api/internal/core/comment/domain"
)

var (
	commentID       = uuid.MustParse("0198a1b2-0000-7000-8000-0000000c0001")
	commentPostID   = uuid.MustParse("0198a1b2-0000-7000-8000-0000000c0002")
	commentParentID = uuid.MustParse("0198a1b2-0000-7000-8000-0000000c0003")
	commentAuthorID = uuid.MustParse("0198a1b2-0000-7000-8000-0000000c0004")
	commentReplyID  = uuid.MustParse("0198a1b2-0000-7000-8000-0000000c0005")
)

// memberComment is an approved top-level comment by a signed-in author.
func memberComment() commentdomain.Comment {
	return commentdomain.Comment{
		UUID:           commentID,
		PostUUID:       commentPostID,
		AuthorUUID:     &commentAuthorID,
		AuthorUsername: "gopher",
		AuthorEmail:    "gopher@example.com",
		IPHash:         "iphash",
		UserAgent:      "curl/8.5.0",
		Content:        "Nice *post*",
		ContentHTML:    "<p>Nice <em>post</em></p>",
		Status:         commentdomain.StatusApproved,
		UpvoteCount:    3,
		FlagCount:      1,
		ReplyCount:     2,
		CreatedAt:      time.Date(2026, 8, 1, 17, 0, 0, 0, time.FixedZone("WIB", 7*60*60)),
		UpdatedAt:      time.Date(2026, 8, 1, 11, 0, 0, 0, time.UTC),
	}
}

const memberCommentIDsJSON = `"id": "0198a1b2-0000-7000-8000-0000000c0001", "postId": "0198a1b2-0000-7000-8000-0000000c0002"`

const memberCommentTimesJSON = `"createdAt": "2026-08-01T10:00:00Z", "updatedAt": "2026-08-01T11:00:00Z"`

func TestComment(t *testing.T) {
	t.Parallel()

	reply := memberComment()
	reply.ParentUUID = &commentParentID
	reply.Depth = 1
	reply.EditedAt = new(time.Date(2026, 8, 1, 10, 30, 0, 0, time.UTC))

	guest := memberComment()
	guest.AuthorUUID, guest.AuthorUsername, guest.AuthorName = nil, "", "Visitor"

	anonymous := memberComment()
	anonymous.AuthorUUID, anonymous.AuthorUsername = nil, ""

	deleted := memberComment()
	deleted.Status = commentdomain.StatusDeleted

	tests := []struct {
		name    string
		comment commentdomain.Comment
		want    string
	}{
		{
			name:    "member comment hides email and IP hash",
			comment: memberComment(),
			want: `{` + memberCommentIDsJSON + `, "parentId": null, "depth": 0,
				"author": {"id": "0198a1b2-0000-7000-8000-0000000c0004", "name": "gopher", "guest": false},
				"content": "Nice *post*", "contentHtml": "<p>Nice <em>post</em></p>", "status": "approved",
				"upvoteCount": 3, "replyCount": 2, "editedAt": null, ` + memberCommentTimesJSON + `}`,
		},
		{
			name:    "edited reply",
			comment: reply,
			want: `{` + memberCommentIDsJSON + `, "parentId": "0198a1b2-0000-7000-8000-0000000c0003", "depth": 1,
				"author": {"id": "0198a1b2-0000-7000-8000-0000000c0004", "name": "gopher", "guest": false},
				"content": "Nice *post*", "contentHtml": "<p>Nice <em>post</em></p>", "status": "approved",
				"upvoteCount": 3, "replyCount": 2, "editedAt": "2026-08-01T10:30:00Z", ` + memberCommentTimesJSON + `}`,
		},
		{
			name:    "guest author",
			comment: guest,
			want: `{` + memberCommentIDsJSON + `, "parentId": null, "depth": 0,
				"author": {"id": null, "name": "Visitor", "guest": true},
				"content": "Nice *post*", "contentHtml": "<p>Nice <em>post</em></p>", "status": "approved",
				"upvoteCount": 3, "replyCount": 2, "editedAt": null, ` + memberCommentTimesJSON + `}`,
		},
		{
			name:    "no author",
			comment: anonymous,
			want: `{` + memberCommentIDsJSON + `, "parentId": null, "depth": 0, "author": null,
				"content": "Nice *post*", "contentHtml": "<p>Nice <em>post</em></p>", "status": "approved",
				"upvoteCount": 3, "replyCount": 2, "editedAt": null, ` + memberCommentTimesJSON + `}`,
		},
		{
			name:    "deleted comment keeps its place without content or author",
			comment: deleted,
			want: `{` + memberCommentIDsJSON + `, "parentId": null, "depth": 0, "author": null,
				"content": "", "contentHtml": "", "status": "deleted",
				"upvoteCount": 3, "replyCount": 2, "editedAt": null, ` + memberCommentTimesJSON + `}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertJSON(t, tt.want, Comment(tt.comment))
		})
	}
}

func TestCommentThread(t *testing.T) {
	t.Parallel()

	reply := memberComment()
	reply.UUID, reply.ParentUUID, reply.Depth, reply.ReplyCount = commentReplyID, &commentID, 1, 0

	const parentJSON = memberCommentIDsJSON + `, "parentId": null, "depth": 0,
		"author": {"id": "0198a1b2-0000-7000-8000-0000000c0004", "name": "gopher", "guest": false},
		"content": "Nice *post*", "contentHtml": "<p>Nice <em>post</em></p>", "status": "approved",
		"upvoteCount": 3, "replyCount": 2, "editedAt": null, ` + memberCommentTimesJSON

	tests := []struct {
		name   string
		thread commentdomain.Thread
		want   string
	}{
		{
			name:   "no replies",
			thread: commentdomain.Thread{Comment: memberComment()},
			want:   `{` + parentJSON + `, "replies": [], "repliesTotal": 0}`,
		},
		{
			name: "first page of replies",
			thread: commentdomain.Thread{
				Comment: memberComment(),
				Replies: commentdomain.ListResult{Items: []commentdomain.Comment{reply}, Total: 5, Page: 1, PerPage: 1},
			},
			want: `{` + parentJSON + `, "repliesTotal": 5, "replies": [{
				"id": "0198a1b2-0000-7000-8000-0000000c0005", "postId": "0198a1b2-0000-7000-8000-0000000c0002",
				"parentId": "0198a1b2-0000-7000-8000-0000000c0001", "depth": 1,
				"author": {"id": "0198a1b2-0000-7000-8000-0000000c0004", "name": "gopher", "guest": false},
				"content": "Nice *post*", "contentHtml": "<p>Nice <em>post</em></p>", "status": "approved",
				"upvoteCount": 3, "replyCount": 0, "editedAt": null, ` + memberCommentTimesJSON + `}]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertJSON(t, tt.want, CommentThread(tt.thread))
		})
	}
}
