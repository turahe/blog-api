package service

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/core/audit"
	"github.com/turahe/blog-api/internal/core/event"
	"github.com/turahe/blog-api/internal/core/newsletter/domain"
)

// ProviderConfig is the stored sending settings and the lists.
type ProviderConfig struct {
	Config domain.Config
	Lists  []domain.List
}

// ProviderConfig returns the settings and every list, archived ones included.
func (s *Service) ProviderConfig(ctx context.Context) (ProviderConfig, error) {
	cfg, err := s.Repo.Config(ctx)
	if err != nil {
		return ProviderConfig{}, err
	}

	lists, err := s.Repo.Lists(ctx, true)
	if err != nil {
		return ProviderConfig{}, err
	}

	return ProviderConfig{Config: cfg, Lists: lists}, nil
}

// SaveProviderConfig replaces the settings and the list set. Lists missing from lists are
// archived: they keep their memberships and history but can no longer be joined or targeted.
func (s *Service) SaveProviderConfig(ctx context.Context, actor uuid.UUID, cfg domain.Config, lists []domain.ListInput) (ProviderConfig, error) {
	if err := cfg.Validate(); err != nil {
		return ProviderConfig{}, err
	}

	if err := domain.ValidateLists(lists); err != nil {
		return ProviderConfig{}, err
	}

	before, err := s.Repo.Config(ctx)
	if err != nil {
		return ProviderConfig{}, err
	}

	now := s.Clock.Now()
	cfg.UpdatedAt, cfg.UpdatedBy = &now, &actor

	err = s.Events.InTx(ctx, func(ctx context.Context) error {
		if err := s.Repo.SaveConfig(ctx, cfg); err != nil {
			return err
		}

		return s.Repo.SaveLists(ctx, lists, now)
	})
	if err != nil {
		return ProviderConfig{}, err
	}

	auditConfig(ctx, before, cfg, lists)

	return s.ProviderConfig(ctx)
}

func auditConfig(ctx context.Context, before, after domain.Config, lists []domain.ListInput) {
	changes := map[string][2]any{
		"from_name":             {before.FromName, after.FromName},
		"from_email":            {before.FromEmail, after.FromEmail},
		"reply_to":              {before.ReplyTo, after.ReplyTo},
		"postal_address":        {before.PostalAddress, after.PostalAddress},
		"confirm_ttl_hours":     {int(before.ConfirmTTL.Hours()), int(after.ConfirmTTL.Hours())},
		"double_optin_required": {before.DoubleOptInRequired, after.DoubleOptInRequired},
	}
	for field, pair := range changes {
		if pair[0] != pair[1] {
			audit.AddChange(ctx, field, pair[0], pair[1])
		}
	}

	slugs := make([]string, 0, len(lists))
	for _, l := range lists {
		slugs = append(slugs, l.Slug)
	}

	audit.AddMetadata(ctx, "lists", slugs)
}

// ListSubscribers returns one page of subscribers.
func (s *Service) ListSubscribers(ctx context.Context, filter domain.SubscriberFilter) (domain.SubscriberPage, error) {
	if filter.Status != "" && !validStatus(filter.Status) {
		return domain.SubscriberPage{}, domain.Invalid("unknown status %q", filter.Status)
	}

	filter.Page, filter.PerPage = clampPage(filter.Page, filter.PerPage)

	return s.Repo.ListSubscribers(ctx, filter)
}

// SubscriberDetail is a subscriber with its latest consent history.
type SubscriberDetail struct {
	Subscriber domain.Subscriber
	History    []domain.ConsentEvent
}

// Subscriber returns one subscriber and its latest consent events.
func (s *Service) Subscriber(ctx context.Context, id uuid.UUID) (SubscriberDetail, error) {
	sub, err := s.Repo.Subscriber(ctx, id)
	if err != nil {
		return SubscriberDetail{}, err
	}

	history, err := s.Repo.ConsentHistory(ctx, id, historyLimit)
	if err != nil {
		return SubscriberDetail{}, err
	}

	return SubscriberDetail{Subscriber: sub, History: history}, nil
}

// Delete modes.
const (
	DeleteUnsubscribe = "unsubscribe"
	DeleteHard        = "hard_delete"
)

// DeleteSubscriber unsubscribes the subscriber or, in hard_delete mode, erases its personal
// data while keeping the suppressed row and consent history.
func (s *Service) DeleteSubscriber(ctx context.Context, actor, id uuid.UUID, mode string) (SubscriberDetail, error) {
	if mode == "" {
		mode = DeleteUnsubscribe
	}

	if mode != DeleteUnsubscribe && mode != DeleteHard {
		return SubscriberDetail{}, domain.Invalid("mode must be unsubscribe or hard_delete")
	}

	audit.SetResource(ctx, domain.ResourceSubscriber, id)
	audit.AddMetadata(ctx, "mode", mode)

	err := s.Events.InTx(ctx, func(ctx context.Context) error {
		sub, err := s.Repo.Subscriber(ctx, id)
		if err != nil {
			return err
		}

		if sub.Status == domain.StatusErased {
			return nil
		}

		source := string(domain.SourceAdmin)
		if mode == DeleteUnsubscribe {
			return s.unsubscribeAll(ctx, &sub, source, "", "", "", &actor)
		}

		return s.erase(ctx, sub, source, &actor, s.Clock.Now())
	})
	if err != nil {
		return SubscriberDetail{}, err
	}

	return s.Subscriber(ctx, id)
}

