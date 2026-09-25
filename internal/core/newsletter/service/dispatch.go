package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/core/event"
	"github.com/turahe/blog-api/internal/core/newsletter/domain"
	"github.com/turahe/blog-api/internal/core/newsletter/ports"
)

// maxErrorLength bounds the provider error kept on a delivery.
const maxErrorLength = 500

// ReleaseDue queues up to limit scheduled issues whose send time has come and returns how many
// it queued.
func (s *Service) ReleaseDue(ctx context.Context, limit int) (int, error) {
	now := s.Clock.Now()

	due, err := s.Repo.DueIssues(ctx, now, limit)
	if err != nil {
		return 0, err
	}

	released := 0

	for _, issue := range due {
		err := s.Events.InTx(ctx, func(ctx context.Context) error {
			moved, err := s.Repo.MoveIssue(ctx, issue.UUID, []domain.IssueStatus{domain.IssueScheduled}, domain.IssueQueued, now)
			if err != nil || !moved {
				return err
			}

			released++
			issue.Status = domain.IssueQueued

			return s.queued(ctx, issue, nil, now)
		})
		if err != nil {
			return released, err
		}
	}

	return released, nil
}

// Dispatch delivers a queued or sending issue to every active subscriber of its lists. It is
// safe to run again or concurrently: each recipient is claimed before it is mailed, and sent
// recipients are skipped. It stops when the issue is cancelled. When deliveries failed and can
// be retried it returns an error wrapping domain.ErrDeliveriesPending so the consumer retries
// with backoff.
func (s *Service) Dispatch(ctx context.Context, issueID uuid.UUID) error {
	if s.Sender == nil {
		return domain.ErrNotConfigured
	}

	issue, err := s.Repo.Issue(ctx, issueID)
	if errors.Is(err, domain.ErrNotFound) {
		s.Logger.WarnContext(ctx, "newsletter: dispatch for a missing issue", "issue_id", issueID)
		return nil
	}

	if err != nil {
		return err
	}

	runStart := s.Clock.Now()

	if run, err := s.begin(ctx, issue, runStart); err != nil || !run {
		return err
	}

	cfg, err := s.Repo.Config(ctx)
	if err != nil {
		return err
	}

	body := ""
	if s.Markdown != nil {
		body = s.Markdown.Render(issue.BodyMarkdown)
	}

	if done, err := s.sendAll(ctx, issue, cfg, body, runStart); err != nil || !done {
		return err
	}

	return s.finish(ctx, issueID)
}

// begin moves a queued issue to sending and reports whether this run should deliver it.
func (s *Service) begin(ctx context.Context, issue domain.Issue, now time.Time) (bool, error) {
	switch issue.Status {
	case domain.IssueQueued:
		return s.Repo.MoveIssue(ctx, issue.UUID, []domain.IssueStatus{domain.IssueQueued}, domain.IssueSending, now)
	case domain.IssueSending:
		return true, nil
	case domain.IssueDraft, domain.IssueScheduled, domain.IssueSent, domain.IssueCancelled:
	}

	return false, nil
}

// sendAll mails claimed recipients batch by batch until none is left. It reports false when
// the issue stopped sending, for example because it was cancelled.
func (s *Service) sendAll(ctx context.Context, issue domain.Issue, cfg domain.Config, body string, runStart time.Time) (bool, error) {
	for {
		current, err := s.Repo.Issue(ctx, issue.UUID)
		if err != nil || current.Status != domain.IssueSending {
			return false, err
		}

		now := s.Clock.Now()

		recipients, err := s.Repo.ClaimRecipients(ctx, issue.UUID, domain.Claim{
			Limit: s.cfg.BatchSize, MaxAttempts: s.cfg.MaxAttempts, Now: now,
			StaleBefore: now.Add(-s.cfg.ClaimTimeout), RetryBefore: runStart,
		})
		if err != nil || len(recipients) == 0 {
			return err == nil, err
		}

		for _, r := range recipients {
			if err := s.Repo.RecordDelivery(ctx, s.deliver(ctx, issue, cfg, body, r)); err != nil {
				return false, err
			}
		}
	}
}

