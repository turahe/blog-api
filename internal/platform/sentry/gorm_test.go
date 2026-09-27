package sentry

import (
	"testing"

	sentrygo "github.com/getsentry/sentry-go"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/platform/config"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type post struct {
	ID   int
	Slug string
}

func dryRunDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(postgres.New(postgres.Config{DSN: "host=127.0.0.1 port=1 dbname=none"}), &gorm.Config{
		DryRun:               true,
		DisableAutomaticPing: true,
	})
	require.NoError(t, err)
	require.NoError(t, db.Use(GORMTracing{}))

	return db
}

//nolint:paralleltest // binds the global Sentry hub
func TestGORMTracingRecordsStatementsAsChildSpans(t *testing.T) {
	transport, flush := enableWith(t, config.Config{SentryLogsLevel: "off", SentryTracesSampleRate: 1})
	db := dryRunDB(t)

	tx := sentrygo.StartTransaction(t.Context(), "GET /posts/:slug")

	var rows []post
	require.NoError(t, db.WithContext(tx.Context()).Where("slug = ?", "secret-slug").Find(&rows).Error)
	tx.Finish()
	flush()

	txs := transport.OfType("transaction")
	require.Len(t, txs, 1)
	require.Len(t, txs[0].Spans, 1)

	span := txs[0].Spans[0]
	require.Equal(t, "db.sql.query", span.Op)
	require.Equal(t, `SELECT * FROM "posts" WHERE slug = $1`, span.Description)
	require.Equal(t, "postgresql", span.Data["db.system"])
	require.Equal(t, "posts", span.Data["db.collection.name"])
	require.Equal(t, sentrygo.SpanStatusOK, span.Status)
	require.Equal(t, gormSpanOrigin, span.Origin)
}

//nolint:paralleltest // binds the global Sentry hub
func TestGORMTracingIgnoresStatementsOutsideASpan(t *testing.T) {
	transport, flush := enableWith(t, config.Config{SentryLogsLevel: "off", SentryTracesSampleRate: 1})
	db := dryRunDB(t)

	var rows []post
	require.NoError(t, db.WithContext(t.Context()).Find(&rows).Error)
	flush()

	require.Empty(t, transport.OfType("transaction"), "a statement never starts its own transaction")
}
