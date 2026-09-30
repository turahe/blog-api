package persistence

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/core/newsletter/domain"
	"github.com/turahe/blog-api/internal/core/newsletter/ports"
	"github.com/turahe/blog-api/internal/shared/pagination"
	"gorm.io/gorm"
)

var newsletterSubscribersCfg = pagination.CursorConfig{
	Kind: "newsletter_subscribers_admin",
	Sort: []pagination.SortField{
		{Name: "subscribed_at", Column: "s.created_at", Dir: pagination.Desc, Type: pagination.TypeTime},
		{Name: "email", Column: "s.normalized_email", Dir: pagination.Asc, Type: pagination.TypeString},
		{Name: "id", Column: "s.id", Dir: pagination.Desc, Type: pagination.TypeInt64},
	},
	TTL:            pagination.DefaultTTL,
	MaxPerPage:     pagination.DefaultMaxPerPage,
	DefaultPerPage: pagination.DefaultPerPage,
}

// NewsletterRepository stores newsletter lists, subscribers, tokens, consent history, issues,
// and deliveries in PostgreSQL.
type NewsletterRepository struct {
	db *gorm.DB
}

var _ ports.Repository = (*NewsletterRepository)(nil)

// NewNewsletterRepository returns a NewsletterRepository over db.
func NewNewsletterRepository(db *gorm.DB) *NewsletterRepository {
	return &NewsletterRepository{db: db}
}

type newsletterListRow struct {
	UUID        uuid.UUID
	Slug        string
	Name        string
	Description string
	IsDefault   bool
	Position    int
	ArchivedAt  *time.Time
}

// Lists returns the lists in display order.
func (r *NewsletterRepository) Lists(ctx context.Context, includeArchived bool) ([]domain.List, error) {
	query := `SELECT uuid, slug, name, description, is_default, position, archived_at FROM newsletter_lists`
	if !includeArchived {
		query += ` WHERE archived_at IS NULL`
	}

	var rows []newsletterListRow
	if err := conn(ctx, r.db).Raw(query + ` ORDER BY archived_at NULLS FIRST, position, slug`).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("newsletter lists: %w", err)
	}

	out := make([]domain.List, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.List(row))
	}

	return out, nil
}

// SaveLists upserts lists by slug in the given order and archives the other active lists.
func (r *NewsletterRepository) SaveLists(ctx context.Context, lists []domain.ListInput, at time.Time) error {
	db := conn(ctx, r.db)
	slugs := make([]string, 0, len(lists))

	for i, l := range lists {
		slugs = append(slugs, l.Slug)

		err := db.Exec(`INSERT INTO newsletter_lists (slug, name, description, is_default, position, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (slug) DO UPDATE SET name = EXCLUDED.name, description = EXCLUDED.description,
				is_default = EXCLUDED.is_default, position = EXCLUDED.position, archived_at = NULL,
				updated_at = EXCLUDED.updated_at`,
			l.Slug, strings.TrimSpace(l.Name), l.Description, l.IsDefault, i, at, at).Error
		if err != nil {
			return fmt.Errorf("save newsletter list %s: %w", l.Slug, err)
		}
	}

	err := db.Exec(`UPDATE newsletter_lists SET archived_at = ?, is_default = false, updated_at = ?
		WHERE archived_at IS NULL AND slug NOT IN ?`, at, at, slugs).Error
	if err != nil {
		return fmt.Errorf("archive newsletter lists: %w", err)
	}

	return nil
}

type newsletterConfigRow struct {
	FromName            string
	FromEmail           string
	ReplyTo             string
	PostalAddress       string
	ConfirmTTLSeconds   int
	DoubleOptinRequired bool
	UpdatedByUUID       *uuid.UUID
	UpdatedAt           time.Time
}

// Config returns the stored settings or the defaults.
func (r *NewsletterRepository) Config(ctx context.Context) (domain.Config, error) {
	var rows []newsletterConfigRow

	err := conn(ctx, r.db).Raw(`SELECT c.from_name, c.from_email, c.reply_to, c.postal_address, c.confirm_ttl_seconds,
		c.double_optin_required, u.uuid AS updated_by_uuid, c.updated_at
		FROM newsletter_provider_config c LEFT JOIN users u ON u.id = c.updated_by WHERE c.id = 1`).Scan(&rows).Error
	if err != nil {
		return domain.Config{}, fmt.Errorf("newsletter config: %w", err)
	}

	if len(rows) == 0 {
		return domain.DefaultConfig(), nil
	}

	row := rows[0]

	return domain.Config{
		FromName: row.FromName, FromEmail: row.FromEmail, ReplyTo: row.ReplyTo, PostalAddress: row.PostalAddress,
		ConfirmTTL: time.Duration(row.ConfirmTTLSeconds) * time.Second, DoubleOptInRequired: row.DoubleOptinRequired,
		UpdatedAt: &row.UpdatedAt, UpdatedBy: row.UpdatedByUUID,
	}, nil
}

