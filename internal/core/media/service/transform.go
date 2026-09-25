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

// WithPolicy applies admin-set presets, quality, and default format to transforms.
func (s *Service) WithPolicy(policy ports.PolicySource) *Service {
	s.policy = policy
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

	if !transformable(asset) {
		return "", fmt.Errorf("%w: media is not a transformable image", ErrValidation)
	}

	policy, err := s.transformPolicy(ctx)
	if err != nil {
		return "", err
	}

	url, err := s.transformer.URL(asset, policy.Apply(t))
	if err != nil {
		return "", fmt.Errorf("build transform url: %w", err)
	}

	return url, nil
}

// Variants returns the signed preset URLs of each ready raster asset. Presets are generated
// by the server, so they are not limited to MEDIA_TRANSFORM_WIDTHS.
func (s *Service) Variants(ctx context.Context, assets ...mediadomain.MediaAsset) (map[uuid.UUID]map[string]string, error) {
	if s.transformer == nil {
		return nil, nil
	}

	policy, err := s.transformPolicy(ctx)
	if err != nil {
		return nil, err
	}

	out := make(map[uuid.UUID]map[string]string, len(assets))

	for _, asset := range assets {
		urls := map[string]string{}

		if transformable(asset) {
			for _, v := range policy.Variants {
				url, err := s.transformer.URL(asset, policy.Apply(mediadomain.Transform{Width: v.Width, Format: v.Format}))
				if err != nil {
					return nil, fmt.Errorf("build variant url: %w", err)
				}

				urls[v.Name] = url
			}
		}

		out[asset.UUID] = urls
	}

	return out, nil
}

func (s *Service) transformPolicy(ctx context.Context) (mediadomain.TransformPolicy, error) {
	if s.policy == nil {
		return mediadomain.TransformPolicy{}, nil
	}

	policy, err := s.policy.TransformPolicy(ctx)
	if err != nil {
		return mediadomain.TransformPolicy{}, fmt.Errorf("read transform policy: %w", err)
	}

	return policy, nil
}

func transformable(asset mediadomain.MediaAsset) bool {
	if asset.Status != mediadomain.StatusReady || asset.DeletedAt != nil {
		return false
	}

	_, ok := mediadomain.TransformableTypes[asset.ContentType]

	return ok
}
