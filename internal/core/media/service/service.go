// Package service implements the presigned upload flow and media asset management.
package service

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	mediadomain "github.com/turahe/blog-api/internal/core/media/domain"
	"github.com/turahe/blog-api/internal/core/media/ports"
)

// Media service errors.
var (
	ErrValidation       = errors.New("validation error")
	ErrNotFound         = mediadomain.ErrNotFound
	ErrUploadIncomplete = errors.New("upload incomplete")
	ErrUploadExpired    = errors.New("upload expired")
	ErrStorage          = errors.New("storage error")
)

// IDGenerator returns new UUIDs.
type IDGenerator interface {
	New() uuid.UUID
}

// Clock returns the current time.
type Clock interface {
	Now() time.Time
}

// Service implements ports.Service.
type Service struct {
	repo       ports.Repository
	storage    ports.ObjectStorage
	ids        IDGenerator
	clock      Clock
	disk       string
	allowMIME  map[string]struct{}
	maxBytes   int64
	presignTTL time.Duration
}

// New returns a Service enforcing the allowed MIME types and size limit.
func New(
	repo ports.Repository,
	storage ports.ObjectStorage,
	ids IDGenerator,
	clock Clock,
	disk string,
	allowedMIME []string,
	maxBytes int64,
	presignTTL time.Duration,
) *Service {
	allowMIME := make(map[string]struct{}, len(allowedMIME))
	for _, mime := range allowedMIME {
		mime = strings.TrimSpace(strings.ToLower(mime))
		if mime == "" {
			continue
		}

		allowMIME[mime] = struct{}{}
	}

	return &Service{
		repo:       repo,
		storage:    storage,
		ids:        ids,
		clock:      clock,
		disk:       strings.TrimSpace(disk),
		allowMIME:  allowMIME,
		maxBytes:   maxBytes,
		presignTTL: presignTTL,
	}
}

// PresignUpload validates the upload, stores a pending asset, and returns a presigned PUT URL.
func (s *Service) PresignUpload(
	ctx context.Context,
	uploadedBy *uuid.UUID,
	filename, contentType string,
	sizeBytes int64,
	tags []string,
) (mediadomain.PresignResult, error) {
	sanitized, err := sanitizeFilename(filename)
	if err != nil {
		return mediadomain.PresignResult{}, err
	}

	contentType = strings.TrimSpace(strings.ToLower(contentType))
	if !s.isAllowedContentType(contentType) {
		return mediadomain.PresignResult{}, fmt.Errorf("%w: content type %q not allowed", ErrValidation, contentType)
	}

	if sizeBytes < 1 || sizeBytes > s.maxBytes {
		return mediadomain.PresignResult{}, fmt.Errorf("%w: invalid size %d", ErrValidation, sizeBytes)
	}

	now := s.clock.Now()
	expiresAt := now.Add(s.presignTTL)
	id := s.ids.New()
	asset := mediadomain.MediaAsset{
		UUID:             id,
		StorageKey:       fmt.Sprintf("media/%s/%s", id.String(), sanitized),
		OriginalFilename: sanitized,
		ContentType:      contentType,
		SizeBytes:        0,
		Disk:             s.disk,
		Status:           mediadomain.StatusPending,
		UploadedByUUID:   uploadedBy,
		Tags:             append([]string(nil), tags...),
		PresignExpiresAt: &expiresAt,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	asset, err = s.repo.Create(ctx, asset)
	if err != nil {
		return mediadomain.PresignResult{}, err
	}

	uploadURL, headers, err := s.storage.PresignPut(ctx, asset.StorageKey, contentType, s.presignTTL)
	if err != nil {
		return mediadomain.PresignResult{}, fmt.Errorf("%w: presign put: %w", ErrStorage, err)
	}

	return mediadomain.PresignResult{
		Asset:           asset,
		UploadURL:       uploadURL,
		RequiredHeaders: headers,
		ExpiresAt:       expiresAt,
	}, nil
}

// CompleteUpload verifies the uploaded object and marks the asset ready.
func (s *Service) CompleteUpload(ctx context.Context, id uuid.UUID) (mediadomain.MediaAsset, error) {
	asset, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return mediadomain.MediaAsset{}, ErrNotFound
		}

		return mediadomain.MediaAsset{}, err
	}

	if asset.DeletedAt != nil {
		return mediadomain.MediaAsset{}, ErrNotFound
	}

	if asset.Status == mediadomain.StatusReady {
		return asset, nil
	}

	if asset.Status == mediadomain.StatusPending && isExpired(s.clock.Now(), asset.PresignExpiresAt) {
		return mediadomain.MediaAsset{}, fmt.Errorf("%w: presign expired for %s", ErrUploadExpired, asset.UUID)
	}

	info, err := s.storage.HeadObject(ctx, asset.StorageKey)
	if err != nil {
		if errors.Is(err, ports.ErrObjectNotFound) {
			return mediadomain.MediaAsset{}, fmt.Errorf("%w: object %q", ErrUploadIncomplete, asset.StorageKey)
		}

		return mediadomain.MediaAsset{}, fmt.Errorf("%w: head object: %w", ErrStorage, err)
	}

	contentType := strings.TrimSpace(strings.ToLower(info.ContentType))
	if !s.isAllowedContentType(contentType) {
		return mediadomain.MediaAsset{}, fmt.Errorf("%w: content type %q not allowed", ErrValidation, contentType)
	}

	if info.Size < 1 || info.Size > s.maxBytes {
		return mediadomain.MediaAsset{}, fmt.Errorf("%w: invalid size %d", ErrValidation, info.Size)
	}

	asset.Status = mediadomain.StatusReady
	asset.SizeBytes = info.Size
	asset.ContentType = contentType
	asset.UpdatedAt = s.clock.Now()

	asset, err = s.repo.Update(ctx, asset)
	if err != nil {
		return mediadomain.MediaAsset{}, err
	}

	return asset, nil
}

