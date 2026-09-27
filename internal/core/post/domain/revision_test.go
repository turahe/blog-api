package domain

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestSnapshotOf(t *testing.T) {
	t.Parallel()

	cat, cover := uuid.New(), uuid.New()
	post := Post{
		ID: 7, UUID: uuid.New(), Title: "T", Slug: "t", Excerpt: "E", Content: "C", Status: StatusPublished,
		CommentPolicy: CommentPolicyReadOnly, CategoryUUID: &cat, CoverImageMediaUUID: &cover, Version: 4,
	}
	tags := []RevisionTag{{ID: uuid.New(), Name: "go"}}
	media := []RevisionMedia{{MediaAssetID: cover, Kind: "cover", SortOrder: 0}}

	require.Equal(t, Snapshot{
		Title: "T", Slug: "t", Excerpt: "E", Content: "C", Status: StatusPublished, CommentPolicy: CommentPolicyReadOnly,
		CategoryUUID: &cat, CoverImageMediaUUID: &cover, Tags: tags, Media: media,
	}, SnapshotOf(post, tags, media))
}

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

func TestDiffSnapshotsScalarFields(t *testing.T) {
	t.Parallel()

	cover := uuid.New()
	prev := Snapshot{Slug: "old", Excerpt: "a", Status: StatusDraft, CommentPolicy: CommentPolicyOpen}
	next := Snapshot{Slug: "new", Excerpt: "b", Status: StatusPublished, CommentPolicy: CommentPolicyDisabled, CoverImageMediaUUID: &cover}

	fields, diff := DiffSnapshots(prev, next)

	require.Equal(t, []string{FieldSlug, FieldExcerpt, FieldStatus, FieldCommentPolicy, FieldCoverImage}, fields)
	require.Equal(t, map[string]any{"from": "old", "to": "new"}, diff[FieldSlug])
	require.Equal(t, map[string]any{"from": "a", "to": "b"}, diff[FieldExcerpt])
	require.Equal(t, map[string]any{"from": StatusDraft, "to": StatusPublished}, diff[FieldStatus])
	require.Equal(t, map[string]any{"from": CommentPolicyOpen, "to": CommentPolicyDisabled}, diff[FieldCommentPolicy])
	require.Equal(t, map[string]any{"from": (*uuid.UUID)(nil), "to": &cover}, diff[FieldCoverImage])
}

func TestDiffSnapshotsMediaAddedAndRemoved(t *testing.T) {
	t.Parallel()

	kept, dropped, added := uuid.New(), uuid.New(), uuid.New()
	prev := Snapshot{Media: []RevisionMedia{{MediaAssetID: kept, Kind: "inline_image"}, {MediaAssetID: dropped, Kind: "inline_image"}}}
	next := Snapshot{Media: []RevisionMedia{{MediaAssetID: kept, Kind: "inline_image"}, {MediaAssetID: added, Kind: "cover"}}}

	fields, diff := DiffSnapshots(prev, next)

	require.Equal(t, []string{FieldMedia}, fields)
	require.Equal(t, map[string]any{
		"added":   []RevisionMedia{{MediaAssetID: added, Kind: "cover"}},
		"removed": []RevisionMedia{{MediaAssetID: dropped, Kind: "inline_image"}},
	}, diff[FieldMedia])
}

func TestDiffSnapshotsComparesUnparseableSEOVerbatim(t *testing.T) {
	t.Parallel()

	prev := Snapshot{SEO: json.RawMessage(`{"seo_title":`)}

	fields, _ := DiffSnapshots(prev, prev)
	require.Empty(t, fields, "identical bytes are unchanged")

	fields, _ = DiffSnapshots(prev, Snapshot{SEO: json.RawMessage(`{"seo_title":"x"}`)})
	require.Equal(t, []string{FieldSEO}, fields)
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
