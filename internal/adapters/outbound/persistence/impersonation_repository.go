package persistence

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	impdomain "github.com/turahe/blog-api/internal/core/impersonation/domain"
	"gorm.io/gorm"
)

// ImpersonationRepository stores impersonation sessions in PostgreSQL.
type ImpersonationRepository struct {
	db *gorm.DB
}

// NewImpersonationRepository returns an ImpersonationRepository over db.
func NewImpersonationRepository(db *gorm.DB) *ImpersonationRepository {
	return &ImpersonationRepository{db: db}
}

type impersonationRow struct {
	UUID               uuid.UUID
	ActorUUID          uuid.UUID
	TargetUUID         uuid.UUID
	State              string
	Reason             string
	IPAddress          *string
	UserAgent          *string
	StartedAt          time.Time
	ExpiresAt          time.Time
	EndedAt            *time.Time
	EndReason          *string
	ParticipantsActive bool
}

const impersonationSelect = `SELECT s.uuid, a.uuid AS actor_uuid, t.uuid AS target_uuid, s.state, s.reason,
	s.ip_address, s.user_agent, s.started_at, s.expires_at, s.ended_at, s.end_reason,
	(a.status = 'active' AND a.deleted_at IS NULL AND t.status = 'active' AND t.deleted_at IS NULL) AS participants_active
	FROM impersonation_sessions s JOIN users a ON a.id = s.actor_id JOIN users t ON t.id = s.target_id`

// Create inserts an active session; a second active session for the same actor is
// impdomain.ErrAlreadyActive. The insert runs in a savepoint so that conflict leaves an
// enclosing transaction usable.
func (r *ImpersonationRepository) Create(ctx context.Context, s impdomain.Session) error {
	err := conn(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		return tx.Exec(`
		INSERT INTO impersonation_sessions (uuid, actor_id, target_id, state, reason, ip_address, user_agent, started_at, expires_at)
		VALUES (?, `+idOf("users")+`, `+idOf("users")+`, ?, ?, ?, ?, ?, ?)`,
			s.UUID, s.ActorUUID, s.TargetUUID, string(s.State), s.Reason, nullableString(s.IP), nullableString(s.UserAgent),
			s.StartedAt, s.ExpiresAt).Error
	})
	if isUniqueViolation(err) {
		return impdomain.ErrAlreadyActive
	}

	return err
}

// Get returns the session with id.
func (r *ImpersonationRepository) Get(ctx context.Context, id uuid.UUID) (impdomain.Session, error) {
	sessions, err := r.list(ctx, impersonationSelect+` WHERE s.uuid = ?`, id)
	if err != nil {
		return impdomain.Session{}, err
	}

	if len(sessions) == 0 {
		return impdomain.Session{}, impdomain.ErrNotFound
	}

	return sessions[0], nil
}

// ActiveForActor returns the actor's active session, if any.
func (r *ImpersonationRepository) ActiveForActor(ctx context.Context, actorID uuid.UUID) (impdomain.Session, bool, error) {
	sessions, err := r.list(ctx, impersonationSelect+` WHERE a.uuid = ? AND s.state = 'active'`, actorID)
	if err != nil || len(sessions) == 0 {
		return impdomain.Session{}, false, err
	}

	return sessions[0], true, nil
}

// End moves an active session to state; false when it had already ended.
func (r *ImpersonationRepository) End(ctx context.Context, id uuid.UUID, state impdomain.State, reason impdomain.EndReason, at time.Time) (bool, error) {
	result := conn(ctx, r.db).Exec(`UPDATE impersonation_sessions SET state = ?, end_reason = ?, ended_at = ?
		WHERE uuid = ? AND state = 'active'`, string(state), string(reason), at, id)
	if result.Error != nil {
		return false, fmt.Errorf("end impersonation session: %w", result.Error)
	}

	return result.RowsAffected == 1, nil
}

// Expired returns up to limit active sessions whose expiry is at or before now.
func (r *ImpersonationRepository) Expired(ctx context.Context, now time.Time, limit int) ([]impdomain.Session, error) {
	return r.list(ctx, impersonationSelect+` WHERE s.state = 'active' AND s.expires_at <= ?
		ORDER BY s.expires_at, s.id LIMIT ?`, now, limit)
}

func (r *ImpersonationRepository) list(ctx context.Context, query string, args ...any) ([]impdomain.Session, error) {
	var rows []impersonationRow
	if err := conn(ctx, r.db).Raw(query, args...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("impersonation sessions: %w", err)
	}

	out := make([]impdomain.Session, 0, len(rows))
	for _, row := range rows {
		s := impdomain.Session{
			UUID: row.UUID, ActorUUID: row.ActorUUID, TargetUUID: row.TargetUUID,
			State: impdomain.State(row.State), Reason: row.Reason,
			IP: derefString(row.IPAddress), UserAgent: derefString(row.UserAgent),
			StartedAt: row.StartedAt, ExpiresAt: row.ExpiresAt, EndedAt: row.EndedAt,
			ParticipantsActive: row.ParticipantsActive,
		}
		if row.EndReason != nil {
			reason := impdomain.EndReason(*row.EndReason)
			s.EndReason = &reason
		}

		out = append(out, s)
	}

	return out, nil
}