// SaveConfig upserts the settings row.
func (r *NewsletterRepository) SaveConfig(ctx context.Context, cfg domain.Config) error {
	updatedAt := time.Now().UTC()
	if cfg.UpdatedAt != nil {
		updatedAt = *cfg.UpdatedAt
	}

	err := conn(ctx, r.db).Exec(`INSERT INTO newsletter_provider_config
		(id, from_name, from_email, reply_to, postal_address, confirm_ttl_seconds, double_optin_required, updated_by, updated_at)
		VALUES (1, ?, ?, ?, ?, ?, ?, `+idOf("users")+`, ?)
		ON CONFLICT (id) DO UPDATE SET from_name = EXCLUDED.from_name, from_email = EXCLUDED.from_email,
			reply_to = EXCLUDED.reply_to, postal_address = EXCLUDED.postal_address,
			confirm_ttl_seconds = EXCLUDED.confirm_ttl_seconds, double_optin_required = EXCLUDED.double_optin_required,
			updated_by = EXCLUDED.updated_by, updated_at = EXCLUDED.updated_at`,
		cfg.FromName, cfg.FromEmail, cfg.ReplyTo, cfg.PostalAddress, int(cfg.ConfirmTTL/time.Second),
		cfg.DoubleOptInRequired, cfg.UpdatedBy, updatedAt).Error
	if err != nil {
		return fmt.Errorf("save newsletter config: %w", err)
	}

	return nil
}

