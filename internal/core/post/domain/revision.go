package domain

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ErrRevisionNotFound is returned for a revision that does not exist on the post.
var ErrRevisionNotFound = fmt.Errorf("%w: revision", ErrNotFound)

// RevisionType names the write that produced a revision.
type RevisionType string

// Revision types.
const (
	RevisionCreate    RevisionType = "create"
	RevisionUpdate    RevisionType = "update"
	RevisionPublish   RevisionType = "publish"
	RevisionUnpublish RevisionType = "unpublish"
	RevisionArchive   RevisionType = "archive"
	RevisionDelete    RevisionType = "delete"
	RevisionUndelete  RevisionType = "undelete"
	RevisionRestore   RevisionType = "restore"
)

// Revision diff field names, in display order.
const (
	FieldTitle         = "title"
	FieldSlug          = "slug"
	FieldExcerpt       = "excerpt"
	FieldContent       = "content"
	FieldStatus        = "status"
	FieldCommentPolicy = "comment_policy"
	FieldCategory      = "category_id"
	FieldCoverImage    = "cover_image_media_id"
	FieldTags          = "tags"
	FieldMedia         = "media"
	FieldSEO           = "seo"
)

var fieldLabels = map[string]string{
	FieldTitle:         "title",
	FieldSlug:          "slug",
	FieldExcerpt:       "excerpt",
	FieldContent:       "content",
	FieldStatus:        "status",
	FieldCommentPolicy: "comment policy",
	FieldCategory:      "category",
	FieldCoverImage:    "cover image",
	FieldTags:          "tags",
	FieldMedia:         "media",
	FieldSEO:           "SEO",
}

// RevisionTag is a tag as it was linked when the revision was taken.
type RevisionTag struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// RevisionMedia is a post media attachment as it was when the revision was taken.
type RevisionMedia struct {
	MediaAssetID uuid.UUID `json:"media_asset_id"`
	Kind         string    `json:"kind"`
	SortOrder    int       `json:"sort_order"`
}

// Snapshot is the complete editable state of a post at one revision.
type Snapshot struct {
	Title               string
	Slug                string
	Excerpt             string
	Content             string
	Status              Status
	CommentPolicy       CommentPolicy
	CategoryUUID        *uuid.UUID
	CoverImageMediaUUID *uuid.UUID
	Tags                []RevisionTag
	Media               []RevisionMedia
	// SEO is the post's SEO fields as a JSON object; empty means none.
	SEO json.RawMessage
}

// SnapshotOf captures post with its tags and media.
func SnapshotOf(post Post, tags []RevisionTag, media []RevisionMedia) Snapshot {
	return Snapshot{
		Title:               post.Title,
		Slug:                post.Slug,
		Excerpt:             post.Excerpt,
		Content:             post.Content,
		Status:              post.Status,
		CommentPolicy:       post.CommentPolicy,
		CategoryUUID:        post.CategoryUUID,
		CoverImageMediaUUID: post.CoverImageMediaUUID,
		Tags:                tags,
		Media:               media,
	}
}

// Revision is one append-only entry in a post's history.
type Revision struct {
	UUID     uuid.UUID
	PostUUID uuid.UUID
	Number   int
	Type     RevisionType
	Snapshot Snapshot
	// ChangedFields lists the diff keys in display order.
	ChangedFields []string
	// Diff maps each changed field to its change: {from, to} for scalars, lengths for
	// content, and added/removed lists for tags and media.
	Diff       map[string]any
	Changelog  string
	EditorNote string
	AuthorUUID *uuid.UUID
	// RestoreFromUUID and RestoreFromNumber identify the revision a restore copied.
	RestoreFromUUID   *uuid.UUID
	RestoreFromNumber *int
	RequestID         string
	CreatedAt         time.Time
}

// RevisionRef selects a revision by public id or per-post number; exactly one is set.
type RevisionRef struct {
	UUID   *uuid.UUID
	Number *int
}

