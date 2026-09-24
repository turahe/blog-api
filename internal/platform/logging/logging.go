// Package logging builds the application's structured JSON logger.
package logging

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

const redacted = "[REDACTED]"

// New returns a JSON logger on stdout; debug level when environment is "local".
func New(environment string) *slog.Logger {
	return NewTo(os.Stdout, environment)
}

// NewTo is New with an explicit destination writer.
func NewTo(w io.Writer, environment string) *slog.Logger {
	level := slog.LevelInfo

	if environment == "local" {
		level = slog.LevelDebug
	}

	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level:       level,
		ReplaceAttr: redactSensitive,
	}))
}

// redactSensitive masks credential-bearing attributes (at any group depth) so a
// careless log call cannot ship secrets to the log pipeline.
func redactSensitive(_ []string, a slog.Attr) slog.Attr {
	if isSensitiveKey(a.Key) {
		return slog.String(a.Key, redacted)
	}

	return a
}

func isSensitiveKey(key string) bool {
	key = strings.ToLower(key)

	switch key {
	case "authorization", "cookie", "set-cookie", "dsn", "api_key", "apikey":
		return true
	}

	return strings.Contains(key, "password") ||
		strings.Contains(key, "secret") ||
		strings.HasSuffix(key, "token")
}
