package requests

import (
	"testing"
)

func TestCreatePostValidation(t *testing.T) {
	t.Parallel()

	base := map[string]any{"title": "Hello", "slug": "hello", "content": "Body"}

	runBindCases[CreatePost](t, []bindCase{
		{name: "valid", body: with(base, "excerpt", "e", "categoryId", idA.String(), "tags", []string{"go"})},
		{name: "title required", body: with(base, "title", absent), want: errs("title", msgRequired("title"))},
		{name: "title too long", body: with(base, "title", long(256)), want: errs("title", msgMaxChars("title", 255))},
		{name: "slug required", body: with(base, "slug", absent), want: errs("slug", msgRequired("slug"))},
		{name: "slug too long", body: with(base, "slug", long(256)), want: errs("slug", msgMaxChars("slug", 255))},
		{name: "excerpt too long", body: with(base, "excerpt", long(2001)), want: errs("excerpt", msgMaxChars("excerpt", 2000))},
		{name: "content required", body: with(base, "content", absent), want: errs("content", msgRequired("content"))},
		{name: "categoryId must be a uuid", body: with(base, "categoryId", "x"), want: errs("categoryId", msgUUID("categoryId"))},
	})
}

func TestUpdatePostValidation(t *testing.T) {
	t.Parallel()

	runBindCases[UpdatePost](t, []bindCase{
		{name: "empty body is valid", body: `{}`},
		{name: "null category clears", body: `{"categoryId": null, "commentPolicy": "read_only"}`},
		{name: "comment policy unknown", body: `{"commentPolicy": "moderated"}`, want: errs("commentPolicy", msgOneOf("commentPolicy", "open authenticated read_only disabled"))},
		{name: "tags must be an array", body: `{"tags": "go"}`, want: msgGeneral("json: cannot unmarshal string into Go struct field UpdatePost.tags of type []string")},
	})
}

func TestReplacePostMediaValidation(t *testing.T) {
	t.Parallel()

	item := map[string]any{"mediaAssetId": idA.String(), "kind": "cover", "sortOrder": 0}

	runBindCases[ReplacePostMedia](t, []bindCase{
		{name: "valid", body: with(nil, "items", []any{item}, "enforceCoverConsistency", true)},
		{name: "empty items clears", body: `{"items": []}`},
		{name: "items required", body: `{}`, want: errs("items", msgRequired("items"))},
		{
			name: "item fields validated",
			body: `{"items": [{"sortOrder": 0}]}`,
			want: map[string][]string{
				"mediaAssetId": {msgRequired("mediaAssetId")},
				"kind":         {msgRequired("kind")},
			},
		},
	})
}
