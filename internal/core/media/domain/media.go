// Package domain holds media assets, upload presigning results, and post attachments.
package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// ErrNotFound means the media asset does not exist or was deleted.
var ErrNotFound = errors.New("media not found")

// Media asset lifecycle states.
const (
	StatusPending = "pending"
	StatusReady   = "ready"
	StatusFailed  = "failed"
)

// MediaAsset is an uploaded object and its metadata.
type MediaAsset struct {
	ID               int64
	UUID             uuid.UUID
	StorageKey       string
	OriginalFilename string
	ContentType      string
	SizeBytes        int64
	Width            *int
	Height           *int
	ChecksumSHA256   *string
	Disk             string
	Status           string
	UploadedByUUID   *uuid.UUID
	Tags             []string
	PresignExpiresAt *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
	DeletedAt        *time.Time
}

// ImageUpload is an image sent through the API rather than a presigned URL.
// The stored content type is sniffed from Data; the client-declared type is ignored.
type ImageUpload struct {
	UploadedBy *uuid.UUID
	Filename   string
	Data       []byte
	MaxBytes   int64
	Tags       []string
}

// PresignResult is a pending asset with the presigned upload URL and required headers.
type PresignResult struct {
	Asset           MediaAsset
	UploadURL       string
	RequiredHeaders map[string]string
	ExpiresAt       time.Time
}

// ListFilter selects and pages media assets. Unused keeps ready assets that no avatar,
// category, post cover, post attachment, or SEO image references; links inside post bodies
// are not tracked, so an unused asset may still be linked from Markdown.
type ListFilter struct {
	Page    int
	PerPage int
	Query   string
	Disk    string
	Status  string
	Unused  bool
}

// UsageFilter narrows a storage usage report to one uploader.
type UsageFilter struct {
	UploadedBy *uuid.UUID
	TopLimit   int
}

// UsageRow is a count and byte total for one group.
type UsageRow struct {
	Key   string
	Count int64
	Bytes int64
}

// UploaderUsage is one uploader's share. A nil UserUUID groups assets without an uploader.
type UploaderUsage struct {
	UserUUID *uuid.UUID
	Username string
	Count    int64
	Bytes    int64
}

// Usage is a storage usage report. Soft-deleted assets are grouped under status "deleted":
// their objects stay in the bucket until the orphan cleanup purges them.
type Usage struct {
	Total         UsageRow
	ByStatus      []UsageRow
	ByContentType []UsageRow
	TopUploaders  []UploaderUsage
}

// StatusDeleted is the usage group of soft-deleted assets.
const StatusDeleted = "deleted"

// PurgeResult counts what one orphan cleanup run removed.
type PurgeResult struct {
	Abandoned int
	Trashed   int
	Bytes     int64
}

// ListResult is a page of media assets.
type ListResult struct {
	Items   []MediaAsset
	Total   int64
	Page    int
	PerPage int
}

// Post media attachment kinds.
const (
	KindCover       = "cover"
	KindInlineImage = "inline_image"
	KindAttachment  = "attachment"
)

// PostMediaItem attaches a media asset to a post.
type PostMediaItem struct {
	MediaAssetUUID uuid.UUID
	Kind           string
	SortOrder      int
	Media          *MediaAsset
}
