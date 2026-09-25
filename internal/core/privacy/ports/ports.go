// Package ports declares what the privacy service needs from storage and other modules.
package ports

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/core/privacy/domain"
)

// Repository stores privacy requests.
type Repository interface {
	// Create inserts a pending request.
	Create(ctx context.Context, request domain.Request) error
	// Latest returns the user's most recent request of kind, or domain.ErrNotFound.
	Latest(ctx context.Context, userID uuid.UUID, kind domain.Kind) (domain.Request, error)
	// ClaimNext marks the oldest pending request running and returns it; a running request
	// started before staleBefore is claimed again. ok is false when there is none.
	ClaimNext(ctx context.Context, now, staleBefore time.Time) (request domain.Request, ok bool, err error)
	// Complete marks the request completed, recording the archive of an export.
	Complete(ctx context.Context, id uuid.UUID, storageKey *string, expiresAt *time.Time, at time.Time) error
	// Fail records the error and moves the request back to pending, or to failed when final.
	Fail(ctx context.Context, id uuid.UUID, message string, final bool, at time.Time) error
	// Archives returns requests holding an export archive, limited to one user when userID is
	// non-nil and to archives expiring before `before` when it is non-zero.
	Archives(ctx context.Context, userID *uuid.UUID, before time.Time, limit int) ([]domain.Request, error)
	// ClearArchive forgets the archive of an export after it was deleted from storage.
	ClearArchive(ctx context.Context, id uuid.UUID) error
}

// ArchiveStore keeps export archives in object storage.
type ArchiveStore interface {
	PutObject(ctx context.Context, key, contentType string, body []byte) error
	PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error)
	DeleteObject(ctx context.Context, key string) error
}

// DataSource collects everything stored about a user as one JSON document.
type DataSource interface {
	ExportUser(ctx context.Context, userID uuid.UUID, at time.Time) ([]byte, error)
}

// Eraser anonymizes a user in place: it deletes activity, consents, sessions, and tokens,
// scrubs identifying fields, and keeps authored content attributed to the anonymized account.
type Eraser interface {
	EraseUser(ctx context.Context, userID uuid.UUID, at time.Time) error
}

// PasswordVerifier checks a user's current password for step-up actions.
type PasswordVerifier interface {
	VerifyPassword(ctx context.Context, userID uuid.UUID, password string) (bool, error)
}
