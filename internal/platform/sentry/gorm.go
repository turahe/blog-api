package sentry

import (
	"errors"

	sentrygo "github.com/getsentry/sentry-go"
	"gorm.io/gorm"
)

const (
	gormSpanKey    = "sentry:span"
	gormSpanOrigin = sentrygo.SpanOrigin("auto.db.gorm")
)

// GORMTracing is a gorm.Plugin that records each statement as a child span of
// the sampled span on the statement's context. Statements without one (startup,
// migrations, unsampled requests) are left alone, so it never starts transactions.
type GORMTracing struct{}

var _ gorm.Plugin = GORMTracing{}

// Name implements gorm.Plugin.
func (GORMTracing) Name() string { return "sentry:tracing" }

// Initialize implements gorm.Plugin.
func (GORMTracing) Initialize(db *gorm.DB) error {
	cb := db.Callback()

	return errors.Join(
		cb.Create().Before("gorm:create").Register("sentry:before_create", startGORMSpan),
		cb.Create().After("gorm:create").Register("sentry:after_create", finishGORMSpan),
		cb.Query().Before("gorm:query").Register("sentry:before_query", startGORMSpan),
		cb.Query().After("gorm:query").Register("sentry:after_query", finishGORMSpan),
		cb.Update().Before("gorm:update").Register("sentry:before_update", startGORMSpan),
		cb.Update().After("gorm:update").Register("sentry:after_update", finishGORMSpan),
		cb.Delete().Before("gorm:delete").Register("sentry:before_delete", startGORMSpan),
		cb.Delete().After("gorm:delete").Register("sentry:after_delete", finishGORMSpan),
		cb.Row().Before("gorm:row").Register("sentry:before_row", startGORMSpan),
		cb.Row().After("gorm:row").Register("sentry:after_row", finishGORMSpan),
		cb.Raw().Before("gorm:raw").Register("sentry:before_raw", startGORMSpan),
		cb.Raw().After("gorm:raw").Register("sentry:after_raw", finishGORMSpan),
	)
}

// sampledParent returns the sampled span on the statement's context, or nil.
func sampledParent(db *gorm.DB) *sentrygo.Span {
	ctx := db.Statement.Context
	if ctx == nil {
		return nil
	}

	parent := sentrygo.SpanFromContext(ctx)
	if parent == nil || !parent.Sampled.Bool() {
		return nil
	}

	return parent
}

func startGORMSpan(db *gorm.DB) {
	parent := sampledParent(db)
	if parent == nil {
		return
	}

	db.InstanceSet(gormSpanKey, parent.StartChild("db.sql.query", sentrygo.WithSpanOrigin(gormSpanOrigin)))
}

func finishGORMSpan(db *gorm.DB) {
	// InstanceGet formats a key per call; skip it when start could not have set one.
	if sampledParent(db) == nil {
		return
	}

	value, ok := db.InstanceGet(gormSpanKey)
	if !ok {
		return
	}

	span, ok := value.(*sentrygo.Span)
	if !ok {
		return
	}

	// Statement.SQL holds placeholders; bound values stay in Statement.Vars.
	span.Description = db.Statement.SQL.String()
	span.SetData("db.system", "postgresql")
	span.SetData("db.rows_affected", db.RowsAffected)

	if db.Statement.Table != "" {
		span.SetData("db.collection.name", db.Statement.Table)
	}

	if db.Error != nil && !errors.Is(db.Error, gorm.ErrRecordNotFound) {
		span.Status = sentrygo.SpanStatusInternalError
	} else {
		span.Status = sentrygo.SpanStatusOK
	}

	span.Finish()
}
