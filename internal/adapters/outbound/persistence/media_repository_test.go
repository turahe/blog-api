package persistence

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMediaTagsNeverEncodesNull(t *testing.T) {
	t.Parallel()

	for name, tags := range map[string][]string{
		"nil":   nil,
		"empty": {},
	} {
		value, err := mediaTags(tags).Value()
		require.NoError(t, err, name)
		require.Equal(t, "{}", value, name)
	}
}

func TestMediaTagsCopiesInput(t *testing.T) {
	t.Parallel()

	in := []string{"a", "b"}
	out := mediaTags(in)
	in[0] = "changed"

	require.Equal(t, []string{"a", "b"}, []string(out))
}
