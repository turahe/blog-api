package persistence

import (
	"context"
	"time"

	"github.com/google/uuid"
	consentdomain "github.com/turahe/blog-api/internal/core/consent/domain"
	"gorm.io/gorm"
)

// ConsentRepository stores analytics consent subjects and decisions in PostgreSQL.
type ConsentRepository struct {
	db *gorm.DB
}

// NewConsentRepository returns a ConsentRepository over db.
func NewConsentRepository(db *gorm.DB) *ConsentRepository {
	return &ConsentRepository{db: db}
}

type consentSubjectRow struct {
	UUID       uuid.UUID
	UserUUID   *uuid.UUID
	CreatedAt  time.Time
	LastSeenAt time.Time
}

type consentRow struct {
	UUID          uuid.UUID
	SubjectUUID   uuid.UUID
	Purpose       string
	Status        string
	PolicyVersion string
	DecidedAt     time.Time
	WithdrawnAt   *time.Time
}

const consentSubjectSelect = `
	SELECT s.uuid, u.uuid AS user_uuid, s.created_at, s.last_seen_at
	FROM consent_subjects s
	LEFT JOIN users u ON u.id = s.user_id`

const consentSelect = `
	SELECT c.uuid, s.uuid AS subject_uuid, c.purpose, c.status, c.policy_version, c.decided_at, c.withdrawn_at
	FROM analytics_consents c
	JOIN consent_subjects s ON s.id = c.subject_id`

// CreateSubject stores a new subject under tokenHash.
func (r *ConsentRepository) CreateSubject(ctx context.Context, subject consentdomain.Subject, tokenHash string) error {
	return conn(ctx, r.db).Exec(`
		INSERT INTO consent_subjects (uuid, token_hash, created_at, last_seen_at) VALUES (?, ?, ?, ?)`,
		subject.UUID, tokenHash, subject.CreatedAt, subject.LastSeenAt).Error
}

// SubjectByToken returns the subject for tokenHash.
func (r *ConsentRepository) SubjectByToken(ctx context.Context, tokenHash string) (consentdomain.Subject, error) {
	return r.subject(ctx, consentSubjectSelect+" WHERE s.token_hash = ?", tokenHash)
}

func (r *ConsentRepository) subject(ctx context.Context, query string, args ...any) (consentdomain.Subject, error) {
	var rows []consentSubjectRow
	if err := conn(ctx, r.db).Raw(query, args...).Scan(&rows).Error; err != nil {
		return consentdomain.Subject{}, err
	}

	if len(rows) == 0 {
		return consentdomain.Subject{}, consentdomain.ErrNotFound
	}

	row := rows[0]

	return consentdomain.Subject{UUID: row.UUID, UserUUID: row.UserUUID, CreatedAt: row.CreatedAt, LastSeenAt: row.LastSeenAt}, nil
}

// SetUser links or unlinks the subject's user.
func (r *ConsentRepository) SetUser(ctx context.Context, subjectID uuid.UUID, userID *uuid.UUID) error {
	return conn(ctx, r.db).Exec(`
		UPDATE consent_subjects SET user_id = (SELECT id FROM users WHERE uuid = ?) WHERE uuid = ?`,
		userID, subjectID).Error
}

// Touch records that the subject was seen at.
func (r *ConsentRepository) Touch(ctx context.Context, subjectID uuid.UUID, at time.Time) error {
	return conn(ctx, r.db).Exec(`UPDATE consent_subjects SET last_seen_at = ? WHERE uuid = ?`, at, subjectID).Error
}

// List returns the subject's consents in purpose order.
func (r *ConsentRepository) List(ctx context.Context, subjectID uuid.UUID) ([]consentdomain.Consent, error) {
	var rows []consentRow

	err := conn(ctx, r.db).Raw(consentSelect+" WHERE s.uuid = ? ORDER BY c.purpose", subjectID).Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	out := make([]consentdomain.Consent, 0, len(rows))
	for _, row := range rows {
		out = append(out, mapConsent(row))
	}

	return out, nil
}

