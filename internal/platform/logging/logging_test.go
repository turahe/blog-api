package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewLevelByEnvironment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		environment string
		wantDebug   bool
	}{
		{name: "local", environment: "local", wantDebug: true},
		{name: "production", environment: "production", wantDebug: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			logger := New(tt.environment)
			require.NotNil(t, logger)
			require.Equal(t, tt.wantDebug, logger.Enabled(t.Context(), slog.LevelDebug))
			require.True(t, logger.Enabled(t.Context(), slog.LevelInfo))
		})
	}
}

func TestNewToFansOutToExtraHandlers(t *testing.T) {
	t.Parallel()

	var primary, extra bytes.Buffer

	NewTo(&primary, "production", slog.NewJSONHandler(&extra, nil)).Info("fan out", "k", "v")

	for name, buf := range map[string]*bytes.Buffer{"primary": &primary, "extra": &extra} {
		var entry map[string]any
		require.NoError(t, json.Unmarshal(buf.Bytes(), &entry), name)
		require.Equal(t, "fan out", entry["msg"], name)
		require.Equal(t, "v", entry["k"], name)
	}
}

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

func TestLoggerAddsRequestIDFromContext(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	ctx := WithRequestID(t.Context(), "req-ctx")
	NewTo(&buf, "production").With("component", "cache").InfoContext(ctx, "lookup")

	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))
	require.Equal(t, "req-ctx", entry["request_id"])
	require.Equal(t, "cache", entry["component"])
}

func TestLoggerKeepsExplicitRequestID(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	ctx := WithRequestID(t.Context(), "req-ctx")
	NewTo(&buf, "production").InfoContext(ctx, "http request", "request_id", "req-explicit")

	require.Equal(t, 1, bytes.Count(buf.Bytes(), []byte(`"request_id"`)))
	require.Contains(t, buf.String(), "req-explicit")
}

func TestLoggerWithoutRequestID(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	NewTo(&buf, "production").InfoContext(t.Context(), "startup")

	require.NotContains(t, buf.String(), "request_id")
	require.Empty(t, RequestID(t.Context()))
	require.Equal(t, t.Context(), WithRequestID(t.Context(), ""))
}

func TestRequestIDWithNilContext(t *testing.T) {
	t.Parallel()

	var ctx context.Context

	require.Empty(t, RequestID(ctx))
}

func TestLoggerWithGroupKeepsRequestID(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	ctx := WithRequestID(t.Context(), "req-ctx")
	NewTo(&buf, "production").WithGroup("job").InfoContext(ctx, "ran", "name", "prune")

	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))
	require.Equal(t, map[string]any{"name": "prune", "request_id": "req-ctx"}, entry["job"])
}

func TestMarkReported(t *testing.T) {
	t.Parallel()

	require.False(t, Reported(t.Context()))
	require.True(t, Reported(MarkReported(t.Context())))
}

func TestLoggerLevelByEnvironment(t *testing.T) {
	t.Parallel()

	var local, prod bytes.Buffer

	NewTo(&local, "local").Debug("visible")
	NewTo(&prod, "production").Debug("hidden")

	require.Contains(t, local.String(), "visible")
	require.Empty(t, prod.String())
}
