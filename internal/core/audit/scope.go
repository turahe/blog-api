// Package audit lets core services annotate the audit entry of the current request.
//
// The HTTP layer opens a Scope per audited request and writes one entry after the
// handler returns. Services add what only they know: the actor of an anonymous
// request (login, password reset), the resource, and before/after changes. Every
// helper is a no-op when the context carries no scope.
package audit

import (
	"context"
	"maps"
	"sync"

	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/core/audit/domain"
)

type scopeKey struct{}

// Scope collects annotations for one entry.
type Scope struct {
	mu           sync.Mutex
	actor        *uuid.UUID
	resourceType string
	resourceID   *uuid.UUID
	changes      map[string]domain.Change
	metadata     map[string]any
}

// WithScope returns ctx carrying a new Scope.
func WithScope(ctx context.Context) (context.Context, *Scope) {
	scope := &Scope{}
	return context.WithValue(ctx, scopeKey{}, scope), scope
}

func from(ctx context.Context) *Scope {
	scope, _ := ctx.Value(scopeKey{}).(*Scope)
	return scope
}

// SetActor names the user acting in an otherwise anonymous request.
func SetActor(ctx context.Context, userID uuid.UUID) {
	if s := from(ctx); s != nil {
		s.mu.Lock()
		defer s.mu.Unlock()

		s.actor = &userID
	}
}

// SetResource names the entity the request changed.
func SetResource(ctx context.Context, resourceType string, id uuid.UUID) {
	if s := from(ctx); s != nil {
		s.mu.Lock()
		defer s.mu.Unlock()

		s.resourceType, s.resourceID = resourceType, &id
	}
}

// AddChange records a field's value before and after the mutation. Never pass
// secrets or personal data that should not be kept for the retention period.
func AddChange(ctx context.Context, field string, before, after any) {
	if s := from(ctx); s != nil {
		s.mu.Lock()
		defer s.mu.Unlock()

		if s.changes == nil {
			s.changes = map[string]domain.Change{}
		}

		s.changes[field] = domain.Change{From: before, To: after}
	}
}

// AddMetadata attaches a non-sensitive detail to the entry.
func AddMetadata(ctx context.Context, key string, value any) {
	if s := from(ctx); s != nil {
		s.mu.Lock()
		defer s.mu.Unlock()

		if s.metadata == nil {
			s.metadata = map[string]any{}
		}

		s.metadata[key] = value
	}
}

// Apply copies the annotations onto entry, overriding what the HTTP layer inferred.
func (s *Scope) Apply(entry *domain.Entry) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.actor != nil {
		entry.ActorID = s.actor
	}

	if s.resourceID != nil {
		entry.ResourceType, entry.ResourceID = s.resourceType, s.resourceID
	}

	if len(s.changes) > 0 {
		entry.Changes = s.changes
	}

	if len(s.metadata) > 0 {
		if entry.Metadata == nil {
			entry.Metadata = map[string]any{}
		}

		maps.Copy(entry.Metadata, s.metadata)
	}
}

// Actor returns the actor set through SetActor, if any.
func (s *Scope) Actor() *uuid.UUID {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.actor
}