func (s *Service) finish(ctx context.Context, issueID uuid.UUID) error {
	counts, err := s.Repo.DeliveryCounts(ctx, issueID, s.cfg.MaxAttempts)
	if err != nil {
		return err
	}

	if counts.Retryable > 0 {
		return fmt.Errorf("%w: %d", domain.ErrDeliveriesPending, counts.Retryable)
	}

	if _, err := s.Repo.FinishIssue(ctx, issueID, counts, s.Clock.Now()); err != nil {
		return err
	}

	s.Logger.InfoContext(ctx, "newsletter issue sent", "issue_id", issueID, "sent", counts.Sent, "failed", counts.Failed)

	return nil
}

// deliver mails one recipient with fresh unsubscribe and preferences links.
func (s *Service) deliver(ctx context.Context, issue domain.Issue, cfg domain.Config, body string, r domain.Recipient) domain.DeliveryResult {
	now := s.Clock.Now()
	result := domain.DeliveryResult{DeliveryID: r.DeliveryID, At: now}
	expires := now.Add(s.cfg.LinkTTL)

	unsubscribe, err := s.issueToken(ctx, r.SubscriberID, domain.PurposeUnsubscribe, &issue.UUID, expires)
	if err != nil {
		result.Error = truncate(err.Error(), maxErrorLength)
		return result
	}

	preferences, err := s.issueToken(ctx, r.SubscriberID, domain.PurposePreferences, &issue.UUID, expires)
	if err != nil {
		result.Error = truncate(err.Error(), maxErrorLength)
		return result
	}

	email := s.compose(ctx, issue, cfg, body, r, links{Unsubscribe: unsubscribe, Preferences: preferences})

	if err := s.Sender.Send(ctx, email); err != nil {
		result.Permanent = errors.Is(err, domain.ErrPermanent)
		result.Error = truncate(err.Error(), maxErrorLength)

		return result
	}

	result.Sent = true

	return result
}

// SyncSubscriber pushes the subscriber's current state to the provider's contact store.
// Syncing current state, not the change, keeps retries and reordering harmless.
func (s *Service) SyncSubscriber(ctx context.Context, id uuid.UUID) error {
	if s.Contacts == nil {
		return nil
	}

	sub, err := s.Repo.Subscriber(ctx, id)
	if errors.Is(err, domain.ErrNotFound) {
		return nil
	}

	if err != nil {
		return err
	}

	return s.Contacts.SyncContact(ctx, ports.Contact{
		ID: sub.UUID, Email: sub.Email, Name: sub.DisplayName, Status: sub.Status, Format: sub.Format,
		Lists: sub.ActiveLists(), ChangedAt: sub.UpdatedAt,
	})
}

// HandleFeedback applies a provider bounce or complaint. Hard bounces and complaints suppress
// the address until its owner opts in again; soft bounces are only recorded.
func (s *Service) HandleFeedback(ctx context.Context, fb domain.ProviderFeedback) error {
	email, err := domain.NormalizeEmail(fb.Email)
	if err != nil {
		return err
	}

	switch fb.Kind {
	case domain.BounceHard, domain.BounceSoft, domain.Complaint:
	default:
		return domain.Invalid("type must be hard_bounce, soft_bounce, or complaint")
	}

	return s.Events.InTx(ctx, func(ctx context.Context) error {
		sub, found, err := s.byEmail(ctx, email)
		if err != nil || !found {
			return err
		}

		now := s.Clock.Now()
		source := domain.ConsentSourceProvider

		switch fb.Kind {
		case domain.BounceSoft:
			return s.Repo.AppendConsent(ctx, s.consent(sub, domain.ConsentSoftBounce, "", source, "", now))
		case domain.BounceHard:
			sub.Status, sub.BouncedAt = domain.StatusBounced, &now
			return s.suppress(ctx, &sub, domain.ConsentBounced, "bounced", now)
		case domain.Complaint:
			sub.Status, sub.ComplainedAt = domain.StatusComplained, &now
			return s.suppress(ctx, &sub, domain.ConsentComplained, "complained", now)
		}

		return nil
	})
}