// ParseRevisionRef reads raw as a revision UUID or a positive revision number.
func ParseRevisionRef(raw string) (RevisionRef, error) {
	raw = strings.TrimSpace(raw)
	if id, err := uuid.Parse(raw); err == nil {
		return RevisionRef{UUID: &id}, nil
	}

	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || strconv.Itoa(n) != raw {
		return RevisionRef{}, fmt.Errorf("%w: revision must be a UUID or a positive number", ErrValidation)
	}

	return RevisionRef{Number: &n}, nil
}

// RevisionFilter selects and pages one post's revisions, newest first.
type RevisionFilter struct {
	PostUUID   uuid.UUID
	AuthorUUID *uuid.UUID
	From       *time.Time
	To         *time.Time
	Page       int
	PerPage    int
}

// RevisionPage is a page of revisions.
type RevisionPage struct {
	Items   []Revision
	Total   int64
	Page    int
	PerPage int
}

// Editor identifies who is changing posts in ctx, for revision attribution.
type Editor struct {
	UserID    *uuid.UUID
	RequestID string
}

type editorKey struct{}

// WithEditor returns ctx carrying editor.
func WithEditor(ctx context.Context, editor Editor) context.Context {
	return context.WithValue(ctx, editorKey{}, editor)
}

// EditorFrom returns the editor set by WithEditor, or the zero Editor.
func EditorFrom(ctx context.Context) Editor {
	editor, _ := ctx.Value(editorKey{}).(Editor)
	return editor
}

// DiffSnapshots returns the fields that differ from prev to next, in display order, and
// their changes. Content changes record lengths only; the snapshots hold the full text.
func DiffSnapshots(prev, next Snapshot) ([]string, map[string]any) {
	diff := map[string]any{}

	scalar := func(field string, from, to any) {
		diff[field] = map[string]any{"from": from, "to": to}
	}

	if prev.Title != next.Title {
		scalar(FieldTitle, prev.Title, next.Title)
	}

	if prev.Slug != next.Slug {
		scalar(FieldSlug, prev.Slug, next.Slug)
	}

	if prev.Excerpt != next.Excerpt {
		scalar(FieldExcerpt, prev.Excerpt, next.Excerpt)
	}

	if prev.Content != next.Content {
		diff[FieldContent] = map[string]any{"from_length": len(prev.Content), "to_length": len(next.Content)}
	}

	if prev.Status != next.Status {
		scalar(FieldStatus, prev.Status, next.Status)
	}

	if prev.CommentPolicy != next.CommentPolicy {
		scalar(FieldCommentPolicy, prev.CommentPolicy, next.CommentPolicy)
	}

	if !sameID(prev.CategoryUUID, next.CategoryUUID) {
		scalar(FieldCategory, prev.CategoryUUID, next.CategoryUUID)
	}

	if !sameID(prev.CoverImageMediaUUID, next.CoverImageMediaUUID) {
		scalar(FieldCoverImage, prev.CoverImageMediaUUID, next.CoverImageMediaUUID)
	}

	if change, ok := tagChange(prev.Tags, next.Tags); ok {
		diff[FieldTags] = change
	}

	if change, ok := mediaChange(prev.Media, next.Media); ok {
		diff[FieldMedia] = change
	}

	if !sameJSON(prev.SEO, next.SEO) {
		scalar(FieldSEO, rawOrNil(prev.SEO), rawOrNil(next.SEO))
	}

	return orderedFields(diff), diff
}

