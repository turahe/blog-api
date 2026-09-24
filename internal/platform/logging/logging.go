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

	return slog.New(requestIDHandler{inner: handler})
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

const requestIDAttr = "request_id"

type requestIDKey struct{}

// WithRequestID returns ctx carrying the request correlation id. Records logged
// through a *Context method with that ctx gain a request_id attribute.
func WithRequestID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}

	return context.WithValue(ctx, requestIDKey{}, id)
}

// RequestID returns the correlation id stored by WithRequestID, or "".
func RequestID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}

	id, _ := ctx.Value(requestIDKey{}).(string)

	return id
}

// requestIDHandler adds request_id from the record's context unless the call
// site already passed one.
type requestIDHandler struct {
	inner slog.Handler
}

func (h requestIDHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h requestIDHandler) Handle(ctx context.Context, record slog.Record) error {
	if id := RequestID(ctx); id != "" && !hasAttr(record, requestIDAttr) {
		record = record.Clone()
		record.AddAttrs(slog.String(requestIDAttr, id))
	}

	return h.inner.Handle(ctx, record)
}

func (h requestIDHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return requestIDHandler{inner: h.inner.WithAttrs(attrs)}
}

func (h requestIDHandler) WithGroup(name string) slog.Handler {
	return requestIDHandler{inner: h.inner.WithGroup(name)}
}

func hasAttr(record slog.Record, key string) bool {
	found := false

	record.Attrs(func(a slog.Attr) bool {
		found = a.Key == key
		return !found
	})

	return found
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
