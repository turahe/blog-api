package persistence

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// errUserGone reports an export or erasure of a user that no longer exists.
var errUserGone = errors.New("user not found")

// ErasedFullName replaces the name of an erased account; authored content keeps showing it.
const ErasedFullName = "Deleted user"

// PrivacyData exports and erases everything stored about a user.
type PrivacyData struct {
	db *gorm.DB
}

// NewPrivacyData returns a PrivacyData over db.
func NewPrivacyData(db *gorm.DB) *PrivacyData {
	return &PrivacyData{db: db}
}

// exportDocument builds the whole export in one query so it is a consistent snapshot.
const exportDocument = `SELECT jsonb_build_object(
	'format_version', 1,
	'generated_at', ?::timestamptz,
	'account', jsonb_build_object(
		'id', u.uuid, 'email', u.email, 'username', u.username, 'full_name', u.full_name,
		'status', u.status, 'email_verified_at', u.email_verified_at, 'last_login_at', u.last_login_at,
		'created_at', u.created_at,
		'two_factor_enabled', EXISTS (SELECT 1 FROM user_two_factor_methods t WHERE t.user_id = u.id AND t.confirmed_at IS NOT NULL),
		'roles', COALESCE((SELECT jsonb_agg(ro.name ORDER BY ro.name) FROM user_roles ur JOIN roles ro ON ro.id = ur.role_id
			WHERE ur.user_id = u.id), '[]'::jsonb)),
	'profile', (SELECT to_jsonb(p) - 'user_id' - 'updated_by' FROM user_profiles p WHERE p.user_id = u.id),
	'privacy', (SELECT to_jsonb(s) - 'user_id' FROM user_privacy_settings s WHERE s.user_id = u.id),
	'activity', COALESCE((SELECT jsonb_agg(jsonb_build_object(
		'id', a.uuid, 'action', a.action, 'category', a.category, 'result', a.result,
		'resource_type', a.resource_type, 'resource_id', a.resource_id, 'ip_address', a.ip_address,
		'user_agent', a.user_agent, 'metadata', a.metadata, 'occurred_at', a.occurred_at) ORDER BY a.occurred_at, a.id)
		FROM audit_logs a WHERE a.actor_id = u.id), '[]'::jsonb),
	'sessions', COALESCE((SELECT jsonb_agg(jsonb_build_object(
		'created_at', rs.created_at, 'expires_at', rs.expires_at, 'revoked_at', rs.revoked_at,
		'user_agent', rs.user_agent, 'ip_address', rs.ip_address) ORDER BY rs.created_at, rs.id)
		FROM refresh_sessions rs WHERE rs.user_id = u.id), '[]'::jsonb),
	'oauth_identities', COALESCE((SELECT jsonb_agg(jsonb_build_object(
		'provider', o.provider, 'email', o.email, 'created_at', o.created_at, 'last_used_at', o.last_used_at)
		ORDER BY o.provider) FROM user_oauth_identities o WHERE o.user_id = u.id), '[]'::jsonb),
	'analytics_consents', COALESCE((SELECT jsonb_agg(jsonb_build_object(
		'purpose', c.purpose, 'status', c.status, 'policy_version', c.policy_version,
		'decided_at', c.decided_at, 'withdrawn_at', c.withdrawn_at) ORDER BY c.decided_at, c.id)
		FROM analytics_consents c JOIN consent_subjects cs ON cs.id = c.subject_id WHERE cs.user_id = u.id), '[]'::jsonb),
	'posts', COALESCE((SELECT jsonb_agg(jsonb_build_object(
		'id', po.uuid, 'title', po.title, 'slug', po.slug, 'status', po.status, 'excerpt', po.excerpt,
		'content', po.content, 'created_at', po.created_at, 'published_at', po.published_at, 'deleted_at', po.deleted_at)
		ORDER BY po.created_at, po.id) FROM posts po WHERE po.author_id = u.id), '[]'::jsonb),
	'comments', COALESCE((SELECT jsonb_agg(jsonb_build_object(
		'id', cm.uuid, 'post_id', (SELECT pp.uuid FROM posts pp WHERE pp.id = cm.post_id), 'content', cm.content,
		'status', cm.status, 'created_at', cm.created_at, 'edited_at', cm.edited_at)
		ORDER BY cm.created_at, cm.id) FROM comments cm WHERE cm.author_id = u.id), '[]'::jsonb),
	'media', COALESCE((SELECT jsonb_agg(jsonb_build_object(
		'id', m.uuid, 'original_filename', m.original_filename, 'content_type', m.content_type,
		'size_bytes', m.size_bytes, 'created_at', m.created_at, 'deleted_at', m.deleted_at)
		ORDER BY m.created_at, m.id) FROM media_assets m WHERE m.uploaded_by = u.id), '[]'::jsonb),
	'notifications', COALESCE((SELECT jsonb_agg(jsonb_build_object(
		'id', n.uuid, 'type', n.type, 'title', n.title, 'body', n.body, 'read_at', n.read_at, 'created_at', n.created_at)
		ORDER BY n.created_at, n.id) FROM notifications n WHERE n.user_id = u.id), '[]'::jsonb)
)::text AS document
FROM users u WHERE u.uuid = ? AND u.deleted_at IS NULL`

