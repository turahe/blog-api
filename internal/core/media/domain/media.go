package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

var ErrNotFound = errors.New("media not found")

const (
	StatusPending = "pending"
	StatusReady   = "ready"
	StatusFailed  = "failed"
)

type MediaAsset struct {
	ID               uuid.UUID
	StorageKey       string
	OriginalFilename string
	ContentType      string
	SizeBytes        int64
	Width            *int
	Height           *int
	ChecksumSHA256   *string
	Disk             string
	Status           string
	UploadedBy       *uuid.UUID
	Tags             []string
	PresignExpiresAt *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
	DeletedAt        *time.Time
}

type PresignResult struct {
	Asset           MediaAsset
	UploadURL       string
	RequiredHeaders map[string]string
	ExpiresAt       time.Time
}
