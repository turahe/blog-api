package persistence

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/core/newsletter/domain"
	"gorm.io/gorm"
)

type newsletterIssueRow struct {
	ID            int64
	UUID          uuid.UUID
	Subject       string
	Preheader     string
	BodyMarkdown  string
	Status        string
	SendAt        *time.Time
	QueuedAt      *time.Time
	StartedAt     *time.Time
	CompletedAt   *time.Time
	SentCount     int
	FailedCount   int
	CreatedByUUID *uuid.UUID
	UpdatedByUUID *uuid.UUID
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

const newsletterIssueSelect = `SELECT i.id, i.uuid, i.subject, i.preheader, i.body_markdown, i.status, i.send_at, i.queued_at,
	i.started_at, i.completed_at, i.sent_count, i.failed_count, cu.uuid AS created_by_uuid, uu.uuid AS updated_by_uuid,
	i.created_at, i.updated_at
	FROM newsletter_issues i LEFT JOIN users cu ON cu.id = i.created_by LEFT JOIN users uu ON uu.id = i.updated_by`

// CreateIssue inserts an issue and its lists.
func (r *NewsletterRepository) CreateIssue(ctx context.Context, issue domain.Issue) error {
	return conn(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		err := tx.Exec(`INSERT INTO newsletter_issues (uuid, subject, preheader, body_markdown, status, send_at, queued_at,
			created_by, updated_by, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, `+idOf("users")+`, `+idOf("users")+`, ?, ?)`,
			issue.UUID, issue.Subject, issue.Preheader, issue.BodyMarkdown, string(issue.Status), issue.SendAt,
			issue.QueuedAt, issue.CreatedBy, issue.UpdatedBy, issue.CreatedAt, issue.UpdatedAt).Error
		if err != nil {
			return fmt.Errorf("create newsletter issue: %w", err)
		}

		return replaceIssueLists(tx, issue.UUID, issue.Lists)
	})
}

// UpdateIssue saves the issue while its stored status is still expected.
func (r *NewsletterRepository) UpdateIssue(ctx context.Context, issue domain.Issue, expected domain.IssueStatus) (bool, error) {
	var saved bool

	err := conn(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		result := tx.Exec(`UPDATE newsletter_issues SET subject = ?, preheader = ?, body_markdown = ?, status = ?, send_at = ?,
			queued_at = ?, updated_by = `+idOf("users")+`, updated_at = ? WHERE uuid = ? AND status = ?`,
			issue.Subject, issue.Preheader, issue.BodyMarkdown, string(issue.Status), issue.SendAt, issue.QueuedAt,
			issue.UpdatedBy, issue.UpdatedAt, issue.UUID, string(expected))
		if result.Error != nil {
			return fmt.Errorf("update newsletter issue: %w", result.Error)
		}

		if saved = result.RowsAffected == 1; !saved {
			return nil
		}

		return replaceIssueLists(tx, issue.UUID, issue.Lists)
	})

	return saved, err
}

func replaceIssueLists(tx *gorm.DB, issueID uuid.UUID, slugs []string) error {
	if err := tx.Exec(`DELETE FROM newsletter_issue_lists WHERE issue_id = `+idOf("newsletter_issues"), issueID).Error; err != nil {
		return fmt.Errorf("clear newsletter issue lists: %w", err)
	}

	err := tx.Exec(`INSERT INTO newsletter_issue_lists (issue_id, list_id)
		SELECT i.id, l.id FROM newsletter_issues i, newsletter_lists l WHERE i.uuid = ? AND l.slug IN ?`, issueID, slugs).Error
	if err != nil {
		return fmt.Errorf("save newsletter issue lists: %w", err)
	}

	return nil
}

// Issue returns the issue with id.
func (r *NewsletterRepository) Issue(ctx context.Context, id uuid.UUID) (domain.Issue, error) {
	issues, err := r.issues(ctx, newsletterIssueSelect+` WHERE i.uuid = ?`, id)
	if err != nil {
		return domain.Issue{}, err
	}

	if len(issues) == 0 {
		return domain.Issue{}, domain.ErrNotFound
	}

	return issues[0], nil
}

// ListIssues returns one page of issues, newest first.
func (r *NewsletterRepository) ListIssues(ctx context.Context, filter domain.IssueFilter) (domain.IssuePage, error) {
	clause, args := "", []any{}
	if filter.Status != "" {
		clause, args = ` WHERE i.status = ?`, append(args, string(filter.Status))
	}

	var total int64
	if err := conn(ctx, r.db).Raw(`SELECT count(*) FROM newsletter_issues i`+clause, args...).Scan(&total).Error; err != nil {
		return domain.IssuePage{}, fmt.Errorf("count newsletter issues: %w", err)
	}

	items, err := r.issues(ctx, newsletterIssueSelect+clause+` ORDER BY i.created_at DESC, i.id DESC LIMIT ? OFFSET ?`,
		append(args, filter.PerPage, (filter.Page-1)*filter.PerPage)...)
	if err != nil {
		return domain.IssuePage{}, err
	}

	return domain.IssuePage{Items: items, Page: filter.Page, PerPage: filter.PerPage, Total: total}, nil
}

