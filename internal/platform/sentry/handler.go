package sentry

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	sentrygo "github.com/getsentry/sentry-go"
	"github.com/turahe/blog-api/internal/platform/logging"
)

// Handler is a slog.Handler that reports Error-level records as Sentry events.
// It uses the hub on the record's context (set per request/message by the
// tracing middleware) so events carry request data and link to the transaction.
// Attributes pass through logging.Redact; records logged with a context marked
// by logging.MarkReported are skipped.
type Handler struct {
	attrs  []slog.Attr
	groups []string
}

var _ slog.Handler = (*Handler)(nil)

// NewHandler returns a Handler; pass it to logging.New as an extra handler.
func NewHandler() *Handler {
	return &Handler{}
}

// Enabled accepts Error level and above.
func (h *Handler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= slog.LevelError
}

// Handle captures record as a Sentry event; error/err attributes become the exception.
func (h *Handler) Handle(ctx context.Context, record slog.Record) error {
	if logging.Reported(ctx) {
		return nil
	}

	hub := sentrygo.GetHubFromContext(ctx)
	if hub == nil {
		hub = sentrygo.CurrentHub()
	}

	client := hub.Client()
	if client == nil {
		return nil
	}

	extra := make(sentrygo.Context)

	var errs []error

	collect := func(prefix string, a slog.Attr) {
		flatten(prefix, a, extra, &errs)
	}

	for _, a := range h.attrs {
		collect("", a)
	}

	prefix := groupPrefix(h.groups)

	record.Attrs(func(a slog.Attr) bool {
		collect(prefix, a)
		return true
	})

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

	return nil
}

// WithAttrs returns a Handler that adds attrs to every event.
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	prefix := groupPrefix(h.groups)

	next := &Handler{
		attrs:  append([]slog.Attr{}, h.attrs...),
		groups: h.groups,
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
	}
}

// flatten writes a (possibly grouped) attribute into extra as dotted keys,
// redacting sensitive keys and collecting error values under error/err.
func flatten(prefix string, a slog.Attr, extra map[string]any, errs *[]error) {
	a = logging.Redact(nil, a)
	a.Value = a.Value.Resolve()

	if a.Value.Kind() == slog.KindGroup {
		groupPrefix := prefix
		if a.Key != "" {
			groupPrefix += a.Key + "."
		}

		for _, sub := range a.Value.Group() {
			flatten(groupPrefix, sub, extra, errs)
		}

		return
	}

	if a.Key == "" {
		return
	}

	if err, ok := a.Value.Any().(error); ok && (a.Key == "error" || a.Key == "err") {
		*errs = append(*errs, err)
	}

	extra[prefix+a.Key] = a.Value.String()
}

func groupPrefix(groups []string) string {
	if len(groups) == 0 {
		return ""
	}

	return strings.Join(groups, ".") + "."
}