// Changelog summarises a revision for people, e.g. "Updated title and content".
func Changelog(typ RevisionType, fields []string, restoredFrom *int) string {
	labels := make([]string, 0, len(fields))

	for _, field := range fields {
		if typ != RevisionUpdate && field == FieldStatus {
			continue
		}

		labels = append(labels, fieldLabels[field])
	}

	lead := map[RevisionType]string{
		RevisionCreate:    "Created post",
		RevisionPublish:   "Published",
		RevisionUnpublish: "Unpublished",
		RevisionArchive:   "Archived",
		RevisionDelete:    "Moved to trash",
		RevisionUndelete:  "Restored from trash",
		RevisionRestore:   "Restored revision",
		RevisionUpdate:    "Updated",
	}[typ]

	if typ == RevisionRestore && restoredFrom != nil {
		lead = fmt.Sprintf("Restored revision %d", *restoredFrom)
	}

	switch {
	case typ == RevisionCreate:
		return lead
	case typ == RevisionUpdate && len(labels) == 0:
		return "Saved without changes"
	case typ == RevisionUpdate:
		return lead + " " + joinLabels(labels)
	case len(labels) == 0:
		return lead
	default:
		return lead + "; changed " + joinLabels(labels)
	}
}

func joinLabels(labels []string) string {
	if len(labels) == 1 {
		return labels[0]
	}

	return strings.Join(labels[:len(labels)-1], ", ") + " and " + labels[len(labels)-1]
}

func orderedFields(diff map[string]any) []string {
	order := []string{
		FieldTitle, FieldSlug, FieldExcerpt, FieldContent, FieldStatus, FieldCommentPolicy,
		FieldCategory, FieldCoverImage, FieldTags, FieldMedia, FieldSEO,
	}

	fields := make([]string, 0, len(diff))
	for _, field := range order {
		if _, ok := diff[field]; ok {
			fields = append(fields, field)
		}
	}

	return fields
}

func tagChange(prev, next []RevisionTag) (map[string]any, bool) {
	before := make(map[uuid.UUID]string, len(prev))
	for _, tag := range prev {
		before[tag.ID] = tag.Name
	}

	after := make(map[uuid.UUID]string, len(next))
	for _, tag := range next {
		after[tag.ID] = tag.Name
	}

	added := []string{}

	for _, tag := range next {
		if _, ok := before[tag.ID]; !ok {
			added = append(added, tag.Name)
		}
	}

	removed := []string{}

	for _, tag := range prev {
		if _, ok := after[tag.ID]; !ok {
			removed = append(removed, tag.Name)
		}
	}

	if len(added) == 0 && len(removed) == 0 {
		return nil, false
	}

	return map[string]any{"added": added, "removed": removed}, true
}

func mediaChange(prev, next []RevisionMedia) (map[string]any, bool) {
	key := func(m RevisionMedia) string { return m.MediaAssetID.String() + ":" + m.Kind }

	before := make(map[string]struct{}, len(prev))
	for _, m := range prev {
		before[key(m)] = struct{}{}
	}

	after := make(map[string]struct{}, len(next))
	for _, m := range next {
		after[key(m)] = struct{}{}
	}

	added := []RevisionMedia{}

	for _, m := range next {
		if _, ok := before[key(m)]; !ok {
			added = append(added, m)
		}
	}

	removed := []RevisionMedia{}

	for _, m := range prev {
		if _, ok := after[key(m)]; !ok {
			removed = append(removed, m)
		}
	}

	switch {
	case len(added) > 0 || len(removed) > 0:
		return map[string]any{"added": added, "removed": removed}, true
	case !slices.Equal(prev, next):
		return map[string]any{"reordered": true}, true
	default:
		return nil, false
	}
}

func sameID(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == b
	}

	return *a == *b
}

func sameJSON(a, b json.RawMessage) bool {
	return bytes.Equal(canonicalJSON(a), canonicalJSON(b))
}

func canonicalJSON(raw json.RawMessage) []byte {
	if len(bytes.TrimSpace(raw)) == 0 {
		return []byte("{}")
	}

	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return raw
	}

	out, err := json.Marshal(v)
	if err != nil {
		return raw
	}

	return out
}

func rawOrNil(raw json.RawMessage) any {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}

	return raw
}
