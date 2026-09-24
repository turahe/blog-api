// Package sentry configures the Sentry SDK and reports error logs as Sentry events.
package sentry

import (
	"fmt"
	"strings"
	"time"

	sentrygo "github.com/getsentry/sentry-go"
	"github.com/turahe/blog-api/internal/platform/config"
)

const flushTimeout = 2 * time.Second

// Init configures the global Sentry client. It is a no-op when SENTRY_DSN is
// unset. The returned flush func is always non-nil and delivers buffered events.
func Init(cfg config.Config, release string) (func(), error) {
	return initWithTransport(cfg, release, nil)
}

func initWithTransport(cfg config.Config, release string, transport sentrygo.Transport) (func(), error) {
	if !cfg.SentryEnabled() {
		return func() {}, nil
	}

	err := sentrygo.Init(sentrygo.ClientOptions{
		Dsn:              cfg.SentryDSN,
		Environment:      cfg.SentryEnvironment,
		Release:          release,
		EnableTracing:    true,
		TracesSampleRate: cfg.SentryTracesSampleRate,
		DataCollection: &sentrygo.DataCollection{
			UserInfo:    sentrygo.Set(false),
			Cookies:     &sentrygo.KeyValueCollectionBehavior{Mode: sentrygo.CollectionOff},
			QueryParams: &sentrygo.KeyValueCollectionBehavior{Mode: sentrygo.CollectionOff},
			HTTPBodies:  []sentrygo.BodyType{},
		},
		BeforeSend:            scrub,
		BeforeSendTransaction: scrub,
		Transport:             transport,
	})
	if err != nil {
		return nil, fmt.Errorf("init sentry: %w", err)
	}

	return func() { sentrygo.Flush(flushTimeout) }, nil
}

// scrub drops request data that can carry credentials or reset tokens.
func scrub(event *sentrygo.Event, _ *sentrygo.EventHint) *sentrygo.Event {
	req := event.Request
	if req == nil {
		return event
	}

	req.QueryString = ""
	req.Cookies = ""
	req.Data = ""

	for name := range req.Headers {
		if isSensitiveHeader(name) {
			delete(req.Headers, name)
		}
	}

	return event
}

func isSensitiveHeader(name string) bool {
	name = strings.ToLower(name)

	switch name {
	case "authorization", "proxy-authorization", "cookie", "set-cookie", "x-api-key":
		return true
	}

	return strings.Contains(name, "token") ||
		strings.Contains(name, "secret") ||
		strings.Contains(name, "password")
}