type newsletterSubscriberRow struct {
	ID                     int64
	UUID                   uuid.UUID
	Email                  *string
	DisplayName            *string
	UserUUID               *uuid.UUID
	Status                 string
	Format                 string
	Source                 string
	IPHash                 *string
	UserAgent              *string
	ConfirmSends           int
	ConfirmWindowStartedAt *time.Time
	OptedInAt              *time.Time
	UnsubscribedAt         *time.Time
	BouncedAt              *time.Time
	ComplainedAt           *time.Time
	ErasedAt               *time.Time
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

const newsletterSubscriberSelect = `SELECT s.id, s.uuid, s.email, s.display_name, u.uuid AS user_uuid, s.status, s.format,
	s.source, s.ip_hash, s.user_agent, s.confirm_sends, s.confirm_window_started_at, s.opted_in_at, s.unsubscribed_at,
	s.bounced_at, s.complained_at, s.erased_at, s.created_at, s.updated_at
	FROM newsletter_subscribers s LEFT JOIN users u ON u.id = s.user_id`

// Subscriber returns the subscriber with id and its memberships.
func (r *NewsletterRepository) Subscriber(ctx context.Context, id uuid.UUID) (domain.Subscriber, error) {
	return r.oneSubscriber(ctx, newsletterSubscriberSelect+` WHERE s.uuid = ?`, id)
}

// SubscriberByEmail returns the subscriber with the normalized address.
func (r *NewsletterRepository) SubscriberByEmail(ctx context.Context, normalized string) (domain.Subscriber, error) {
	return r.oneSubscriber(ctx, newsletterSubscriberSelect+` WHERE s.normalized_email = ?`, normalized)
}

// SubscriberByUser returns the subscriber linked to the user.
func (r *NewsletterRepository) SubscriberByUser(ctx context.Context, userID uuid.UUID) (domain.Subscriber, error) {
	return r.oneSubscriber(ctx, newsletterSubscriberSelect+` WHERE u.uuid = ?`, userID)
}

func (r *NewsletterRepository) oneSubscriber(ctx context.Context, query string, args ...any) (domain.Subscriber, error) {
	subs, err := r.subscribers(ctx, query+` LIMIT 1`, args...)
	if err != nil {
		return domain.Subscriber{}, err
	}

	if len(subs) == 0 {
		return domain.Subscriber{}, domain.ErrNotFound
	}

	return subs[0], nil
}

// CreateSubscriber inserts s and its memberships in a savepoint, so a taken address leaves an
// enclosing transaction usable.
func (r *NewsletterRepository) CreateSubscriber(ctx context.Context, s domain.Subscriber) error {
	err := conn(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		err := tx.Exec(`INSERT INTO newsletter_subscribers (uuid, email, normalized_email, display_name, user_id, status,
			format, source, ip_hash, user_agent, confirm_sends, confirm_window_started_at, opted_in_at, created_at, updated_at)
			VALUES (?, ?, ?, ?, `+idOf("users")+`, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			s.UUID, s.Email, s.Email, nullableString(s.DisplayName), s.UserID, string(s.Status), string(s.Format),
			string(s.Source), nullableString(s.IPHash), nullableString(s.UserAgent), s.ConfirmSends, s.ConfirmWindowStart,
			s.OptedInAt, s.CreatedAt, s.UpdatedAt).Error
		if err != nil {
			return err
		}

		return saveMemberships(tx, s.UUID, s.Memberships, s.UpdatedAt)
	})
	if isUniqueViolation(err) {
		return domain.ErrConflict
	}

	if err != nil {
		return fmt.Errorf("create newsletter subscriber: %w", err)
	}

	return nil
}

// UpdateSubscriber saves the mutable columns and, when modified, the memberships.
func (r *NewsletterRepository) UpdateSubscriber(ctx context.Context, s domain.Subscriber) error {
	db := conn(ctx, r.db)

	err := db.Exec(`UPDATE newsletter_subscribers SET display_name = ?, user_id = `+idOf("users")+`, status = ?, format = ?,
		confirm_sends = ?, confirm_window_started_at = ?, opted_in_at = ?, unsubscribed_at = ?, bounced_at = ?,
		complained_at = ?, updated_at = ? WHERE uuid = ?`,
		nullableString(s.DisplayName), s.UserID, string(s.Status), string(s.Format), s.ConfirmSends, s.ConfirmWindowStart,
		s.OptedInAt, s.UnsubscribedAt, s.BouncedAt, s.ComplainedAt, s.UpdatedAt, s.UUID).Error
	if err != nil {
		return fmt.Errorf("update newsletter subscriber: %w", err)
	}

	if !s.MembershipsModified() {
		return nil
	}

	return saveMemberships(db, s.UUID, s.Memberships, s.UpdatedAt)
}

func saveMemberships(db *gorm.DB, subscriberID uuid.UUID, memberships []domain.Membership, at time.Time) error {
	for _, m := range memberships {
		err := db.Exec(`INSERT INTO newsletter_list_memberships (subscriber_id, list_id, state, joined_at, left_at, updated_at)
			SELECT s.id, l.id, ?, ?, ?, ? FROM newsletter_subscribers s, newsletter_lists l WHERE s.uuid = ? AND l.slug = ?
			ON CONFLICT (subscriber_id, list_id) DO UPDATE SET state = EXCLUDED.state, joined_at = EXCLUDED.joined_at,
				left_at = EXCLUDED.left_at, updated_at = EXCLUDED.updated_at`,
			string(m.State), m.JoinedAt, m.LeftAt, at, subscriberID, m.ListSlug).Error
		if err != nil {
			return fmt.Errorf("save newsletter membership %s: %w", m.ListSlug, err)
		}
	}

	return nil
}

// ListSubscribers returns one page filtered by status, list, and an email or name prefix.
// Uses cursor-based keyset pagination when after/before cursors are present, and offset
// pagination when the caller provides only page/perPage via ModeOffset.
func (r *NewsletterRepository) ListSubscribers(ctx context.Context, filter domain.SubscriberFilter) (domain.SubscriberPage, error) {
	pr := filter.PageRequest
	if pr.Limit <= 0 {
		pr.Limit = newsletterSubscribersCfg.DefaultPerPage
	}

	var cursorFields map[string]any
	if pr.Mode == pagination.ModeCursor && pr.Cursor != "" {
		decoded, _, err := pagination.DecodeCursor(newsletterSubscribersCfg, pr.Cursor)
		if err != nil {
			return domain.SubscriberPage{}, err
		}
		if err := pagination.ValidateCursor(newsletterSubscribersCfg, decoded); err != nil {
			return domain.SubscriberPage{}, err
		}
		cursorFields = decoded
	}
	seek, err := pagination.BuildSeek(newsletterSubscribersCfg, pr, cursorFields)
	if err != nil {
		return domain.SubscriberPage{}, err
	}

	where, args := []string{"TRUE"}, []any{}

	if filter.Status != "" {
		where, args = append(where, "s.status = ?"), append(args, string(filter.Status))
	}

	if filter.List != "" {
		where = append(where, `EXISTS (SELECT 1 FROM newsletter_list_memberships m JOIN newsletter_lists l ON l.id = m.list_id
			WHERE m.subscriber_id = s.id AND l.slug = ? AND m.state <> 'left')`)
		args = append(args, filter.List)
	}

	if q := strings.TrimSpace(filter.Query); q != "" {
		pattern := escapeLike(strings.ToLower(q)) + "%"

		where = append(where, `(s.normalized_email LIKE ? ESCAPE '\' OR lower(s.display_name) LIKE ? ESCAPE '\')`)
		args = append(args, pattern, pattern)
	}

	if seek.WhereClause != "" {
		where = append(where, seek.WhereClause)
		args = append(args, seek.BindVars...)
	}

	clause := " WHERE " + strings.Join(where, " AND ")

	var out domain.SubscriberPage
	if pr.IncludeTotal {
		var total int64
		if err := conn(ctx, r.db).Raw(`SELECT count(*) FROM newsletter_subscribers s`+clause, args...).Scan(&total).Error; err != nil {
			return domain.SubscriberPage{}, fmt.Errorf("count newsletter subscribers: %w", err)
		}
		out.Total = &total
	}

	query := newsletterSubscriberSelect + clause + " " + seek.OrderClause + " LIMIT ?"
	queryArgs := append([]any{}, args...)
	queryArgs = append(queryArgs, seek.LimitFetch)
	if pr.Mode == pagination.ModeOffset {
		query += " OFFSET ?"
		queryArgs = append(queryArgs, pr.Offset)
	}

	items, err := r.subscribers(ctx, query, queryArgs...)
	if err != nil {
		return domain.SubscriberPage{}, err
	}

	if seek.ReverseDisplay {
		for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
			items[i], items[j] = items[j], items[i]
		}
	}

	page, hasNext, hasPrev := pagination.TruncatePage(items, pr.Limit, pr.Forward, pr.Cursor != "")
	out.Items = page
	out.HasNextPage = hasNext
	out.HasPreviousPage = hasPrev
	out.Limit = pr.Limit
	if pr.Mode == pagination.ModeOffset {
		out.OffsetPage = pr.Page
		out.OffsetPerPage = pr.Limit
	}

	if len(page) > 0 {
		if out.HasNextPage {
			fields := pagination.SortValues[domain.Subscriber](newsletterSubscribersCfg, page[len(page)-1],
				func(row domain.Subscriber, i int) any {
					switch i {
					case 0:
						return row.CreatedAt
					case 1:
						return strings.ToLower(strings.TrimSpace(row.Email))
					case 2:
						return row.ID
					}
					return nil
				})
			cur, err := pagination.EncodeCursor(newsletterSubscribersCfg, fields)
			if err != nil {
				return domain.SubscriberPage{}, err
			}
			out.NextCursor = cur
		}
		if out.HasPreviousPage {
			fields := pagination.SortValues[domain.Subscriber](newsletterSubscribersCfg, page[0],
				func(row domain.Subscriber, i int) any {
					switch i {
					case 0:
						return row.CreatedAt
					case 1:
						return strings.ToLower(strings.TrimSpace(row.Email))
					case 2:
						return row.ID
					}
					return nil
				})
			cur, err := pagination.EncodeCursor(newsletterSubscribersCfg, fields)
			if err != nil {
				return domain.SubscriberPage{}, err
			}
			out.PreviousCursor = cur
		}
	}

	return out, nil
}

func escapeLike(value string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(value)
}

// EraseSubscriber nulls the personal data, leaves every list, scrubs consent feedback and IP
// hashes, and deletes the subscriber's tokens.
func (r *NewsletterRepository) EraseSubscriber(ctx context.Context, id uuid.UUID, at time.Time) error {
	db := conn(ctx, r.db)
	statements := []struct {
		sql  string
		args []any
	}{
		{`UPDATE newsletter_subscribers SET email = NULL, normalized_email = NULL, display_name = NULL, user_id = NULL,
			ip_hash = NULL, user_agent = NULL, status = 'erased', erased_at = ?, updated_at = ? WHERE uuid = ?`, []any{at, at, id}},
		{`UPDATE newsletter_list_memberships SET state = 'left', left_at = COALESCE(left_at, ?), updated_at = ?
			WHERE subscriber_id = ` + idOf("newsletter_subscribers") + ` AND state <> 'left'`, []any{at, at, id}},
		{`UPDATE newsletter_consent_audit SET feedback = NULL, ip_hash = NULL
			WHERE subscriber_id = ` + idOf("newsletter_subscribers"), []any{id}},
		{`DELETE FROM newsletter_tokens WHERE subscriber_id = ` + idOf("newsletter_subscribers"), []any{id}},
	}

	for _, st := range statements {
		if err := db.Exec(st.sql, st.args...).Error; err != nil {
			return fmt.Errorf("erase newsletter subscriber: %w", err)
		}
	}

	return nil
}

func (r *NewsletterRepository) subscribers(ctx context.Context, query string, args ...any) ([]domain.Subscriber, error) {
	var rows []newsletterSubscriberRow
	if err := conn(ctx, r.db).Raw(query, args...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("newsletter subscribers: %w", err)
	}

	if len(rows) == 0 {
		return nil, nil
	}

	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}

	memberships, err := r.memberships(ctx, ids)
	if err != nil {
		return nil, err
	}

	out := make([]domain.Subscriber, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.Subscriber{
			ID: row.ID, UUID: row.UUID, Email: derefString(row.Email), DisplayName: derefString(row.DisplayName), UserID: row.UserUUID,
			Status: domain.Status(row.Status), Format: domain.Format(row.Format), Source: domain.Source(row.Source),
			IPHash: derefString(row.IPHash), UserAgent: derefString(row.UserAgent), ConfirmSends: row.ConfirmSends,
			ConfirmWindowStart: row.ConfirmWindowStartedAt, OptedInAt: row.OptedInAt, UnsubscribedAt: row.UnsubscribedAt,
			BouncedAt: row.BouncedAt, ComplainedAt: row.ComplainedAt, ErasedAt: row.ErasedAt,
			CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, Memberships: memberships[row.ID],
		})
	}

	return out, nil
}

