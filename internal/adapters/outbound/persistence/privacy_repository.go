package persistence

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	privacydomain "github.com/turahe/blog-api/internal/core/privacy/domain"
	"gorm.io/gorm"
)

// PrivacyRepository stores personal data export and erasure requests in PostgreSQL.
type PrivacyRepository struct {
	db *gorm.DB
}

// NewPrivacyRepository returns a PrivacyRepository over db.
func NewPrivacyRepository(db *gorm.DB) *PrivacyRepository {
	return &PrivacyRepository{db: db}
}

type privacyRequestRow struct {
	UUID        uuid.UUID
	UserUUID    uuid.UUID
	Kind        string
	Status      string
	StorageKey  *string
	ExpiresAt   *time.Time
	Attempts    int
	LastError   *string
	CreatedAt   time.Time
	StartedAt   *time.Time
	CompletedAt *time.Time
}

const privacyRequestColumns = `r.uuid, (SELECT u.uuid FROM users u WHERE u.id = r.user_id) AS user_uuid,
	r.kind, r.status, r.storage_key, r.expires_at, r.attempts, r.last_error, r.created_at, r.started_at, r.completed_at`

// Create inserts a pending request; a second open request of the same kind is
// privacydomain.ErrAlreadyOpen. The insert runs in a savepoint so that conflict leaves an
// enclosing transaction usable.
func (r *PrivacyRepository) Create(ctx context.Context, request privacydomain.Request) error {
	err := conn(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		return tx.Exec(`
		INSERT INTO privacy_requests (uuid, user_id, kind, status, created_at)
		VALUES (?, `+idOf("users")+`, ?, ?, ?)`,
			request.UUID, request.UserUUID, string(request.Kind), string(request.Status), request.CreatedAt).Error
	})
	if isUniqueViolation(err) {
		return privacydomain.ErrAlreadyOpen
	}

	return err
}

// Latest returns the user's most recent request of kind.
func (r *PrivacyRepository) Latest(ctx context.Context, userID uuid.UUID, kind privacydomain.Kind) (privacydomain.Request, error) {
	requests, err := r.list(ctx, `SELECT `+privacyRequestColumns+` FROM privacy_requests r
		WHERE r.user_id = `+idOf("users")+` AND r.kind = ? ORDER BY r.created_at DESC, r.id DESC LIMIT 1`,
		userID, string(kind))
	if err != nil {
		return privacydomain.Request{}, err
	}

	if len(requests) == 0 {
		return privacydomain.Request{}, privacydomain.ErrNotFound
	}

	return requests[0], nil
}

// ClaimNext marks the oldest claimable request running. SKIP LOCKED lets several
// schedulers claim concurrently without taking the same request.
func (r *PrivacyRepository) ClaimNext(ctx context.Context, now, staleBefore time.Time) (privacydomain.Request, bool, error) {
	requests, err := r.list(ctx, `UPDATE privacy_requests r
		SET status = 'running', started_at = ?, attempts = r.attempts + 1
		WHERE r.id = (
			SELECT q.id FROM privacy_requests q
			WHERE q.status = 'pending' OR (q.status = 'running' AND q.started_at < ?)
			ORDER BY q.created_at, q.id LIMIT 1 FOR UPDATE SKIP LOCKED)
		RETURNING `+privacyRequestColumns, now, staleBefore)
	if err != nil || len(requests) == 0 {
		return privacydomain.Request{}, false, err
	}

	return requests[0], true, nil
}

// Complete marks the request completed with an optional export archive.
func (r *PrivacyRepository) Complete(ctx context.Context, id uuid.UUID, storageKey *string, expiresAt *time.Time, at time.Time) error {
	return r.exec(ctx, `UPDATE privacy_requests SET status = 'completed', storage_key = ?, expires_at = ?,
		completed_at = ?, last_error = NULL WHERE uuid = ?`, storageKey, expiresAt, at, id)
}

// Fail records message and requeues the request, or marks it failed when final.
func (r *PrivacyRepository) Fail(ctx context.Context, id uuid.UUID, message string, final bool, at time.Time) error {
	status, completed := string(privacydomain.StatusPending), (*time.Time)(nil)
	if final {
		status, completed = string(privacydomain.StatusFailed), &at
	}

	return r.exec(ctx, `UPDATE privacy_requests SET status = ?, last_error = ?, completed_at = ? WHERE uuid = ?`,
		status, message, completed, id)
}

// Archives returns requests holding an export archive, oldest expiry first.
func (r *PrivacyRepository) Archives(ctx context.Context, userID *uuid.UUID, before time.Time, limit int) ([]privacydomain.Request, error) {
	where, args := []string{"r.storage_key IS NOT NULL"}, []any{}
	if userID != nil {
		where, args = append(where, "r.user_id = "+idOf("users")), append(args, *userID)
	}

	if !before.IsZero() {
		where, args = append(where, "r.expires_at < ?"), append(args, before)
	}

	return r.list(ctx, `SELECT `+privacyRequestColumns+` FROM privacy_requests r WHERE `+
		strings.Join(where, " AND ")+` ORDER BY r.expires_at, r.id LIMIT ?`, append(args, limit)...)
}

// ClearArchive forgets a deleted export archive.
func (r *PrivacyRepository) ClearArchive(ctx context.Context, id uuid.UUID) error {
	return r.exec(ctx, `UPDATE privacy_requests SET storage_key = NULL WHERE uuid = ?`, id)
}

func (r *PrivacyRepository) exec(ctx context.Context, query string, args ...any) error {
	result := conn(ctx, r.db).Exec(query, args...)
	if result.Error != nil {
		return result.Error
	}

	if result.RowsAffected == 0 {
		return privacydomain.ErrNotFound
	}

	return nil
}

func (r *PrivacyRepository) list(ctx context.Context, query string, args ...any) ([]privacydomain.Request, error) {
	var rows []privacyRequestRow
	if err := conn(ctx, r.db).Raw(query, args...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("privacy requests: %w", err)
	}

	out := make([]privacydomain.Request, 0, len(rows))
	for _, row := range rows {
		out = append(out, privacydomain.Request{
			UUID: row.UUID, UserUUID: row.UserUUID,
			Kind: privacydomain.Kind(row.Kind), Status: privacydomain.Status(row.Status),
			StorageKey: row.StorageKey, ExpiresAt: row.ExpiresAt, Attempts: row.Attempts, LastError: row.LastError,
			CreatedAt: row.CreatedAt, StartedAt: row.StartedAt, CompletedAt: row.CompletedAt,
		})
	}

	return out, nil
}
