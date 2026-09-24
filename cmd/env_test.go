package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadEnvFileSetsMissingKeysOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	require.NoError(t, os.WriteFile(path, []byte(""+
		"# comment\n"+
		"APP_ENV=from-file\n"+
		"export APP_JWT_ISSUER=file-issuer\n"+
		"APP_QUOTED=\"hello world\"\n"+
		"APP_KEEP=from-file\n",
	), 0o600))

	t.Setenv("APP_KEEP", "from-shell")
	require.NoError(t, os.Unsetenv("APP_ENV"))
	require.NoError(t, os.Unsetenv("APP_JWT_ISSUER"))
	require.NoError(t, os.Unsetenv("APP_QUOTED"))

	require.NoError(t, loadEnvFile(path))
	require.Equal(t, "from-file", os.Getenv("APP_ENV"))
	require.Equal(t, "file-issuer", os.Getenv("APP_JWT_ISSUER"))
	require.Equal(t, "hello world", os.Getenv("APP_QUOTED"))
	require.Equal(t, "from-shell", os.Getenv("APP_KEEP"))
}

func TestLoadEnvFileRejectsMalformedLine(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	require.NoError(t, os.WriteFile(path, []byte("NO_EQUALS\n"), 0o600))
	require.ErrorContains(t, loadEnvFile(path), "expected KEY=VALUE")
}

//nolint:paralleltest // t.Chdir mutates the process working directory
//nolint:paralleltest // t.Chdir mutates the process working directory
func TestResolveEnvFileDefaultLoadsDotEnvWhenPresent(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	require.NoError(t, os.WriteFile(".env", []byte("APP_ENV=local\n"), 0o600))

	path, err := resolveEnvFile("")
	require.NoError(t, err)
	require.Equal(t, ".env", path)
}

//nolint:paralleltest // t.Chdir mutates the process working directory
//nolint:paralleltest // t.Chdir mutates the process working directory
func TestResolveEnvFileEmptyDisablesWhenMissing(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	path, err := resolveEnvFile("")
	require.NoError(t, err)
	require.Empty(t, path)
}

func TestResolveEnvFileDashDisables(t *testing.T) {
	t.Parallel()

	path, err := resolveEnvFile("-")
	require.NoError(t, err)
	require.Empty(t, path)
}

func TestResolveEnvFileExplicitMissingErrors(t *testing.T) {
	t.Parallel()

	_, err := resolveEnvFile(filepath.Join(t.TempDir(), "missing.env"))
	require.ErrorContains(t, err, "env file")
}