// ExportUser returns the user's data as a JSON document.
func (d *PrivacyData) ExportUser(ctx context.Context, userID uuid.UUID, at time.Time) ([]byte, error) {
	var documents []string
	if err := conn(ctx, d.db).Raw(exportDocument, at, userID).Scan(&documents).Error; err != nil {
		return nil, fmt.Errorf("export user: %w", err)
	}

	if len(documents) == 0 {
		return nil, errUserGone
	}

	return []byte(documents[0]), nil
}

// eraseStatements run in order in one transaction. Posts, comments, uploads, and admin audit
// rows stay, attributed to the anonymized account.
var eraseStatements = []string{
	`DELETE FROM audit_logs WHERE actor_id = @id AND category IS NOT NULL`,
	`UPDATE audit_logs SET ip_address = NULL, user_agent = NULL, request_id = NULL, metadata = '{}'::jsonb WHERE actor_id = @id`,
	deleteUserSubjects("@id"),
	`DELETE FROM refresh_sessions WHERE user_id = @id`,
	`DELETE FROM password_reset_tokens WHERE user_id = @id`,
	`DELETE FROM user_two_factor_backup_codes WHERE user_id = @id`,
	`DELETE FROM user_two_factor_methods WHERE user_id = @id`,
	`DELETE FROM user_oauth_identities WHERE user_id = @id`,
	`DELETE FROM notifications WHERE user_id = @id`,
	`DELETE FROM user_profiles WHERE user_id = @id`,
	`DELETE FROM user_privacy_settings WHERE user_id = @id`,
	`UPDATE comments SET author_name = NULL, author_email = NULL, ip_hash = NULL, user_agent = NULL WHERE author_id = @id`,
	`UPDATE media_assets SET deleted_at = @at
		WHERE id = (SELECT avatar_id FROM users WHERE id = @id) AND uploaded_by = @id
		AND 'avatar' = ANY (tags) AND deleted_at IS NULL`,
	`UPDATE users SET
		email = 'erased+' || uuid::text || '@invalid',
		username = 'erased-' || replace(uuid::text, '-', ''),
		full_name = @name, password_hash = NULL, avatar_id = NULL, status = 'deleted',
		email_verified_at = NULL, last_login_at = NULL, updated_at = @at
		WHERE id = @id`,
}

// EraseUser anonymizes the user in one transaction. Running it again is harmless.
func (d *PrivacyData) EraseUser(ctx context.Context, userID uuid.UUID, at time.Time) error {
	return conn(ctx, d.db).Transaction(func(tx *gorm.DB) error {
		id, err := idByUUID(tx, "users", userID)
		if errors.Is(err, errUnknownReference) {
			return errUserGone
		}

		if err != nil {
			return err
		}

		args := map[string]any{"id": id, "at": at, "name": ErasedFullName}
		for _, statement := range eraseStatements {
			if err := tx.Exec(statement, args).Error; err != nil {
				return fmt.Errorf("erase user: %w", err)
			}
		}

		return nil
	})
}
