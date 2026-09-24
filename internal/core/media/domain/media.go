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

// PresignResult is a pending asset with the presigned upload URL and required headers.
type PresignResult struct {
	Asset           MediaAsset
	UploadURL       string
	RequiredHeaders map[string]string
	ExpiresAt       time.Time
}

// ListFilter selects and pages media assets.
type ListFilter struct {
	Page    int
	PerPage int
	Query   string
	Disk    string
	Status  string
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
