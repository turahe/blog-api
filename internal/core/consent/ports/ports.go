// Package ports defines the consent module's outbound interfaces.
package ports

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/core/consent/domain"
)

// Repository stores consent subjects and their per-purpose consents.
type Repository interface {
	// CreateSubject stores a new subject under tokenHash.
	CreateSubject(ctx context.Context, subject domain.Subject, tokenHash string) error
	// SubjectByToken returns the subject for tokenHash, or domain.ErrNotFound.
	SubjectByToken(ctx context.Context, tokenHash string) (domain.Subject, error)
	// SetUser links the subject to a user, or unlinks it when userID is nil.
	SetUser(ctx context.Context, subjectID uuid.UUID, userID *uuid.UUID) error
	// Touch records that the subject was seen at.
	Touch(ctx context.Context, subjectID uuid.UUID, at time.Time) error
	// List returns the subject's consents.
	List(ctx context.Context, subjectID uuid.UUID) ([]domain.Consent, error)
	// Get returns a consent and its subject, or domain.ErrNotFound.
	Get(ctx context.Context, consentID uuid.UUID) (domain.Consent, domain.Subject, error)
	// Save inserts or replaces the subject's consent for its purpose.
	Save(ctx context.Context, consent domain.Consent) error
	// DeleteForUser deletes the subjects linked to the user and their consents.
	DeleteForUser(ctx context.Context, userID uuid.UUID) (int64, error)
	// DeleteSubjectEvents deletes the raw analytics events and first-seen record of the subject.
	DeleteSubjectEvents(ctx context.Context, subjectID uuid.UUID) error
}
