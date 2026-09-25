package service

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	mediadomain "github.com/turahe/blog-api/internal/core/media/domain"
	"github.com/turahe/blog-api/internal/core/media/ports"
)

// ErrTransformDisabled is returned when no image transformer is configured.
var ErrTransformDisabled = errors.New("image transforms are not configured")

// WithTransforms enables TransformURL. Only widths in the allowlist are accepted, so the
// number of derivatives per image stays bounded.
func (s *Service) WithTransforms(transformer ports.Transformer, widths []int) *Service {
	s.transformer = transformer
	s.transformWidths = slices.Clone(widths)

	return s
}

// TransformURL validates t against the allowlists and returns the signed URL of the resized
// image. Only ready raster images can be transformed.
func (s *Service) TransformURL(ctx context.Context, id uuid.UUID, t mediadomain.Transform) (string, error) {
	if s.transformer == nil {
		return "", ErrTransformDisabled
	}

	if !slices.Contains(s.transformWidths, t.Width) {
		return "", fmt.Errorf("%w: width must be one of %v", ErrValidation, s.transformWidths)
	}

	if _, ok := mediadomain.TransformFormats[t.Format]; t.Format != "" && !ok {
		return "", fmt.Errorf("%w: format must be webp, avif, jpeg, or png", ErrValidation)
	}

	asset, err := s.GetReady(ctx, id)
	if err != nil {
		return "", err
	}

	if _, ok := mediadomain.TransformableTypes[asset.ContentType]; !ok {
		return "", fmt.Errorf("%w: media is not a transformable image", ErrValidation)
	}

	url, err := s.transformer.URL(asset, t)
	if err != nil {
		return "", fmt.Errorf("build transform url: %w", err)
	}

	return url, nil
}
