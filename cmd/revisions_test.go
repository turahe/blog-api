package cmd

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRevisionsPruneRejectsBadKeepBeforeConnecting(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{name: "missing keep", args: []string{}, wantErr: `required flag(s) "keep" not set`},
		{name: "zero keep", args: []string{"--keep", "0"}, wantErr: "--keep must be at least 1"},
		{name: "negative keep", args: []string{"--keep", "-3"}, wantErr: "--keep must be at least 1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := runCLI(t, append([]string{"revisions", "prune"}, tt.args...)...)
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}
