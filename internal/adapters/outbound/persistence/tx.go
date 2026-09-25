package persistence

import (
	"context"

	"gorm.io/gorm"
)

type txKey struct{}

// Transactor implements event.Transactor: repositories called with the ctx it hands to
// fn run on the same transaction.
type Transactor struct {
	db *gorm.DB
}

// NewTransactor returns a Transactor over db.
func NewTransactor(db *gorm.DB) *Transactor {
	return &Transactor{db: db}
}

// InTx runs fn in a transaction. Inside the caller's transaction it uses a savepoint, so
// an error in fn (such as a unique violation the caller retries) rolls back fn's writes
// only and leaves the outer transaction usable.
func (t *Transactor) InTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return conn(ctx, t.db).Transaction(func(tx *gorm.DB) error {
		return fn(context.WithValue(ctx, txKey{}, tx))
	})
}

// conn returns the transaction carried by ctx, or db bound to ctx.
func conn(ctx context.Context, db *gorm.DB) *gorm.DB {
	if tx, ok := ctx.Value(txKey{}).(*gorm.DB); ok {
		return tx.WithContext(ctx)
	}

	return db.WithContext(ctx)
}
