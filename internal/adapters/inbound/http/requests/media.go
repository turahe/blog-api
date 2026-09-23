package requests

// PresignMedia is POST /api/v1/admin/media.
type PresignMedia struct {
	OriginalFilename string   `json:"original_filename" binding:"required,min=1,max=255"`
	ContentType      string   `json:"content_type" binding:"required"`
	SizeBytes        int64    `json:"size_bytes" binding:"required,gt=0"`
	Tags             []string `json:"tags"`
}

// PatchMediaTags is PATCH /api/v1/admin/media/:id/tags.
type PatchMediaTags struct {
	Tags []string `json:"tags" binding:"required"`
}
