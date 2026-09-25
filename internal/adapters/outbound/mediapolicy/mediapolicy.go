// Package mediapolicy reads the media transform presets and defaults from the site settings.
package mediapolicy

import (
	"context"

	mediadomain "github.com/turahe/blog-api/internal/core/media/domain"
	"github.com/turahe/blog-api/internal/core/media/ports"
	settingsdomain "github.com/turahe/blog-api/internal/core/settings/domain"
)

// SettingsReader is the part of the settings service the policy needs.
type SettingsReader interface {
	Values(ctx context.Context) (settingsdomain.Values, error)
}

// Settings implements ports.PolicySource over the settings service.
type Settings struct {
	settings SettingsReader
}

var _ ports.PolicySource = (*Settings)(nil)

// New returns a policy source over settings.
func New(settings SettingsReader) *Settings {
	return &Settings{settings: settings}
}

// TransformPolicy returns the current presets, quality, and default format. The settings
// service validated the values on write, so a preset that no longer parses is skipped.
func (s *Settings) TransformPolicy(ctx context.Context) (mediadomain.TransformPolicy, error) {
	values, err := s.settings.Values(ctx)
	if err != nil {
		return mediadomain.TransformPolicy{}, err
	}

	policy := mediadomain.TransformPolicy{Quality: int(values.Int("media.default_transform_quality"))}

	if format := values.String("media.default_transform_format"); format != mediadomain.FormatOriginal {
		policy.Format = format
	}

	for _, spec := range values.Strings("media.variants") {
		if v, err := mediadomain.ParseVariant(spec); err == nil {
			policy.Variants = append(policy.Variants, v)
		}
	}

	return policy, nil
}
