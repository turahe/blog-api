// Package eventtest provides an in-memory event recorder for service tests.
package eventtest

import (
	"context"
	"sync"

	"github.com/turahe/blog-api/internal/core/event"
)

// Recorder keeps recorded events; Err, when set, is returned instead.
type Recorder struct {
	mu     sync.Mutex
	events []event.Event
	Err    error
}

// Record implements event.Recorder.
func (r *Recorder) Record(_ context.Context, events ...event.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.Err != nil {
		return r.Err
	}

	r.events = append(r.events, events...)

	return nil
}

// Events returns the recorded events in order.
func (r *Recorder) Events() []event.Event {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]event.Event(nil), r.events...)
}

// Types returns the recorded event types in order.
func (r *Recorder) Types() []string {
	events := r.Events()

	types := make([]string, len(events))
	for i, e := range events {
		types[i] = e.Type
	}

	return types
}

// Unit returns an event.Unit that records into r without a transaction.
func (r *Recorder) Unit() event.Unit {
	return event.Unit{Tx: event.NoTx{}, Recorder: r}
}
