package domain

import (
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// IssueStatus is where an issue is in its lifecycle.
type IssueStatus string

// Issue statuses. draft and scheduled issues can be edited; queued issues wait for the
// worker; sending issues are being delivered.
const (
	IssueDraft     IssueStatus = "draft"
	IssueScheduled IssueStatus = "scheduled"
	IssueQueued    IssueStatus = "queued"
	IssueSending   IssueStatus = "sending"
	IssueSent      IssueStatus = "sent"
	IssueCancelled IssueStatus = "cancelled"
)

// IssueStatuses lists the statuses in lifecycle order.
var IssueStatuses = []IssueStatus{IssueDraft, IssueScheduled, IssueQueued, IssueSending, IssueSent, IssueCancelled}

// transitions are the status changes an editor may request.
var transitions = map[IssueStatus][]IssueStatus{
	IssueDraft:     {IssueScheduled, IssueQueued, IssueCancelled},
	IssueScheduled: {IssueDraft, IssueScheduled, IssueQueued, IssueCancelled},
	IssueQueued:    {IssueCancelled},
	// Queuing a sending issue again resumes a dispatch that ran out of retries.
	IssueSending: {IssueQueued, IssueCancelled},
}

// CanMove reports whether an editor may move an issue from one status to another.
func CanMove(from, to IssueStatus) bool {
	return slices.Contains(transitions[from], to)
}

// Editable reports whether the issue's content and audience can still change.
func (s IssueStatus) Editable() bool {
	return s == IssueDraft || s == IssueScheduled
}

// Issue is one newsletter send.
type Issue struct {
	UUID         uuid.UUID
	Subject      string
	Preheader    string
	BodyMarkdown string
	Lists        []string
	Status       IssueStatus
	SendAt       *time.Time
	QueuedAt     *time.Time
	StartedAt    *time.Time
	CompletedAt  *time.Time
	SentCount    int
	FailedCount  int
	CreatedBy    *uuid.UUID
	UpdatedBy    *uuid.UUID
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Issue field bounds.
const (
	MaxSubjectLength = 200
	MaxBodyLength    = 200000
)

// Validate checks the content and audience.
func (i Issue) Validate() error {
	if n := utf8.RuneCountInString(strings.TrimSpace(i.Subject)); n == 0 || n > MaxSubjectLength ||
		strings.ContainsAny(i.Subject, "\r\n") {
		return Invalid("subject must be 1-%d characters on one line", MaxSubjectLength)
	}

	if utf8.RuneCountInString(i.Preheader) > MaxSubjectLength || strings.ContainsAny(i.Preheader, "\r\n") {
		return Invalid("preheader must be at most %d characters on one line", MaxSubjectLength)
	}

	if n := utf8.RuneCountInString(strings.TrimSpace(i.BodyMarkdown)); n == 0 || n > MaxBodyLength {
		return Invalid("body_markdown must be 1-%d characters", MaxBodyLength)
	}

	if len(i.Lists) == 0 || len(i.Lists) > MaxLists {
		return Invalid("lists must name between 1 and %d lists", MaxLists)
	}

	return nil
}

// IssueFilter selects issues for the admin list.
type IssueFilter struct {
	Status  IssueStatus
	Page    int
	PerPage int
}

// IssuePage is one page of issues.
type IssuePage struct {
	Items   []Issue
	Page    int
	PerPage int
	Total   int64
}

// Claim selects recipients: up to Limit subscribers with no sent delivery and no permanent
// failure, fewer than MaxAttempts attempts, no claim newer than StaleBefore, and no failure
// newer than RetryBefore (so one dispatch run does not retry its own failures).
type Claim struct {
	Limit       int
	MaxAttempts int
	Now         time.Time
	StaleBefore time.Time
	RetryBefore time.Time
}

// Recipient is a claimed delivery: one active subscriber of an issue's lists.
type Recipient struct {
	DeliveryID   uuid.UUID
	SubscriberID uuid.UUID
	Email        string
	Name         string
	Format       Format
	Attempts     int
}

// DeliveryResult is the outcome of one send.
type DeliveryResult struct {
	DeliveryID uuid.UUID
	Sent       bool
	Permanent  bool
	Error      string
	At         time.Time
}

// DeliveryCounts summarises an issue's deliveries.
type DeliveryCounts struct {
	Sent      int
	Failed    int
	Retryable int
}

// BounceKind is what a provider reported about an address.
type BounceKind string

// Provider feedback kinds.
const (
	BounceHard BounceKind = "hard_bounce"
	BounceSoft BounceKind = "soft_bounce"
	Complaint  BounceKind = "complaint"
)

// ProviderFeedback is one bounce or complaint reported by the provider.
type ProviderFeedback struct {
	Kind       BounceKind
	Email      string
	OccurredAt time.Time
}
