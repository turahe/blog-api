package dotenv

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadSetsMissingKeysOnly(t *testing.T) {
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

	for _, key := range []string{"APP_ENV", "APP_JWT_ISSUER", "APP_QUOTED"} {
		t.Setenv(key, "")
		require.NoError(t, os.Unsetenv(key))
	}

	require.NoError(t, Load(path))
	require.Equal(t, "from-file", os.Getenv("APP_ENV"))
	require.Equal(t, "file-issuer", os.Getenv("APP_JWT_ISSUER"))
	require.Equal(t, "hello world", os.Getenv("APP_QUOTED"))
	require.Equal(t, "from-shell", os.Getenv("APP_KEEP"))
}

func TestLoadRejectsMalformedLine(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	require.NoError(t, os.WriteFile(path, []byte("NO_EQUALS\n"), 0o600))
	require.ErrorContains(t, Load(path), "expected KEY=VALUE")
}

func TestLoadMissingFile(t *testing.T) {
	t.Parallel()

	err := Load(filepath.Join(t.TempDir(), "missing.env"))
	require.ErrorIs(t, err, fs.ErrNotExist)
}

func TestLoadRejectsInvalidKey(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), ".env")
	require.NoError(t, os.WriteFile(path, []byte("# NUL bytes are not valid in variable names\nBAD\x00KEY=value\n"), 0o600))

	err := Load(path)
	require.ErrorContains(t, err, ":2: set")
}

//nolint:paralleltest // t.Chdir mutates the process working directory
func TestResolveDefaultLoadsDotEnvWhenPresent(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	require.NoError(t, os.WriteFile(".env", []byte("APP_ENV=local\n"), 0o600))

	path, err := Resolve("")
	require.NoError(t, err)
	require.Equal(t, ".env", path)
}

//nolint:paralleltest // t.Chdir mutates the process working directory
func TestResolveEmptyDisablesWhenMissing(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	path, err := Resolve("")
	require.NoError(t, err)
	require.Empty(t, path)
}

//nolint:paralleltest // t.Chdir mutates the process working directory
func TestResolveDefaultReportsUnreadableDotEnv(t *testing.T) {
	t.Chdir(t.TempDir())
	require.NoError(t, os.Symlink(".env", ".env"))

	path, err := Resolve("")
	require.ErrorContains(t, err, `env file ".env"`)
	require.Empty(t, path)
}

func TestResolveDashDisables(t *testing.T) {
	t.Parallel()

	path, err := Resolve("-")
	require.NoError(t, err)
	require.Empty(t, path)
}

func TestResolveExplicitMissingErrors(t *testing.T) {
	t.Parallel()

	_, err := Resolve(filepath.Join(t.TempDir(), "missing.env"))
	require.ErrorContains(t, err, "env file")
}

func TestResolveExplicitExisting(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "app.env")
	require.NoError(t, os.WriteFile(path, nil, 0o600))

	got, err := Resolve(path)
	require.NoError(t, err)
	require.Equal(t, path, got)
}

func TestParseLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		raw       string
		wantKey   string
		wantValue string
		wantSkip  bool
		wantErr   string
	}{
		{name: "blank", raw: "   ", wantSkip: true},
		{name: "comment", raw: "  # APP_ENV=x", wantSkip: true},
		{name: "plain", raw: "APP_ENV = local ", wantKey: "APP_ENV", wantValue: "local"},
		{name: "export", raw: "export  APP_ENV=local", wantKey: "APP_ENV", wantValue: "local"},
		{name: "single quoted", raw: "A='x y'", wantKey: "A", wantValue: "x y"},
		{name: "one char value", raw: "A=\"", wantKey: "A", wantValue: "\""},
		{name: "empty value", raw: "A=", wantKey: "A", wantValue: ""},
		{name: "mismatched quotes", raw: "A=\"x'", wantKey: "A", wantValue: "\"x'"},
		{name: "missing equals", raw: "A", wantErr: "expected KEY=VALUE"},
		{name: "empty key", raw: " =value", wantErr: "empty key"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			key, value, skip, err := parseLine(tt.raw)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.wantKey, key)
			require.Equal(t, tt.wantValue, value)
			require.Equal(t, tt.wantSkip, skip)
		})
	}
}