type newsletterMembershipRow struct {
	SubscriberID int64
	Slug         string
	Name         string
	State        string
	JoinedAt     *time.Time
	LeftAt       *time.Time
}

func (r *NewsletterRepository) memberships(ctx context.Context, subscriberIDs []int64) (map[int64][]domain.Membership, error) {
	var rows []newsletterMembershipRow

	err := conn(ctx, r.db).Raw(`SELECT m.subscriber_id, l.slug, l.name, m.state, m.joined_at, m.left_at
		FROM newsletter_list_memberships m JOIN newsletter_lists l ON l.id = m.list_id
		WHERE m.subscriber_id IN ? ORDER BY l.position, l.slug`, subscriberIDs).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("newsletter memberships: %w", err)
	}

	out := make(map[int64][]domain.Membership, len(subscriberIDs))
	for _, row := range rows {
		out[row.SubscriberID] = append(out[row.SubscriberID], domain.Membership{
			ListSlug: row.Slug, ListName: row.Name, State: domain.MembershipState(row.State),
			JoinedAt: row.JoinedAt, LeftAt: row.LeftAt,
		})
	}

	return out, nil
}

// CreateToken stores a token hash.
func (r *NewsletterRepository) CreateToken(ctx context.Context, t domain.Token) error {
	err := conn(ctx, r.db).Exec(`INSERT INTO newsletter_tokens (subscriber_id, purpose, token_hash, issue_id, expires_at, created_at)
		VALUES (`+idOf("newsletter_subscribers")+`, ?, ?, `+idOf("newsletter_issues")+`, ?, ?)`,
		t.SubscriberID, string(t.Purpose), t.Hash, t.IssueID, t.ExpiresAt, t.CreatedAt).Error
	if err != nil {
		return fmt.Errorf("create newsletter token: %w", err)
	}

	return nil
}