func (s *Service) suppress(ctx context.Context, sub *domain.Subscriber, kind, change string, now time.Time) error {
	if err := s.Repo.RevokeTokens(ctx, sub.UUID, domain.PurposeConfirm); err != nil {
		return err
	}

	_, err := s.saveChange(ctx, sub, change, []domain.ConsentEvent{
		s.consent(*sub, kind, "", domain.ConsentSourceProvider, "", now),
	}, now)

	return err
}

// maxWebhookEvents bounds the events in one provider webhook.
const maxWebhookEvents = 100

// WebhookInput is a provider webhook request: the signature headers and the raw body.
type WebhookInput struct {
	Timestamp string
	Signature string
	Body      []byte
}

type webhookPayload struct {
	Events []struct {
		Type       string    `json:"type"`
		Email      string    `json:"email"`
		OccurredAt time.Time `json:"occurred_at"`
	} `json:"events"`
}

// ProviderWebhook verifies a signed batch of bounces and complaints and applies it. The whole
// batch is validated first so a bad event never leaves it half applied. It returns how many
// events it applied.
func (s *Service) ProviderWebhook(ctx context.Context, in WebhookInput) (int, error) {
	if s.Webhooks == nil {
		return 0, domain.ErrNotConfigured
	}

	if err := s.Webhooks.Verify(in.Timestamp, in.Signature, in.Body); err != nil {
		return 0, domain.ErrSignature
	}

	var payload webhookPayload
	if err := json.Unmarshal(in.Body, &payload); err != nil {
		return 0, domain.Invalid("body must be JSON with an events array")
	}

	if len(payload.Events) == 0 || len(payload.Events) > maxWebhookEvents {
		return 0, domain.Invalid("events must have between 1 and %d entries", maxWebhookEvents)
	}

	feedback := make([]domain.ProviderFeedback, 0, len(payload.Events))

	for i, e := range payload.Events {
		kind := domain.BounceKind(e.Type)
		if kind != domain.BounceHard && kind != domain.BounceSoft && kind != domain.Complaint {
			return 0, domain.Invalid("events[%d].type must be hard_bounce, soft_bounce, or complaint", i)
		}

		if _, err := domain.NormalizeEmail(e.Email); err != nil {
			return 0, domain.Invalid("events[%d].email is not a valid address", i)
		}

		feedback = append(feedback, domain.ProviderFeedback{Kind: kind, Email: e.Email, OccurredAt: e.OccurredAt})
	}

	for i, fb := range feedback {
		if err := s.HandleFeedback(ctx, fb); err != nil {
			return i, err
		}
	}

	return len(feedback), nil
}

// PruneTokens deletes tokens that expired before now minus retention.
func (s *Service) PruneTokens(ctx context.Context, retention time.Duration) (int64, error) {
	return s.Repo.PruneTokens(ctx, s.Clock.Now().Add(-retention))
}

// subscriberEvent announces a subscriber state change. It carries ids only; consumers read the
// current state, so no address travels through the broker.
func subscriberEvent(sub domain.Subscriber, change string, actor *uuid.UUID, now time.Time) event.Event {
	return event.New(event.NewsletterSubscriberChanged, event.AggregateNewsletterSubscriber, sub.UUID, actor, now,
		map[string]any{
			"subscriber_id": sub.UUID.String(), "status": string(sub.Status), "change": change, "occurred_at": now.UTC(),
		})
}
