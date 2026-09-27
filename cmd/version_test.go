package cmd

import (
	"regexp"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVersionCommand(t *testing.T) {
	t.Parallel()

	out, err := runCLI(t, "version")
	require.NoError(t, err)
	require.Regexp(t, `^version=\S+ commit=\S+ built=\S+ go=`+regexp.QuoteMeta(runtime.Version())+"\n$", out)

	_, err = runCLI(t, "version", "extra")
	require.ErrorContains(t, err, `unknown command "extra"`)
}
