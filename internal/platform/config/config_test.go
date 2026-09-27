package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//nolint:paralleltest // t.Setenv
func TestLoadRejectsUnreadableJWTKeys(t *testing.T) {
	tests := []struct {
		name    string
		pathKey string
		wantErr string
	}{
		{name: "private key", pathKey: "APP_JWT_PRIVATE_KEY_PATH", wantErr: "load JWT private key"},
		{name: "public key", pathKey: "APP_JWT_PUBLIC_KEY_PATH", wantErr: "load JWT public key"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setJWTKeys(t)
			t.Setenv(tt.pathKey, filepath.Join(t.TempDir(), "missing.pem"))

			_, err := Load()
			require.ErrorContains(t, err, tt.wantErr)
			require.ErrorIs(t, err, os.ErrNotExist)
		})
	}
}

func TestValidate(t *testing.T) {
	t.Parallel()

	const sessionKey = "production-session-key-32bytes-min!"

	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{
			name: "cloud sql without database name in production",
			cfg: Config{
				Address: ":8080", SessionKey: sessionKey, Environment: envProduction,
				DBInstanceConnectionName: "proj:region:inst", DBUser: "blog",
			},
			wantErr: "cloud SQL requires",
		},
		{
			name:    "no open connections",
			cfg:     Config{Address: ":8080", DBHost: "db", DBUser: "blog", DBName: "blog", DBMaxOpen: 0},
			wantErr: "invalid database pool limits: idle=0 open=0",
		},
		{
			name:    "more idle than open",
			cfg:     Config{Address: ":8080", DBHost: "db", DBUser: "blog", DBName: "blog", DBMaxOpen: 2, DBMaxIdle: 3},
			wantErr: "invalid database pool limits",
		},
		{
			name:    "negative idle",
			cfg:     Config{Address: ":8080", DBHost: "db", DBUser: "blog", DBName: "blog", DBMaxOpen: 2, DBMaxIdle: -1},
			wantErr: "invalid database pool limits",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := tt.cfg
			require.ErrorContains(t, cfg.validate(), tt.wantErr)
		})
	}
}

func TestValidateSecrets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		cfg            Config
		wantErr        string
		wantSessionKey string
	}{
		{name: "empty address", cfg: Config{}, wantErr: "APP_ADDR"},
		{name: "short session key in production", cfg: Config{Address: ":8080", Environment: envProduction, SessionKey: "short"}, wantErr: "APP_SESSION_KEY"},
		{
			name:           "short session key falls back locally",
			cfg:            Config{Address: ":8080", Environment: "local", SessionKey: "short"},
			wantSessionKey: "local-dev-session-key-32bytes-min!!",
		},
		{
			name:           "long session key kept",
			cfg:            Config{Address: ":8080", Environment: envProduction, SessionKey: strings.Repeat("k", 32)},
			wantSessionKey: strings.Repeat("k", 32),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := tt.cfg

			err := cfg.validateSecrets()
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantSessionKey, cfg.SessionKey)
		})
	}
}

//nolint:paralleltest // t.Setenv
func TestPEMFromEnvOrFile(t *testing.T) {
	const inlineKey, pathKey = "BLOG_TEST_PEM", "BLOG_TEST_PEM_PATH"

	dir := t.TempDir()
	emptyFile := filepath.Join(dir, "empty.pem")
	require.NoError(t, os.WriteFile(emptyFile, []byte("  \n"), 0o600))

	keyFile := filepath.Join(dir, "key.pem")
	require.NoError(t, os.WriteFile(keyFile, []byte("\n-----BEGIN KEY-----\nabc\n-----END KEY-----\n"), 0o600))

	tests := []struct {
		name    string
		inline  string
		path    string
		want    string
		wantErr string
	}{
		{name: "unset", want: ""},
		{name: "inline with escaped newlines", inline: `-----BEGIN KEY-----\nabc\n-----END KEY-----`, want: "-----BEGIN KEY-----\nabc\n-----END KEY-----"},
		{name: "path wins over inline", inline: "ignored", path: keyFile, want: "-----BEGIN KEY-----\nabc\n-----END KEY-----"},
		{name: "missing file", path: filepath.Join(dir, "missing.pem"), wantErr: "read " + pathKey},
		{name: "empty file", path: emptyFile, wantErr: "is empty"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(inlineKey, tt.inline)
			t.Setenv(pathKey, tt.path)

			got, err := pemFromEnvOrFile(inlineKey, pathKey)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

//nolint:paralleltest // t.Setenv
func TestEnvParsers(t *testing.T) {
	const key = "BLOG_TEST_VALUE"

	tests := []struct {
		name  string
		value string
		check func(t *testing.T)
	}{
		{name: "bool parsed", value: " false ", check: func(t *testing.T) {
			t.Helper()
			assert.False(t, boolEnv(key, true))
		}},
		{name: "bool invalid", value: "maybe", check: func(t *testing.T) {
			t.Helper()
			assert.True(t, boolEnv(key, true))
		}},
		{name: "integer invalid", value: "ten", check: func(t *testing.T) {
			t.Helper()
			assert.Equal(t, 7, integer(key, 7))
		}},
		{name: "float invalid", value: "fast", check: func(t *testing.T) {
			t.Helper()
			assert.InDelta(t, 0.5, float(key, 0.5), 0)
		}},
		{name: "float parsed", value: "0.25", check: func(t *testing.T) {
			t.Helper()
			assert.InDelta(t, 0.25, float(key, 0.5), 0)
		}},
		{name: "duration invalid", value: "soon", check: func(t *testing.T) {
			t.Helper()
			assert.Equal(t, time.Minute, duration(key, time.Minute))
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(key, tt.value)
			tt.check(t)
		})
	}
}

//nolint:paralleltest // t.Setenv
func TestDurationOrSeconds(t *testing.T) {
	const durationKey, secondsKey = "BLOG_TEST_DURATION", "BLOG_TEST_SECONDS"

	tests := []struct {
		name     string
		duration string
		seconds  string
		want     time.Duration
	}{
		{name: "fallback", want: time.Hour},
		{name: "duration wins", duration: "90s", seconds: "5", want: 90 * time.Second},
		{name: "invalid duration uses seconds", duration: "soon", seconds: "5", want: 5 * time.Second},
		{name: "seconds", seconds: "120", want: 2 * time.Minute},
		{name: "invalid seconds", seconds: "two", want: time.Hour},
		{name: "non-positive seconds", seconds: "0", want: time.Hour},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(durationKey, tt.duration)
			t.Setenv(secondsKey, tt.seconds)

			assert.Equal(t, tt.want, durationOrSeconds(durationKey, secondsKey, time.Hour))
		})
	}
}