// DueIssues returns scheduled issues due at now, oldest first.
func (r *NewsletterRepository) DueIssues(ctx context.Context, now time.Time, limit int) ([]domain.Issue, error) {
	return r.issues(ctx, newsletterIssueSelect+` WHERE i.status = 'scheduled' AND i.send_at <= ? ORDER BY i.send_at, i.id LIMIT ?`,
		now, limit)
}

// MoveIssue changes the status when it is one of from, stamping queued_at or started_at.
func (r *NewsletterRepository) MoveIssue(
	ctx context.Context, id uuid.UUID, from []domain.IssueStatus, to domain.IssueStatus, at time.Time,
) (bool, error) {
	statuses := make([]string, 0, len(from))
	for _, s := range from {
		statuses = append(statuses, string(s))
	}

	result := conn(ctx, r.db).Exec(`UPDATE newsletter_issues SET status = ?, updated_at = ?,
		queued_at = CASE WHEN ? = 'queued' THEN ? ELSE queued_at END,
		started_at = CASE WHEN ? = 'sending' THEN COALESCE(started_at, ?) ELSE started_at END
		WHERE uuid = ? AND status IN ?`,
		string(to), at, string(to), at, string(to), at, id, statuses)
	if result.Error != nil {
		return false, fmt.Errorf("move newsletter issue: %w", result.Error)
	}

	return result.RowsAffected == 1, nil
}

func (r *NewsletterRepository) issues(ctx context.Context, query string, args ...any) ([]domain.Issue, error) {
	var rows []newsletterIssueRow
	if err := conn(ctx, r.db).Raw(query, args...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("newsletter issues: %w", err)
	}

	if len(rows) == 0 {
		return nil, nil
	}

	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}

	var lists []struct {
		IssueID int64
		Slug    string
	}

	err := conn(ctx, r.db).Raw(`SELECT il.issue_id, l.slug FROM newsletter_issue_lists il
		JOIN newsletter_lists l ON l.id = il.list_id WHERE il.issue_id IN ? ORDER BY l.position, l.slug`, ids).Scan(&lists).Error
	if err != nil {
		return nil, fmt.Errorf("newsletter issue lists: %w", err)
	}

	byIssue := map[int64][]string{}
	for _, l := range lists {
		byIssue[l.IssueID] = append(byIssue[l.IssueID], l.Slug)
	}

	out := make([]domain.Issue, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.Issue{
			UUID: row.UUID, Subject: row.Subject, Preheader: row.Preheader, BodyMarkdown: row.BodyMarkdown,
			Lists: byIssue[row.ID], Status: domain.IssueStatus(row.Status), SendAt: row.SendAt, QueuedAt: row.QueuedAt,
			StartedAt: row.StartedAt, CompletedAt: row.CompletedAt, SentCount: row.SentCount, FailedCount: row.FailedCount,
			CreatedBy: row.CreatedByUUID, UpdatedBy: row.UpdatedByUUID, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		})
	}

	return out, nil
}

// recipientCandidates are the active subscribers of the issue's lists still owed a delivery.
// SKIP LOCKED keeps concurrent claimers on disjoint subscribers.
const newsletterClaimSQL = `WITH issue AS (SELECT id FROM newsletter_issues WHERE uuid = @issue),
candidates AS (
	SELECT s.id FROM newsletter_subscribers s
	WHERE s.status = 'active'
		AND EXISTS (SELECT 1 FROM newsletter_list_memberships m
			JOIN newsletter_issue_lists il ON il.list_id = m.list_id
			WHERE m.subscriber_id = s.id AND m.state = 'active' AND il.issue_id = (SELECT id FROM issue))
		AND NOT EXISTS (SELECT 1 FROM newsletter_deliveries d
			WHERE d.issue_id = (SELECT id FROM issue) AND d.subscriber_id = s.id
				AND (d.status = 'sent' OR d.permanent OR d.attempts >= @max
					OR (d.status = 'sending' AND d.updated_at >= @stale)
					OR (d.status = 'failed' AND d.updated_at >= @retry)))
	ORDER BY s.id LIMIT @limit
	FOR UPDATE OF s SKIP LOCKED
)
INSERT INTO newsletter_deliveries (issue_id, subscriber_id, status, attempts, created_at, updated_at)
SELECT (SELECT id FROM issue), c.id, 'sending', 1, @now, @now FROM candidates c
ON CONFLICT (issue_id, subscriber_id) DO UPDATE
	SET status = 'sending', attempts = newsletter_deliveries.attempts + 1, last_error = NULL, updated_at = EXCLUDED.updated_at
	WHERE newsletter_deliveries.status <> 'sent' AND NOT newsletter_deliveries.permanent
		AND newsletter_deliveries.attempts < @max
		AND (newsletter_deliveries.status = 'failed' OR newsletter_deliveries.updated_at < @stale)
RETURNING uuid, subscriber_id, attempts`

