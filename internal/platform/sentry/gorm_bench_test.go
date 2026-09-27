package sentry

import (
	"context"
	"fmt"
	"testing"

	sentrygo "github.com/getsentry/sentry-go"
	"github.com/turahe/blog-api/internal/platform/config"
	"gorm.io/gorm"
)

// BenchmarkGORMTracing measures one dry-run SELECT (statement building, no
// round trip) per iteration, starting a transaction per iteration when span is
// set so plugin=off and plugin=on differ only by the plugin.
func BenchmarkGORMTracing(b *testing.B) {
	spans := map[string][]sentrygo.SpanOption{
		"none":      nil,
		"unsampled": {sentrygo.WithSpanSampled(sentrygo.SampledFalse)},
		"sampled":   {sentrygo.WithSpanSampled(sentrygo.SampledTrue)},
	}

	for _, span := range []string{"none", "unsampled", "sampled"} {
		for _, plugin := range []string{"off", "on"} {
			b.Run(fmt.Sprintf("span=%s/plugin=%s", span, plugin), func(b *testing.B) {
				benchSentry(b, config.Config{SentryLogsLevel: "off", SentryTracesSampleRate: 1})

				var plugins []gorm.Plugin
				if plugin == "on" {
					plugins = append(plugins, GORMTracing{})
				}

				db := dryRunDB(b, plugins...)

				b.ReportAllocs()

				for b.Loop() {
					ctx := context.Background()

					var tx *sentrygo.Span
					if span != "none" {
						tx = sentrygo.StartTransaction(ctx, "GET /api/v1/posts/:slug", spans[span]...)
						ctx = tx.Context()
					}

					var rows []post

					_ = db.WithContext(ctx).Where("slug = ?", "hello").Find(&rows).Error

					if tx != nil {
						tx.Finish()
					}
				}
			})
		}
	}
}
