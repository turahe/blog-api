package mediapolicy

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	mediadomain "github.com/turahe/blog-api/internal/core/media/domain"
	settingsdomain "github.com/turahe/blog-api/internal/core/settings/domain"
)

type fakeSettings struct {
	values map[string]any
	err    error
}

func (f fakeSettings) Values(context.Context) (settingsdomain.Values, error) {
	return settingsdomain.NewValues(f.values), f.err
}

func TestTransformPolicyReadsSettings(t *testing.T) {
	t.Parallel()

	policy, err := New(fakeSettings{values: map[string]any{
		"media.default_transform_quality": int64(82),
		"media.default_transform_format":  "avif",
		"media.variants":                  []string{"thumb:320:webp", "broken", "hero:1280"},
	}}).TransformPolicy(context.Background())
	require.NoError(t, err)
	require.Equal(t, mediadomain.TransformPolicy{
		Quality:  82,
		Format:   mediadomain.FormatAVIF,
		Variants: []mediadomain.Variant{{Name: "thumb", Width: 320, Format: "webp"}, {Name: "hero", Width: 1280}},
	}, policy)

	policy, err = New(fakeSettings{values: map[string]any{"media.default_transform_format": "original"}}).TransformPolicy(context.Background())
	require.NoError(t, err)
	require.Empty(t, policy.Format, "original keeps the source format")

	_, err = New(fakeSettings{err: errors.New("db down")}).TransformPolicy(context.Background())
	require.Error(t, err)
}
