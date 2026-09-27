package handlers

import (
	"errors"
	"io"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/require"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
)

func TestDecodeProfilePatchRejects(t *testing.T) {
	t.Parallel()

	readFailed := errors.New("connection reset")

	tests := []struct {
		name string
		body io.Reader
		want error
	}{
		{name: "unreadable body", body: iotest.ErrReader(readFailed), want: readFailed},
		{name: "oversized body", body: strings.NewReader(`{"bio":"` + strings.Repeat("x", maxProfilePatchBytes) + `"}`), want: errProfileFieldType},
		{name: "socialLinks not an object", body: strings.NewReader(`{"socialLinks":"@me"}`), want: errProfileFieldType},
		{name: "socialLinks field has the wrong type", body: strings.NewReader(`{"socialLinks":{"github":42}}`), want: errProfileFieldType},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			patch, err := decodeProfilePatch(tc.body)
			require.ErrorIs(t, err, tc.want)
			require.Equal(t, userdomain.ProfilePatch{}, patch)
		})
	}
}

func TestDecodeProfilePatchNullSocialLinksClearsEveryLink(t *testing.T) {
	t.Parallel()

	patch, err := decodeProfilePatch(strings.NewReader(`{"socialLinks":null}`))
	require.NoError(t, err)

	cleared := userdomain.Change[string]{Set: true}
	require.Equal(t, cleared, patch.Twitter)
	require.Equal(t, cleared, patch.LinkedIn)
	require.Equal(t, cleared, patch.GitHub)
}
