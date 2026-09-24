package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	mediadomain "github.com/turahe/blog-api/internal/core/media/domain"
	"golang.org/x/image/webp"
)

// MaxImageDimension bounds each side of an uploaded image in pixels.
const MaxImageDimension = 8192

// sniffLen is the prefix length http.DetectContentType considers.
const sniffLen = 512

type imageFormat struct {
	ext          string
	decodeConfig func(io.Reader) (image.Config, error)
}

var imageFormats = map[string]imageFormat{
	"image/jpeg": {ext: ".jpg", decodeConfig: jpeg.DecodeConfig},
	"image/png":  {ext: ".png", decodeConfig: png.DecodeConfig},
	"image/gif":  {ext: ".gif", decodeConfig: gif.DecodeConfig},
	"image/webp": {ext: ".webp", decodeConfig: webp.DecodeConfig},
}

// UploadImage validates an image by its bytes, stores it, and records a ready asset.
func (s *Service) UploadImage(ctx context.Context, input mediadomain.ImageUpload) (mediadomain.MediaAsset, error) {
	sanitized, err := sanitizeFilename(input.Filename)
	if err != nil {
		return mediadomain.MediaAsset{}, err
	}

	size := int64(len(input.Data))
	if limit := s.uploadLimit(input.MaxBytes); size < 1 || size > limit {
		return mediadomain.MediaAsset{}, fmt.Errorf("%w: invalid size %d (max %d)", ErrValidation, size, limit)
	}

	contentType, format, config, err := s.inspectImage(input.Data)
	if err != nil {
		return mediadomain.MediaAsset{}, err
	}

	id := s.ids.New()
	key := fmt.Sprintf("media/%s/%s%s", id, strings.TrimSuffix(sanitized, filepath.Ext(sanitized)), format.ext)

	if err := s.storage.PutObject(ctx, key, contentType, input.Data); err != nil {
		return mediadomain.MediaAsset{}, fmt.Errorf("%w: put object: %w", ErrStorage, err)
	}

	sum := sha256.Sum256(input.Data)
	checksum := hex.EncodeToString(sum[:])
	width, height := config.Width, config.Height
	now := s.clock.Now()

	return s.repo.Create(ctx, mediadomain.MediaAsset{
		UUID:             id,
		StorageKey:       key,
		OriginalFilename: sanitized,
		ContentType:      contentType,
		SizeBytes:        size,
		Width:            &width,
		Height:           &height,
		ChecksumSHA256:   &checksum,
		Disk:             s.disk,
		Status:           mediadomain.StatusReady,
		UploadedByUUID:   input.UploadedBy,
		Tags:             append([]string(nil), input.Tags...),
		CreatedAt:        now,
		UpdatedAt:        now,
	})
}

// uploadLimit is the smaller positive limit of the service and the caller.
func (s *Service) uploadLimit(callerMax int64) int64 {
	if callerMax > 0 && (s.maxBytes <= 0 || callerMax < s.maxBytes) {
		return callerMax
	}

	return s.maxBytes
}

// inspectImage detects the type from magic bytes (it must be a known image type that
// config also allows) and reads the dimensions from the header without decoding pixels.
func (s *Service) inspectImage(data []byte) (string, imageFormat, image.Config, error) {
	contentType := http.DetectContentType(data)

	format, ok := imageFormats[contentType]
	if !ok || !s.isAllowedContentType(contentType) {
		return "", imageFormat{}, image.Config{}, fmt.Errorf("%w: content type %q not allowed", ErrValidation, contentType)
	}

	config, err := format.decodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", imageFormat{}, image.Config{}, fmt.Errorf("%w: unreadable %s image", ErrValidation, contentType)
	}

	if config.Width < 1 || config.Height < 1 || config.Width > MaxImageDimension || config.Height > MaxImageDimension {
		return "", imageFormat{}, image.Config{}, fmt.Errorf("%w: image dimensions %dx%d exceed %dx%d",
			ErrValidation, config.Width, config.Height, MaxImageDimension, MaxImageDimension)
	}

	return contentType, format, config, nil
}

// verifyContent rejects an object whose leading bytes contradict its declared type.
// Types with a recognisable signature must sniff as themselves; any other allowed
// type must at least not sniff as markup a browser could render.
func verifyContent(declared string, prefix []byte) error {
	sniffed, _, _ := strings.Cut(http.DetectContentType(prefix), ";")
	_, signed := imageFormats[declared]

	switch {
	case signed || declared == "application/pdf":
		if sniffed != declared {
			return fmt.Errorf("%w: content is %q, declared %q", ErrValidation, sniffed, declared)
		}
	case sniffed == "text/html" || sniffed == "text/xml":
		return fmt.Errorf("%w: content is %q, declared %q", ErrValidation, sniffed, declared)
	}

	return nil
}
