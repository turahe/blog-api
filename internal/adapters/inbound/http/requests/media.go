package requests

// PresignMedia is POST /api/v1/admin/media.
type PresignMedia struct {
	OriginalFilename string   `json:"originalFilename" binding:"required,min=1,max=255"`
	ContentType      string   `json:"contentType" binding:"required"`
	SizeBytes        int64    `json:"sizeBytes" binding:"required,gt=0"`
	Tags             []string `json:"tags"`
}

// PatchMediaTags is PATCH /api/v1/admin/media/:id/tags.
type PatchMediaTags struct {
	Tags []string `json:"tags" binding:"required"`
}