type newsletterTokenRow struct {
	ID             int64
	SubscriberUUID uuid.UUID
	Purpose        string
	TokenHash      string
	IssueUUID      *uuid.UUID
	ExpiresAt      time.Time
	UsedAt         *time.Time
	CreatedAt      time.Time
}

// Token returns the token with hash.
func (r *NewsletterRepository) Token(ctx context.Context, hash string) (domain.Token, error) {
	var rows []newsletterTokenRow

	err := conn(ctx, r.db).Raw(`SELECT t.id, s.uuid AS subscriber_uuid, t.purpose, t.token_hash, i.uuid AS issue_uuid,
		t.expires_at, t.used_at, t.created_at
		FROM newsletter_tokens t JOIN newsletter_subscribers s ON s.id = t.subscriber_id
		LEFT JOIN newsletter_issues i ON i.id = t.issue_id WHERE t.token_hash = ?`, hash).Scan(&rows).Error
	if err != nil {
		return domain.Token{}, fmt.Errorf("newsletter token: %w", err)
	}

	if len(rows) == 0 {
		return domain.Token{}, domain.ErrNotFound
	}

	row := rows[0]

	return domain.Token{
		ID: row.ID, SubscriberID: row.SubscriberUUID, Purpose: domain.TokenPurpose(row.Purpose), Hash: row.TokenHash,
		IssueID: row.IssueUUID, ExpiresAt: row.ExpiresAt, UsedAt: row.UsedAt, CreatedAt: row.CreatedAt,
	}, nil
}