// Get returns a consent and its subject.
func (r *ConsentRepository) Get(ctx context.Context, consentID uuid.UUID) (consentdomain.Consent, consentdomain.Subject, error) {
	var rows []consentRow
	if err := conn(ctx, r.db).Raw(consentSelect+" WHERE c.uuid = ?", consentID).Scan(&rows).Error; err != nil {
		return consentdomain.Consent{}, consentdomain.Subject{}, err
	}

	if len(rows) == 0 {
		return consentdomain.Consent{}, consentdomain.Subject{}, consentdomain.ErrNotFound
	}

	subject, err := r.subject(ctx, consentSubjectSelect+" WHERE s.uuid = ?", rows[0].SubjectUUID)
	if err != nil {
		return consentdomain.Consent{}, consentdomain.Subject{}, err
	}

	return mapConsent(rows[0]), subject, nil
}

// Save inserts or replaces the subject's consent for its purpose.
func (r *ConsentRepository) Save(ctx context.Context, c consentdomain.Consent) error {
	return conn(ctx, r.db).Exec(`
		INSERT INTO analytics_consents (uuid, subject_id, purpose, status, policy_version, decided_at, withdrawn_at)
		VALUES (?, `+idOf("consent_subjects")+`, ?, ?, ?, ?, ?)
		ON CONFLICT (subject_id, purpose) DO UPDATE SET
			status = EXCLUDED.status, policy_version = EXCLUDED.policy_version,
			decided_at = EXCLUDED.decided_at, withdrawn_at = EXCLUDED.withdrawn_at`,
		c.UUID, c.SubjectUUID, string(c.Purpose), string(c.Status), c.PolicyVersion, c.DecidedAt, c.WithdrawnAt).Error
}

// deleteUserSubjects deletes the consent subjects of the user whose id is userID (an SQL
// expression) with their raw analytics events and first-seen records, and counts the subjects;
// their consents cascade.
func deleteUserSubjects(userID string) string {
	return `
WITH gone AS (
	DELETE FROM consent_subjects WHERE user_id = ` + userID + ` RETURNING uuid
),` + subjectEventDeletes + `
SELECT count(*) FROM gone`
}

// subjectEventDeletes are CTEs deleting the raw analytics events and first-seen records of the
// subjects in the CTE gone.
const subjectEventDeletes = `
page_views AS (DELETE FROM analytics_page_views WHERE subject_uuid IN (SELECT uuid FROM gone)),
time_spent AS (DELETE FROM analytics_time_spent WHERE subject_uuid IN (SELECT uuid FROM gone)),
navigation AS (DELETE FROM analytics_navigation WHERE subject_uuid IN (SELECT uuid FROM gone)),
searches AS (DELETE FROM analytics_searches WHERE subject_uuid IN (SELECT uuid FROM gone)),
clicks AS (DELETE FROM analytics_search_clicks WHERE subject_uuid IN (SELECT uuid FROM gone)),
first_seen AS (DELETE FROM analytics_subject_first_seen WHERE subject_uuid IN (SELECT uuid FROM gone))`

// DeleteSubjectEvents deletes the raw analytics events and first-seen record of the subject.
func (r *ConsentRepository) DeleteSubjectEvents(ctx context.Context, subjectID uuid.UUID) error {
	return conn(ctx, r.db).Exec(`WITH gone AS (SELECT CAST(? AS uuid) AS uuid),`+subjectEventDeletes+`
SELECT count(*) FROM gone`, subjectID).Error
}

// DeleteForUser deletes the subjects linked to the user with their raw analytics events and
// first-seen records; their consents cascade.
func (r *ConsentRepository) DeleteForUser(ctx context.Context, userID uuid.UUID) (int64, error) {
	var deleted int64

	err := conn(ctx, r.db).Raw(deleteUserSubjects(idOf("users")), userID).Scan(&deleted).Error

	return deleted, err
}

func mapConsent(row consentRow) consentdomain.Consent {
	return consentdomain.Consent{
		UUID:          row.UUID,
		SubjectUUID:   row.SubjectUUID,
		Purpose:       consentdomain.Purpose(row.Purpose),
		Status:        consentdomain.Status(row.Status),
		PolicyVersion: row.PolicyVersion,
		DecidedAt:     row.DecidedAt,
		WithdrawnAt:   row.WithdrawnAt,
	}
}
