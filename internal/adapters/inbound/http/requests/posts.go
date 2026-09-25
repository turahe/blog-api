package requests

import "encoding/json"

// CreatePost is POST /api/v1/admin/posts.
type CreatePost struct {
	Title      string    `json:"title" binding:"required,min=1,max=255"`
	Slug       string    `json:"slug" binding:"omitempty,min=1,max=255"`
	Excerpt    string    `json:"excerpt" binding:"omitempty,max=2000"`
	Content    string    `json:"content" binding:"required"`
	CategoryID *string   `json:"category_id" binding:"omitempty,uuid"`
	Tags       *[]string `json:"tags"`
}

// UpdatePost is PATCH /api/v1/admin/posts/:id.
type UpdatePost struct {
	Title   *string `json:"title"`
	Slug    *string `json:"slug"`
	Excerpt *string `json:"excerpt"`
	Content *string `json:"content"`
	// Category UUID; null clears the category, omitting the field keeps it.
	CategoryID json.RawMessage `json:"category_id" swaggertype:"string" format:"uuid"`
	Tags       *[]string       `json:"tags"`
	// Who may comment: open, authenticated, read_only, or disabled (hides existing comments).
	CommentPolicy *string `json:"comment_policy" binding:"omitempty,oneof=open authenticated read_only disabled" enums:"open,authenticated,read_only,disabled"`
}

// ReplacePostMedia is PATCH /api/v1/admin/posts/:id/media.
type ReplacePostMedia struct {
	EnforceCoverConsistency *bool           `json:"enforce_cover_consistency"`
	Items                   []PostMediaItem `json:"items" binding:"required,dive"`
}

// PostMediaItem is one row in ReplacePostMedia.Items.
type PostMediaItem struct {
	MediaAssetID string `json:"media_asset_id" binding:"required,uuid"`
	Kind         string `json:"kind" binding:"required"`
	SortOrder    int    `json:"sort_order" binding:"gte=0"`
}