// UseToken marks an unused token used.
func (r *NewsletterRepository) UseToken(ctx context.Context, id int64, at time.Time) (bool, error) {
	result := conn(ctx, r.db).Exec(`UPDATE newsletter_tokens SET used_at = ? WHERE id = ? AND used_at IS NULL`, at, id)
	if result.Error != nil {
		return false, fmt.Errorf("use newsletter token: %w", result.Error)
	}

	return result.RowsAffected == 1, nil
}

// RevokeTokens deletes the subscriber's tokens for purpose.
func (r *NewsletterRepository) RevokeTokens(ctx context.Context, subscriberID uuid.UUID, purpose domain.TokenPurpose) error {
	err := conn(ctx, r.db).Exec(`DELETE FROM newsletter_tokens WHERE subscriber_id = `+idOf("newsletter_subscribers")+
		` AND purpose = ?`, subscriberID, string(purpose)).Error
	if err != nil {
		return fmt.Errorf("revoke newsletter tokens: %w", err)
	}

	return nil
}

// PruneTokens deletes tokens that expired before before.
func (r *NewsletterRepository) PruneTokens(ctx context.Context, before time.Time) (int64, error) {
	result := conn(ctx, r.db).Exec(`DELETE FROM newsletter_tokens WHERE expires_at < ?`, before)
	if result.Error != nil {
		return 0, fmt.Errorf("prune newsletter tokens: %w", result.Error)
	}

	return result.RowsAffected, nil
}

// AppendConsent inserts consent events.
func (r *NewsletterRepository) AppendConsent(ctx context.Context, events ...domain.ConsentEvent) error {
	db := conn(ctx, r.db)

	for _, e := range events {
		err := db.Exec(`INSERT INTO newsletter_consent_audit (subscriber_id, event, list_slug, source, reason_code, feedback, ip_hash, occurred_at)
			VALUES (`+idOf("newsletter_subscribers")+`, ?, ?, ?, ?, ?, ?, ?)`,
			e.SubscriberID, e.Event, nullableString(e.ListSlug), e.Source, nullableString(e.ReasonCode),
			nullableString(e.Feedback), nullableString(e.IPHash), e.OccurredAt).Error
		if err != nil {
			return fmt.Errorf("append newsletter consent: %w", err)
		}
	}

	return nil
}

type newsletterConsentRow struct {
	Event      string
	ListSlug   *string
	Source     string
	ReasonCode *string
	Feedback   *string
	IPHash     *string
	OccurredAt time.Time
}

// ConsentHistory returns the subscriber's latest consent events, newest first.
func (r *NewsletterRepository) ConsentHistory(ctx context.Context, subscriberID uuid.UUID, limit int) ([]domain.ConsentEvent, error) {
	var rows []newsletterConsentRow

	err := conn(ctx, r.db).Raw(`SELECT event, list_slug, source, reason_code, feedback, ip_hash, occurred_at
		FROM newsletter_consent_audit WHERE subscriber_id = `+idOf("newsletter_subscribers")+`
		ORDER BY occurred_at DESC, id DESC LIMIT ?`, subscriberID, limit).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("newsletter consent history: %w", err)
	}

	out := make([]domain.ConsentEvent, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.ConsentEvent{
			SubscriberID: subscriberID, Event: row.Event, ListSlug: derefString(row.ListSlug), Source: row.Source,
			ReasonCode: derefString(row.ReasonCode), Feedback: derefString(row.Feedback), IPHash: derefString(row.IPHash),
			OccurredAt: row.OccurredAt,
		})
	}

	return out, nil
}
