package sentry

import (
	"errors"
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

// dryRunDB builds statements for PostgreSQL without a connection.
func dryRunDB(tb testing.TB, plugins ...gorm.Plugin) *gorm.DB {
	tb.Helper()

	db, err := gorm.Open(postgres.New(postgres.Config{DSN: "host=127.0.0.1 port=1 dbname=none"}), &gorm.Config{
		DryRun:               true,
		DisableAutomaticPing: true,
	})
	require.NoError(tb, err)

	sqlDB, err := db.DB()
	require.NoError(tb, err)
	tb.Cleanup(func() { _ = sqlDB.Close() })

	for _, p := range plugins {
		require.NoError(tb, db.Use(p))
	}

	return db
}

//nolint:paralleltest // binds the global Sentry hub
func TestGORMTracingRecordsStatementsAsChildSpans(t *testing.T) {
	transport, flush := enableWith(t, config.Config{SentryLogsLevel: "off", SentryTracesSampleRate: 1})
	db := dryRunDB(t, GORMTracing{})

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
	db := dryRunDB(t, GORMTracing{})

	var rows []post
	require.NoError(t, db.WithContext(t.Context()).Find(&rows).Error)
	flush()

	require.Empty(t, transport.OfType("transaction"), "a statement never starts its own transaction")
}

func TestSampledParentWithoutContext(t *testing.T) {
	t.Parallel()

	require.Nil(t, sampledParent(&gorm.DB{Statement: &gorm.Statement{}}))
}

//nolint:paralleltest // binds the global Sentry hub
func TestFinishGORMSpanStatus(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want sentrygo.SpanStatus
	}{
		{name: "success", want: sentrygo.SpanStatusOK},
		{name: "record not found", err: gorm.ErrRecordNotFound, want: sentrygo.SpanStatusOK},
		{name: "failure", err: errors.New("deadlock"), want: sentrygo.SpanStatusInternalError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport, flush := enableWith(t, config.Config{SentryLogsLevel: "off", SentryTracesSampleRate: 1})
			tx := sentrygo.StartTransaction(t.Context(), "job")
			db := dryRunDB(t).WithContext(tx.Context()).Model(&post{})

			startGORMSpan(db)

			if tt.err != nil {
				_ = db.AddError(tt.err)
			}

			finishGORMSpan(db)
			tx.Finish()
			flush()

			txs := transport.OfType("transaction")
			require.Len(t, txs, 1)
			require.Len(t, txs[0].Spans, 1)
			require.Equal(t, tt.want, txs[0].Spans[0].Status)
		})
	}
}

//nolint:paralleltest // binds the global Sentry hub
func TestFinishGORMSpanIgnoresMissingOrForeignSpan(t *testing.T) {
	tests := []struct {
		name  string
		setup func(db *gorm.DB)
	}{
		{name: "no span started", setup: func(*gorm.DB) {}},
		{name: "foreign value", setup: func(db *gorm.DB) { db.InstanceSet(gormSpanKey, "not a span") }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport, flush := enableWith(t, config.Config{SentryLogsLevel: "off", SentryTracesSampleRate: 1})
			tx := sentrygo.StartTransaction(t.Context(), "job")
			db := dryRunDB(t).WithContext(tx.Context()).Model(&post{})

			tt.setup(db)
			finishGORMSpan(db)
			tx.Finish()
			flush()

			txs := transport.OfType("transaction")
			require.Len(t, txs, 1)
			require.Empty(t, txs[0].Spans)
		})
	}
}