// EraseUser erases the subscriber linked to the account, if any, as part of the account's
// erasure. It runs in the caller's transaction when there is one.
func (s *Service) EraseUser(ctx context.Context, userID uuid.UUID, at time.Time) error {
	return s.Events.InTx(ctx, func(ctx context.Context) error {
		sub, err := s.Repo.SubscriberByUser(ctx, userID)
		if errors.Is(err, domain.ErrNotFound) {
			return nil
		}

		if err != nil || sub.Status == domain.StatusErased {
			return err
		}

		return s.erase(ctx, sub, domain.ConsentSourcePrivacy, nil, at)
	})
}

// erase clears the subscriber's personal data and records the erasure in its consent history
// and as a change event.
func (s *Service) erase(ctx context.Context, sub domain.Subscriber, source string, actor *uuid.UUID, at time.Time) error {
	if err := s.Repo.EraseSubscriber(ctx, sub.UUID, at); err != nil {
		return err
	}

	if err := s.Repo.AppendConsent(ctx, s.consent(sub, domain.ConsentErased, "", source, "", at)); err != nil {
		return err
	}

	sub.Status = domain.StatusErased

	return s.Events.Record(ctx, subscriberEvent(sub, "erased", actor, at))
}

// IssueInput creates an issue. Status is draft (default), scheduled (needs SendAt), or queued
// (send now).
type IssueInput struct {
	Subject      string
	Preheader    string
	BodyMarkdown string
	Lists        []string
	Status       domain.IssueStatus
	SendAt       *time.Time
}

// CreateIssue stores a new issue and, when queued, hands it to the worker.
func (s *Service) CreateIssue(ctx context.Context, actor uuid.UUID, in IssueInput) (domain.Issue, error) {
	now := s.Clock.Now()
	issue := domain.Issue{
		UUID: s.IDs.New(), Subject: in.Subject, Preheader: in.Preheader, BodyMarkdown: in.BodyMarkdown,
		Status: domain.IssueDraft, CreatedBy: &actor, UpdatedBy: &actor, CreatedAt: now, UpdatedAt: now,
	}

	if in.Status == "" {
		in.Status = domain.IssueDraft
	}

	if in.Status != domain.IssueDraft && !domain.CanMove(domain.IssueDraft, in.Status) {
		return domain.Issue{}, domain.Invalid("status must be draft, scheduled, or queued")
	}

	lists, err := s.resolveExact(ctx, in.Lists)
	if err != nil {
		return domain.Issue{}, err
	}

	issue.Lists = lists
	if err := issue.Validate(); err != nil {
		return domain.Issue{}, err
	}

	if err := s.prepareMove(ctx, &issue, in.Status, in.SendAt, now); err != nil {
		return domain.Issue{}, err
	}

	audit.SetResource(ctx, domain.ResourceIssue, issue.UUID)
	audit.AddMetadata(ctx, "status", string(issue.Status))

	err = s.Events.InTx(ctx, func(ctx context.Context) error {
		if err := s.Repo.CreateIssue(ctx, issue); err != nil {
			return err
		}

		return s.queued(ctx, issue, &actor, now)
	})

	return issue, err
}

// IssuePatch edits an issue. Nil fields stay unchanged. Content and lists change only while
// the issue is draft or scheduled.
type IssuePatch struct {
	Subject      *string
	Preheader    *string
	BodyMarkdown *string
	Lists        *[]string
	SendAt       *time.Time
	Status       *domain.IssueStatus
}

// UpdateIssue applies patch. Status changes follow domain.CanMove; queuing a sending issue
// resumes a dispatch that stopped retrying.
func (s *Service) UpdateIssue(ctx context.Context, actor, id uuid.UUID, patch IssuePatch) (domain.Issue, error) {
	audit.SetResource(ctx, domain.ResourceIssue, id)

	issue, err := s.Repo.Issue(ctx, id)
	if err != nil {
		return domain.Issue{}, err
	}

	current := issue.Status
	now := s.Clock.Now()

	if err := s.applyContent(ctx, &issue, patch); err != nil {
		return domain.Issue{}, err
	}

	target := current
	if patch.Status != nil {
		target = *patch.Status
	}

	if patch.SendAt != nil && target != domain.IssueScheduled {
		return domain.Issue{}, domain.Invalid("send_at applies only to scheduled issues")
	}

	if target != current || patch.SendAt != nil {
		if !domain.CanMove(current, target) {
			return domain.Issue{}, domain.ErrConflict
		}

		if err := s.prepareMove(ctx, &issue, target, patch.SendAt, now); err != nil {
			return domain.Issue{}, err
		}

		audit.AddChange(ctx, "status", string(current), string(target))
	}

	issue.UpdatedBy, issue.UpdatedAt = &actor, now

	if err := s.Events.InTx(ctx, func(ctx context.Context) error {
		return s.saveIssue(ctx, issue, current, actor, now)
	}); err != nil {
		return domain.Issue{}, err
	}

	return s.Repo.Issue(ctx, id)
}