// List returns a page of media assets.
func (s *Service) List(ctx context.Context, filter mediadomain.ListFilter) (mediadomain.ListResult, error) {
	if filter.Page < 1 {
		filter.Page = 1
	}

	if filter.PerPage < 1 || filter.PerPage > 100 {
		filter.PerPage = 20
	}

	filter.Query = strings.TrimSpace(filter.Query)
	filter.Disk = strings.TrimSpace(strings.ToLower(filter.Disk))
	filter.Status = strings.TrimSpace(strings.ToLower(filter.Status))

	return s.repo.List(ctx, filter)
}

// Get returns the asset in any status.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (mediadomain.MediaAsset, error) {
	asset, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return mediadomain.MediaAsset{}, ErrNotFound
		}

		return mediadomain.MediaAsset{}, err
	}

	return asset, nil
}

// GetReady returns the asset only when its upload is complete.
func (s *Service) GetReady(ctx context.Context, id uuid.UUID) (mediadomain.MediaAsset, error) {
	asset, err := s.Get(ctx, id)
	if err != nil {
		return mediadomain.MediaAsset{}, err
	}

	if asset.Status != mediadomain.StatusReady {
		return mediadomain.MediaAsset{}, ErrNotFound
	}

	return asset, nil
}

// Delete clears references to the asset from users, categories, and posts, then soft-deletes it.
func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	if _, err := s.Get(ctx, id); err != nil {
		return err
	}

	now := s.clock.Now()
	if err := s.repo.ClearEntityReferences(ctx, id); err != nil {
		return err
	}

	return s.repo.SoftDelete(ctx, id, now)
}

// UpdateTags replaces the asset's tags.
func (s *Service) UpdateTags(ctx context.Context, id uuid.UUID, tags []string) (mediadomain.MediaAsset, error) {
	asset, err := s.Get(ctx, id)
	if err != nil {
		return mediadomain.MediaAsset{}, err
	}

	cleaned := make([]string, 0, len(tags))
	seen := map[string]struct{}{}

	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}

		if len(tag) > 64 {
			return mediadomain.MediaAsset{}, fmt.Errorf("%w: tag too long", ErrValidation)
		}

		key := strings.ToLower(tag)
		if _, ok := seen[key]; ok {
			continue
		}

		seen[key] = struct{}{}

		cleaned = append(cleaned, tag)
		if len(cleaned) > 50 {
			return mediadomain.MediaAsset{}, fmt.Errorf("%w: too many tags", ErrValidation)
		}
	}

	asset.Tags = cleaned
	asset.UpdatedAt = s.clock.Now()

	return s.repo.Update(ctx, asset)
}

func (s *Service) isAllowedContentType(contentType string) bool {
	_, ok := s.allowMIME[contentType]
	return ok
}

func sanitizeFilename(filename string) (string, error) {
	filename = strings.TrimSpace(filename)
	if filename == "" {
		return "", fmt.Errorf("%w: filename required", ErrValidation)
	}

	if strings.ContainsAny(filename, `/\`) {
		return "", fmt.Errorf("%w: invalid filename", ErrValidation)
	}

	base := filepath.Base(filename)
	if base == "" || base == "." || base == ".." || base != filename {
		return "", fmt.Errorf("%w: invalid filename", ErrValidation)
	}

	return base, nil
}

func isExpired(now time.Time, expiresAt *time.Time) bool {
	if expiresAt == nil {
		return false
	}

	return now.After(*expiresAt) || now.Equal(*expiresAt)
}
