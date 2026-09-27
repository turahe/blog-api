package sentry

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	sentrygo "github.com/getsentry/sentry-go"
	"github.com/turahe/blog-api/internal/platform/logging"
)

// Handler is a slog.Handler that reports Error-level records as Sentry events
// and, when built WithLogs, sends records to Sentry Logs.
// It uses the hub on the record's context (set per request/message by the
// tracing middleware) so events carry request data and link to the transaction.
// Attributes pass through logging.Redact; records logged with a context marked
// by logging.MarkReported are not reported as events again.
type Handler struct {
	attrs  []slog.Attr
	groups []string
	logs   *logSink
}

// logSink sends records at level and above to Sentry Logs.
type logSink struct {
	level  slog.Level
	logger sentrygo.Logger
}

var _ slog.Handler = (*Handler)(nil)

// NewHandler returns a Handler; pass it to logging.New as an extra handler.
func NewHandler() *Handler {
	return &Handler{}
}

// WithLogs returns a copy of h that also sends records at level and above to
// Sentry Logs. Call it after Init; before that the logs are dropped.
func (h *Handler) WithLogs(level slog.Level) *Handler {
	next := *h
	next.logs = &logSink{level: level, logger: sentrygo.NewLogger(context.Background())}

	return &next
}

// Enabled accepts Error level and above, plus the Sentry Logs level when set.
func (h *Handler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= slog.LevelError || (h.logs != nil && level >= h.logs.level)
}

// Handle sends record to Sentry Logs and captures Error records as Sentry
// events; error/err attributes become the event's exception.
func (h *Handler) Handle(ctx context.Context, record slog.Record) error {
	attrs := h.flatAttrs(record)

	if h.logs != nil && record.Level >= h.logs.level {
		h.logs.emit(ctx, record, attrs)
	}

	if record.Level < slog.LevelError || logging.Reported(ctx) {
		return nil
	}

	capture(ctx, record, attrs)

	return nil
}

// WithAttrs returns a Handler that adds attrs to every event.
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	prefix := groupPrefix(h.groups)

	next := &Handler{
		attrs:  append([]slog.Attr{}, h.attrs...),
		groups: h.groups,
		logs:   h.logs,
	}
	for _, a := range attrs {
		if prefix != "" {
			a.Key = prefix + a.Key
		}

		next.attrs = append(next.attrs, a)
	}

	return next
}

// WithGroup returns a Handler that prefixes later attribute keys with name.
func (h *Handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}

	return &Handler{
		attrs:  h.attrs,
		groups: append(append([]string{}, h.groups...), name),
		logs:   h.logs,
	}
}

func (h *Handler) flatAttrs(record slog.Record) []slog.Attr {
	out := make([]slog.Attr, 0, len(h.attrs)+record.NumAttrs())

	for _, a := range h.attrs {
		out = flatten("", a, out)
	}

	prefix := groupPrefix(h.groups)

	record.Attrs(func(a slog.Attr) bool {
		out = flatten(prefix, a, out)
		return true
	})

	return out
}

func capture(ctx context.Context, record slog.Record, attrs []slog.Attr) {
	hub := sentrygo.GetHubFromContext(ctx)
	if hub == nil {
		hub = sentrygo.CurrentHub()
	}

	client := hub.Client()
	if client == nil {
		return
	}

	extra := make(sentrygo.Context, len(attrs))

	var errs []error

	for _, a := range attrs {
		if err, ok := a.Value.Any().(error); ok && isErrorKey(a.Key) {
			errs = append(errs, err)
		}

		extra[a.Key] = a.Value.String()
	}

	event := sentrygo.NewEvent()
	if err := errors.Join(errs...); err != nil {
		event = client.EventFromException(err, sentrygo.LevelError)
	}

	event.Level = sentrygo.LevelError
	event.Message = record.Message
	event.Logger = "slog"
	event.Timestamp = record.Time

	if event.Contexts == nil {
		event.Contexts = make(map[string]sentrygo.Context)
	}

	event.Contexts["log"] = extra

	hub.CaptureEvent(event)
}

func (s *logSink) emit(ctx context.Context, record slog.Record, attrs []slog.Attr) {
	entry := s.entry(record.Level).WithCtx(ctx)

	for _, a := range attrs {
		switch a.Value.Kind() {
		case slog.KindInt64:
			entry = entry.Int64(a.Key, a.Value.Int64())
		case slog.KindFloat64:
			entry = entry.Float64(a.Key, a.Value.Float64())
		case slog.KindBool:
			entry = entry.Bool(a.Key, a.Value.Bool())
		case slog.KindString, slog.KindUint64, slog.KindDuration, slog.KindTime,
			slog.KindAny, slog.KindGroup, slog.KindLogValuer:
			entry = entry.String(a.Key, a.Value.String())
		}
	}

	entry.Emit(record.Message)
}

// entry maps a slog level to a Sentry log entry. Fatal and Panic are never
// used: they exit or panic after emitting.
func (s *logSink) entry(level slog.Level) sentrygo.LogEntry {
	switch {
	case level >= slog.LevelError:
		return s.logger.Error()
	case level >= slog.LevelWarn:
		return s.logger.Warn()
	case level >= slog.LevelInfo:
		return s.logger.Info()
	default:
		return s.logger.Debug()
	}
}

// flatten appends a (possibly grouped) attribute to out with dotted keys,
// redacting sensitive keys.
func flatten(prefix string, a slog.Attr, out []slog.Attr) []slog.Attr {
	a = logging.Redact(nil, a)
	a.Value = a.Value.Resolve()

	if a.Value.Kind() == slog.KindGroup {
		groupPrefix := prefix
		if a.Key != "" {
			groupPrefix += a.Key + "."
		}

		for _, sub := range a.Value.Group() {
			out = flatten(groupPrefix, sub, out)
		}

		return out
	}

	if a.Key == "" {
		return out
	}

	return append(out, slog.Attr{Key: prefix + a.Key, Value: a.Value})
}

func isErrorKey(key string) bool {
	if i := strings.LastIndexByte(key, '.'); i >= 0 {
		key = key[i+1:]
	}

	return key == "error" || key == "err"
}

func groupPrefix(groups []string) string {
	if len(groups) == 0 {
		return ""
	}

	return strings.Join(groups, ".") + "."
}
