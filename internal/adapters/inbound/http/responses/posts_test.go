package responses

import (
	"testing"
	"time"

	"github.com/google/uuid"
	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
	tagdomain "github.com/turahe/blog-api/internal/core/tag/domain"
)

var (
	postID       = uuid.MustParse("0198a1b2-0000-7000-8000-000000001001")
	postAuthorID = uuid.MustParse("0198a1b2-0000-7000-8000-000000001002")
	postCategory = uuid.MustParse("0198a1b2-0000-7000-8000-000000001003")
	postCoverID  = uuid.MustParse("0198a1b2-0000-7000-8000-000000001004")
)

func draftPost() postdomain.Post {
	return postdomain.Post{
		ID:            11,
		UUID:          postID,
		AuthorUUID:    postAuthorID,
		Title:         "Hello",
		Slug:          "hello",
		Excerpt:       "Hi",
		Content:       "# Hello",
		Status:        postdomain.StatusDraft,
		CommentPolicy: postdomain.CommentPolicyOpen,
		Version:       2,
		CreatedAt:     time.Date(2026, 8, 1, 17, 0, 0, 0, time.FixedZone("WIB", 7*60*60)),
		UpdatedAt:     time.Date(2026, 8, 1, 11, 0, 0, 0, time.UTC),
	}
}

const draftPostJSON = `"id": "0198a1b2-0000-7000-8000-000000001001", "authorId": "0198a1b2-0000-7000-8000-000000001002",
	"categoryId": null, "title": "Hello", "slug": "hello", "excerpt": "Hi", "content": "# Hello",
	"coverImageMediaId": null, "status": "draft", "commentPolicy": "open", "publishedAt": null,
	"createdAt": "2026-08-01T10:00:00Z", "updatedAt": "2026-08-01T11:00:00Z"`

func TestPost(t *testing.T) {
	t.Parallel()

	published := draftPost()
	published.CategoryUUID = &postCategory
	published.CoverImageMediaUUID = &postCoverID
	published.Status = postdomain.StatusPublished
	published.CommentPolicy = postdomain.CommentPolicyReadOnly
	published.PublishedAt = new(time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC))

	tests := []struct {
		name string
		post postdomain.Post
		want string
	}{
		{name: "draft without references", post: draftPost(), want: `{` + draftPostJSON + `}`},
		{
			name: "published with category and cover",
			post: published,
			want: `{"id": "0198a1b2-0000-7000-8000-000000001001", "authorId": "0198a1b2-0000-7000-8000-000000001002",
				"categoryId": "0198a1b2-0000-7000-8000-000000001003", "title": "Hello", "slug": "hello",
				"excerpt": "Hi", "content": "# Hello", "coverImageMediaId": "0198a1b2-0000-7000-8000-000000001004",
				"status": "published", "commentPolicy": "read_only", "publishedAt": "2026-08-01T12:00:00Z",
				"createdAt": "2026-08-01T10:00:00Z", "updatedAt": "2026-08-01T11:00:00Z"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertJSON(t, tt.want, Post(tt.post))
		})
	}
}

func TestPostSearchHit(t *testing.T) {
	t.Parallel()

	hit := postdomain.SearchHit{Post: draftPost(), Rank: 0.5, Title: "<mark>Hello</mark>", Snippet: "&lt;b&gt; <mark>hello</mark>"}

	assertJSON(t, `{`+draftPostJSON+`, "search": {"rank": 0.5, "title": "<mark>Hello</mark>",
		"snippet": "&lt;b&gt; <mark>hello</mark>"}}`, PostSearchHit(hit))
}

func TestPostWithTags(t *testing.T) {
	t.Parallel()

	tag := tagdomain.Tag{
		ID:        5,
		UUID:      uuid.MustParse("0198a1b2-0000-7000-8000-000000001005"),
		Name:      "Go",
		Slug:      "go",
		CreatedAt: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
	}

	tests := []struct {
		name string
		tags []tagdomain.Tag
		want string
	}{
		{name: "nil tags", tags: nil, want: `{` + draftPostJSON + `, "tags": []}`},
		{name: "empty tags", tags: []tagdomain.Tag{}, want: `{` + draftPostJSON + `, "tags": []}`},
		{
			name: "tags",
			tags: []tagdomain.Tag{tag},
			want: `{` + draftPostJSON + `, "tags": [{"id": "0198a1b2-0000-7000-8000-000000001005", "name": "Go",
				"slug": "go", "createdAt": "2026-07-01T00:00:00Z"}]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertJSON(t, tt.want, PostWithTags(draftPost(), tt.tags))
		})
	}
}
