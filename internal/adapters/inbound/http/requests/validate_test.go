package requests

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin/binding"
	"github.com/stretchr/testify/require"
)

func TestValidationMessageHelpers(t *testing.T) {
	t.Parallel()
	require.Equal(t, "newPassword", lowerFirst("NewPassword"))
	require.Equal(t, map[string][]string{"_form": {"The request body is required."}}, validationErrorDetails(io.EOF))
}

func TestValidationErrorDetails(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
		dst  any
		want map[string][]string
	}{
		{
			name: "comment and raw newline are a syntax error",
			body: "{\n  \"title\": \"a\",\n  // \"slug\": \"a\",\n  \"content\": \"line\nline\"\n}",
			dst:  &CreatePost{},
			want: map[string][]string{"_form": {"The request body must be valid JSON (syntax error at byte 21)."}},
		},
		{
			name: "truncated body",
			body: `{"title": "a"`,
			dst:  &CreatePost{},
			want: map[string][]string{"_form": {"The request body must be valid JSON."}},
		},
		{
			name: "wrong JSON type is keyed by field",
			body: `{"title": "a", "slug": "a", "content": "b", "categoryId": 1}`,
			dst:  &CreatePost{},
			want: map[string][]string{"categoryId": {"The categoryId field must be a string."}},
		},
		{
			name: "wrong JSON type for an integer",
			body: `{"items": [{"mediaAssetId": "x", "kind": "cover", "sortOrder": "1"}]}`,
			dst:  &ReplacePostMedia{},
			want: map[string][]string{"items.0.sortOrder": {"The items.0.sortOrder field must be an integer."}},
		},
		{
			name: "rule failures use Laravel wording",
			body: `{"title": "` + strings.Repeat("x", 256) + `", "categoryId": "nope"}`,
			dst:  &CreatePost{},
			want: map[string][]string{
				"title":      {"The title field must not be greater than 255 characters."},
				"slug":       {"The slug field is required."},
				"content":    {"The content field is required."},
				"categoryId": {"The categoryId field must be a valid UUID."},
			},
		},
		{
			name: "nested fields use dotted paths",
			body: `{"items": [{"mediaAssetId": "nope", "sortOrder": -1}]}`,
			dst:  &ReplacePostMedia{},
			want: map[string][]string{
				"items.0.mediaAssetId": {"The items.0.mediaAssetId field must be a valid UUID."},
				"items.0.kind":         {"The items.0.kind field is required."},
				"items.0.sortOrder":    {"The items.0.sortOrder field must be greater than or equal to 0."},
			},
		},
		{
			name: "size rules are worded by kind",
			body: `{"subject": "s", "bodyMarkdown": "b", "lists": [], "status": "sent"}`,
			dst:  &NewsletterIssueCreate{},
			want: map[string][]string{
				"lists":  {"The lists field must have at least 1 items."},
				"status": {"The selected status is invalid."},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(tc.body))
			err := binding.JSON.Bind(req, tc.dst)
			require.Error(t, err)
			require.Equal(t, tc.want, validationErrorDetails(err))
		})
	}
}
