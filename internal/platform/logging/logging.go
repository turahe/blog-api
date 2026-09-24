// Package logging builds the application's structured JSON logger.
package logging

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
)

const redacted = "[REDACTED]"

// New returns a JSON logger on stdout; debug level when environment is "local".
// Records are also fanned out to each extra handler (e.g. an error tracker).
func New(environment string, extra ...slog.Handler) *slog.Logger {
	return NewTo(os.Stdout, environment, extra...)
}

// NewTo is New with an explicit destination writer.
func NewTo(w io.Writer, environment string, extra ...slog.Handler) *slog.Logger {
	level := slog.LevelInfo

	if environment == "local" {
		level = slog.LevelDebug
	}

	var handler slog.Handler = slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level:       level,
		ReplaceAttr: Redact,
	})

	if len(extra) > 0 {
		handler = slog.NewMultiHandler(append([]slog.Handler{handler}, extra...)...)
	}

	return slog.New(handler)
}

// Redact is a slog ReplaceAttr func that masks credential-bearing attributes
// (at any group depth) so a careless log call cannot ship secrets to a sink.
func Redact(_ []string, a slog.Attr) slog.Attr {
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

type reportedKey struct{}

// MarkReported flags ctx so error-tracking handlers skip further records logged
// with it; use after a failure has already been reported once (e.g. a panic).
func MarkReported(ctx context.Context) context.Context {
	return context.WithValue(ctx, reportedKey{}, true)
}

// Reported reports whether ctx was flagged by MarkReported.
func Reported(ctx context.Context) bool {
	reported, _ := ctx.Value(reportedKey{}).(bool)
	return reported
}