// ClaimRecipients claims recipients for the issue as described by claim.
func (r *NewsletterRepository) ClaimRecipients(ctx context.Context, issueID uuid.UUID, claim domain.Claim) ([]domain.Recipient, error) {
	var claimed []struct {
		UUID         uuid.UUID
		SubscriberID int64
		Attempts     int
	}

	err := conn(ctx, r.db).Raw(newsletterClaimSQL, map[string]any{
		"issue": issueID, "max": claim.MaxAttempts, "stale": claim.StaleBefore, "retry": claim.RetryBefore,
		"limit": claim.Limit, "now": claim.Now,
	}).Scan(&claimed).Error
	if err != nil {
		return nil, fmt.Errorf("claim newsletter recipients: %w", err)
	}

	if len(claimed) == 0 {
		return nil, nil
	}

	ids := make([]int64, 0, len(claimed))
	for _, c := range claimed {
		ids = append(ids, c.SubscriberID)
	}

	var subs []struct {
		ID          int64
		UUID        uuid.UUID
		Email       string
		DisplayName *string
		Format      string
	}

	err = conn(ctx, r.db).Raw(`SELECT id, uuid, email, display_name, format FROM newsletter_subscribers WHERE id IN ?`, ids).
		Scan(&subs).Error
	if err != nil {
		return nil, fmt.Errorf("newsletter recipients: %w", err)
	}

	byID := make(map[int64]int, len(subs))
	for i, s := range subs {
		byID[s.ID] = i
	}

	out := make([]domain.Recipient, 0, len(claimed))

	for _, c := range claimed {
		i, ok := byID[c.SubscriberID]
		if !ok {
			continue
		}

		s := subs[i]
		out = append(out, domain.Recipient{
			DeliveryID: c.UUID, SubscriberID: s.UUID, Email: s.Email, Name: derefString(s.DisplayName),
			Format: domain.Format(s.Format), Attempts: c.Attempts,
		})
	}

	return out, nil
}

// RecordDelivery stores the outcome of one send.
func (r *NewsletterRepository) RecordDelivery(ctx context.Context, result domain.DeliveryResult) error {
	status := "failed"

	var sentAt *time.Time

	if result.Sent {
		status, sentAt = "sent", &result.At
	}

	err := conn(ctx, r.db).Exec(`UPDATE newsletter_deliveries SET status = ?, permanent = ?, last_error = ?, sent_at = ?,
		updated_at = ? WHERE uuid = ?`,
		status, result.Permanent, nullableString(result.Error), sentAt, result.At, result.DeliveryID).Error
	if err != nil {
		return fmt.Errorf("record newsletter delivery: %w", err)
	}

	return nil
}

// DeliveryCounts counts sent deliveries, final failures, and deliveries that can still be
// retried. A failed delivery to someone who has since unsubscribed or left the issue's lists
// is final: it will never be claimed again.
func (r *NewsletterRepository) DeliveryCounts(ctx context.Context, issueID uuid.UUID, maxAttempts int) (domain.DeliveryCounts, error) {
	var counts domain.DeliveryCounts

	err := conn(ctx, r.db).Raw(`WITH issue AS (SELECT id FROM newsletter_issues WHERE uuid = @issue),
		d AS (
			SELECT d.status, d.permanent, d.attempts,
				(s.status = 'active' AND EXISTS (SELECT 1 FROM newsletter_list_memberships m
					JOIN newsletter_issue_lists il ON il.list_id = m.list_id
					WHERE m.subscriber_id = s.id AND m.state = 'active' AND il.issue_id = d.issue_id)) AS reachable
			FROM newsletter_deliveries d JOIN newsletter_subscribers s ON s.id = d.subscriber_id
			WHERE d.issue_id = (SELECT id FROM issue)
		)
		SELECT
			count(*) FILTER (WHERE status = 'sent') AS sent,
			count(*) FILTER (WHERE status <> 'sent' AND (permanent OR attempts >= @max OR NOT reachable)) AS failed,
			count(*) FILTER (WHERE status <> 'sent' AND NOT permanent AND attempts < @max AND reachable) AS retryable
		FROM d`, map[string]any{"issue": issueID, "max": maxAttempts}).Scan(&counts).Error
	if err != nil {
		return domain.DeliveryCounts{}, fmt.Errorf("newsletter delivery counts: %w", err)
	}

	return counts, nil
}

// FinishIssue marks a sending issue sent.
func (r *NewsletterRepository) FinishIssue(ctx context.Context, id uuid.UUID, counts domain.DeliveryCounts, at time.Time) (bool, error) {
	result := conn(ctx, r.db).Exec(`UPDATE newsletter_issues SET status = 'sent', sent_count = ?, failed_count = ?,
		completed_at = ?, updated_at = ? WHERE uuid = ? AND status = 'sending'`, counts.Sent, counts.Failed, at, at, id)
	if result.Error != nil {
		return false, fmt.Errorf("finish newsletter issue: %w", result.Error)
	}

	return result.RowsAffected == 1, nil
}