// saveIssue writes an edited issue, or only its status once it left draft and scheduled,
// guarded by the status it was read with; a status change to queued hands it to the worker.
func (s *Service) saveIssue(ctx context.Context, issue domain.Issue, current domain.IssueStatus, actor uuid.UUID, now time.Time) error {
	var (
		saved bool
		err   error
	)

	if current.Editable() {
		saved, err = s.Repo.UpdateIssue(ctx, issue, current)
	} else {
		saved, err = s.Repo.MoveIssue(ctx, issue.UUID, []domain.IssueStatus{current}, issue.Status, now)
	}

	if err != nil {
		return err
	}

	if !saved {
		return domain.ErrConflict
	}

	if issue.Status == current {
		return nil
	}

	return s.queued(ctx, issue, &actor, now)
}

func (s *Service) applyContent(ctx context.Context, issue *domain.Issue, patch IssuePatch) error {
	edits := patch.Subject != nil || patch.Preheader != nil || patch.BodyMarkdown != nil || patch.Lists != nil
	if !edits {
		return nil
	}

	if !issue.Status.Editable() {
		return domain.ErrConflict
	}

	if patch.Subject != nil {
		issue.Subject = *patch.Subject
	}

	if patch.Preheader != nil {
		issue.Preheader = *patch.Preheader
	}

	if patch.BodyMarkdown != nil {
		issue.BodyMarkdown = *patch.BodyMarkdown
	}

	if patch.Lists != nil {
		lists, err := s.resolveExact(ctx, *patch.Lists)
		if err != nil {
			return err
		}

		issue.Lists = lists
	}

	return issue.Validate()
}

// prepareMove sets the fields for moving issue to status and checks its preconditions.
func (s *Service) prepareMove(ctx context.Context, issue *domain.Issue, status domain.IssueStatus, sendAt *time.Time, now time.Time) error {
	switch status {
	case domain.IssueScheduled:
		if sendAt == nil {
			sendAt = issue.SendAt
		}

		if sendAt == nil || !sendAt.After(now) {
			return domain.Invalid("send_at must be in the future to schedule an issue")
		}

		issue.SendAt = sendAt
	case domain.IssueQueued:
		issue.QueuedAt = &now
	case domain.IssueDraft:
		issue.SendAt = nil
	case domain.IssueSending, domain.IssueSent, domain.IssueCancelled:
	}

	if status == domain.IssueScheduled || status == domain.IssueQueued {
		cfg, err := s.Repo.Config(ctx)
		if err != nil {
			return err
		}

		if err := cfg.ReadyToSend(); err != nil {
			return err
		}
	}

	issue.Status = status

	return nil
}

// queued records the send request for an issue that just became queued.
func (s *Service) queued(ctx context.Context, issue domain.Issue, actor *uuid.UUID, now time.Time) error {
	if issue.Status != domain.IssueQueued {
		return nil
	}

	return s.Events.Record(ctx, event.New(event.NewsletterIssueSendRequested, event.AggregateNewsletterIssue,
		issue.UUID, actor, now, map[string]any{"issue_id": issue.UUID.String(), "requested_at": now.UTC()}))
}

// Issue returns one issue.
func (s *Service) Issue(ctx context.Context, id uuid.UUID) (domain.Issue, error) {
	return s.Repo.Issue(ctx, id)
}

// ListIssues returns one page of issues.
func (s *Service) ListIssues(ctx context.Context, filter domain.IssueFilter) (domain.IssuePage, error) {
	if filter.Status != "" && !validIssueStatus(filter.Status) {
		return domain.IssuePage{}, domain.Invalid("unknown status %q", filter.Status)
	}

	filter.Page, filter.PerPage = clampPage(filter.Page, filter.PerPage)

	return s.Repo.ListIssues(ctx, filter)
}

func validStatus(status domain.Status) bool { return slices.Contains(domain.Statuses, status) }

func validIssueStatus(status domain.IssueStatus) bool {
	return slices.Contains(domain.IssueStatuses, status)
}

// Page size bounds for admin lists.
const (
	defaultPerPage = 20
	maxPerPage     = 100
)

func clampPage(page, perPage int) (int, int) {
	if page < 1 {
		page = 1
	}

	if perPage < 1 {
		perPage = defaultPerPage
	}

	return page, min(perPage, maxPerPage)
}
