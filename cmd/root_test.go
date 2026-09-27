package cmd

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// runCLI executes a fresh command tree with --env-file - so no .env leaks into the process.
func runCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()

	root := newRootCmd()
	out := new(bytes.Buffer)
	root.SetOut(out)
	root.SetErr(new(bytes.Buffer))
	root.SetArgs(append([]string{"--env-file", "-"}, args...))

	err := root.Execute()

	return out.String(), err
}

func TestRootHelpListsGroupedCommands(t *testing.T) {
	t.Parallel()

	out, err := runCLI(t, "--help")
	require.NoError(t, err)

	require.Contains(t, out, "Runtime Commands:")
	require.Contains(t, out, "Data Commands:")
	require.Contains(t, out, "Ops Commands:")
	require.Contains(t, out, "serve")
	require.Contains(t, out, "migrate")
	require.Contains(t, out, "version")
}

func TestRootEnvFile(t *testing.T) {
	t.Parallel()

	missing := filepath.Join(t.TempDir(), "missing.env")

	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{name: "missing file fails before the command runs", args: []string{"--env-file", missing, "version"}, wantErr: "env file"},
		{name: "help ignores a missing file", args: []string{"--env-file", missing, "help"}},
		{name: "dash disables loading", args: []string{"--env-file", "-", "version"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := newRootCmd()
			out := new(bytes.Buffer)
			root.SetOut(out)
			root.SetErr(new(bytes.Buffer))
			root.SetArgs(tt.args)

			err := root.Execute()
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				require.Empty(t, out.String())

				return
			}

			require.NoError(t, err)
		})
	}
}
