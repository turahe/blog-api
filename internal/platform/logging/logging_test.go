package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoggerRedactsSensitiveAttributes(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	NewTo(&buf, "production").
		With("Authorization", "Bearer abc").
		Info("login",
			"password", "hunter2",
			"request_id", "req-1",
			"token_type", "Bearer",
			slog.Group("session", "refresh_token", "raw-refresh", "user_agent", "curl"),
		)

	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))
	require.Equal(t, redacted, entry["Authorization"])
	require.Equal(t, redacted, entry["password"])
	require.Equal(t, "req-1", entry["request_id"])
	require.Equal(t, "Bearer", entry["token_type"])
	require.Equal(t, map[string]any{"refresh_token": redacted, "user_agent": "curl"}, entry["session"])
	require.NotContains(t, buf.String(), "hunter2")
	require.NotContains(t, buf.String(), "raw-refresh")
}

func TestLoggerLevelByEnvironment(t *testing.T) {
	t.Parallel()

	var local, prod bytes.Buffer

	NewTo(&local, "local").Debug("visible")
	NewTo(&prod, "production").Debug("hidden")

	require.Contains(t, local.String(), "visible")
	require.Empty(t, prod.String())
}
