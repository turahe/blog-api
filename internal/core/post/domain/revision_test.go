package domain

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestDiffSnapshots(t *testing.T) {
	t.Parallel()

	cat := uuid.New()
	tagA, tagB := uuid.New(), uuid.New()
	media := uuid.New()

	prev := Snapshot{
		Title: "Old", Slug: "old", Content: "abc", Status: StatusDraft, CommentPolicy: CommentPolicyOpen,
		Tags:  []RevisionTag{{ID: tagA, Name: "a"}},
		Media: []RevisionMedia{{MediaAssetID: media, Kind: "inline_image", SortOrder: 0}},
	}
	next := prev
	next.Title = "New"
	next.Content = "abcdef"
	next.CategoryUUID = &cat
	next.Tags = []RevisionTag{{ID: tagB, Name: "b"}}
	next.SEO = json.RawMessage(`{ }`)

	fields, diff := DiffSnapshots(prev, next)

	require.Equal(t, []string{FieldTitle, FieldContent, FieldCategory, FieldTags}, fields, "an empty SEO object equals none")
	require.Equal(t, map[string]any{"from": "Old", "to": "New"}, diff[FieldTitle])
	require.Equal(t, map[string]any{"from_length": 3, "to_length": 6}, diff[FieldContent])
	require.Equal(t, map[string]any{"added": []string{"b"}, "removed": []string{"a"}}, diff[FieldTags])

	reordered := next
	reordered.Media = []RevisionMedia{{MediaAssetID: media, Kind: "inline_image", SortOrder: 3}}
	fields, diff = DiffSnapshots(next, reordered)
	require.Equal(t, []string{FieldMedia}, fields)
	require.Equal(t, map[string]any{"reordered": true}, diff[FieldMedia])

	fields, diff = DiffSnapshots(prev, prev)
	require.Empty(t, fields)
	require.Empty(t, diff)
}

func TestChangelog(t *testing.T) {
	t.Parallel()

	three := 3

	cases := []struct {
		typ    RevisionType
		fields []string
		from   *int
		want   string
	}{
		{RevisionCreate, nil, nil, "Created post"},
		{RevisionUpdate, nil, nil, "Saved without changes"},
		{RevisionUpdate, []string{FieldTitle}, nil, "Updated title"},
		{RevisionUpdate, []string{FieldTitle, FieldContent, FieldTags}, nil, "Updated title, content and tags"},
		{RevisionPublish, []string{FieldStatus}, nil, "Published"},
		{RevisionUndelete, []string{FieldStatus, FieldSlug}, nil, "Restored from trash; changed slug"},
		{RevisionRestore, []string{FieldContent}, &three, "Restored revision 3; changed content"},
	}

	for _, tc := range cases {
		require.Equal(t, tc.want, Changelog(tc.typ, tc.fields, tc.from))
	}
}

func TestParseRevisionRef(t *testing.T) {
	t.Parallel()

	id := uuid.New()

	ref, err := ParseRevisionRef(id.String())
	require.NoError(t, err)
	require.Equal(t, &id, ref.UUID)

	ref, err = ParseRevisionRef(" 12 ")
	require.NoError(t, err)
	require.Equal(t, 12, *ref.Number)

	for _, bad := range []string{"", "0", "-1", "+3", "007", "1.5", "abc"} {
		_, err := ParseRevisionRef(bad)
		require.ErrorIs(t, err, ErrValidation, bad)
	}
}

func TestEditorContext(t *testing.T) {
	t.Parallel()

	require.Equal(t, Editor{}, EditorFrom(t.Context()))

	id := uuid.New()
	ctx := WithEditor(t.Context(), Editor{UserID: &id, RequestID: "r"})
	require.Equal(t, Editor{UserID: &id, RequestID: "r"}, EditorFrom(ctx))
}
