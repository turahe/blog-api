package cmd

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRootHelpListsGroupedCommands(t *testing.T) {
	root := newRootCmd()
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetArgs([]string{"--help"})
	require.NoError(t, root.Execute())
	out := buf.String()
	require.Contains(t, out, "Runtime Commands:")
	require.Contains(t, out, "Data Commands:")
	require.Contains(t, out, "Ops Commands:")
	require.Contains(t, out, "serve")
	require.Contains(t, out, "migrate")
	require.Contains(t, out, "version")
}

func TestVersionCommand(t *testing.T) {
	root := newRootCmd()
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetArgs([]string{"--env-file", "-", "version"})
	require.NoError(t, root.Execute())
	require.Contains(t, buf.String(), "version=")
}
