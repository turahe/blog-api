// Package ports declares what the newsletter core needs from storage, mail, and providers.
package ports

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/core/newsletter/domain"
)

// Repository stores lists, subscribers, tokens, consent history, issues, and deliveries.
type Repository interface {
	Lists(ctx context.Context, includeArchived bool) ([]domain.List, error)
	// SaveLists upserts lists by slug and archives the active lists not in the set.
	SaveLists(ctx context.Context, lists []domain.ListInput, at time.Time) error
	// Config returns the stored settings, or domain.DefaultConfig when none were saved.
	Config(ctx context.Context) (domain.Config, error)
	SaveConfig(ctx context.Context, cfg domain.Config) error

	Subscriber(ctx context.Context, id uuid.UUID) (domain.Subscriber, error)
	SubscriberByEmail(ctx context.Context, normalized string) (domain.Subscriber, error)
	SubscriberByUser(ctx context.Context, userID uuid.UUID) (domain.Subscriber, error)
	// CreateSubscriber inserts s and its memberships; a taken email is domain.ErrConflict.
	CreateSubscriber(ctx context.Context, s domain.Subscriber) error
	// UpdateSubscriber saves the subscriber columns and, when modified, its memberships.
	UpdateSubscriber(ctx context.Context, s domain.Subscriber) error
	ListSubscribers(ctx context.Context, filter domain.SubscriberFilter) (domain.SubscriberPage, error)
	// EraseSubscriber nulls the subscriber's personal data, feedback, and IP hashes, leaves
	// status erased, and deletes its tokens.
	EraseSubscriber(ctx context.Context, id uuid.UUID, at time.Time) error

	CreateToken(ctx context.Context, t domain.Token) error
	// Token returns the token with hash; the caller checks purpose and expiry.
	Token(ctx context.Context, hash string) (domain.Token, error)
	// UseToken marks an unused token used; false when it was already used.
	UseToken(ctx context.Context, id int64, at time.Time) (bool, error)
	RevokeTokens(ctx context.Context, subscriberID uuid.UUID, purpose domain.TokenPurpose) error
	PruneTokens(ctx context.Context, before time.Time) (int64, error)

	AppendConsent(ctx context.Context, events ...domain.ConsentEvent) error
	ConsentHistory(ctx context.Context, subscriberID uuid.UUID, limit int) ([]domain.ConsentEvent, error)

	CreateIssue(ctx context.Context, issue domain.Issue) error
	Issue(ctx context.Context, id uuid.UUID) (domain.Issue, error)
	// UpdateIssue saves content, audience, status, and schedule while the stored status is
	// still expected; false when it changed meanwhile.
	UpdateIssue(ctx context.Context, issue domain.Issue, expected domain.IssueStatus) (bool, error)
	ListIssues(ctx context.Context, filter domain.IssueFilter) (domain.IssuePage, error)
	// DueIssues returns scheduled issues whose send time is at or before now.
	DueIssues(ctx context.Context, now time.Time, limit int) ([]domain.Issue, error)
	// MoveIssue changes the status when it is one of from; false when it was not.
	MoveIssue(ctx context.Context, id uuid.UUID, from []domain.IssueStatus, to domain.IssueStatus, at time.Time) (bool, error)
	// ClaimRecipients claims active subscribers of the issue's lists for delivery. Claims are
	// exclusive across concurrent callers.
	ClaimRecipients(ctx context.Context, issueID uuid.UUID, claim domain.Claim) ([]domain.Recipient, error)
	RecordDelivery(ctx context.Context, result domain.DeliveryResult) error
	DeliveryCounts(ctx context.Context, issueID uuid.UUID, maxAttempts int) (domain.DeliveryCounts, error)
	// FinishIssue marks a sending issue sent with its counts; false when it was not sending.
	FinishIssue(ctx context.Context, id uuid.UUID, counts domain.DeliveryCounts, at time.Time) (bool, error)
}

// Email is one rendered newsletter message.
type Email struct {
	To      string
	Subject string
	Text    string
	// HTML is empty for plaintext subscribers.
	HTML     string
	FromName string
	// FromEmail overrides the transport's sender address when set.
	FromEmail string
	ReplyTo   string
	// Headers carries List-Unsubscribe and List-Unsubscribe-Post.
	Headers map[string]string
	// IdempotencyKey is the delivery id, stable across retries.
	IdempotencyKey string
}

// Sender delivers newsletter email. Wrap domain.ErrPermanent for addresses that will never
// be accepted; any other error is retried.
type Sender interface {
	Send(ctx context.Context, email Email) error
}

// Contact is the state a provider mirrors for one subscriber.
type Contact struct {
	ID        uuid.UUID
	Email     string
	Name      string
	Status    domain.Status
	Format    domain.Format
	Lists     []string
	ChangedAt time.Time
}

// ContactSync mirrors subscriber state to the provider's contact store.
type ContactSync interface {
	SyncContact(ctx context.Context, contact Contact) error
}

// WebhookVerifier authenticates provider webhooks; it returns domain.ErrSignature on failure.
type WebhookVerifier interface {
	Verify(timestamp, signature string, body []byte) error
}

// ConfirmEmail asks an address to confirm its subscription.
type ConfirmEmail struct {
	To        string
	Name      string
	Token     string
	ExpiresAt time.Time
	Lists     []string
}

// WelcomeEmail follows a confirmation and links to the preferences page.
type WelcomeEmail struct {
	To               string
	Name             string
	PreferencesToken string
	Lists            []string
}

// Mailer sends the transactional newsletter emails (confirmation and welcome).
type Mailer interface {
	SendConfirm(ctx context.Context, email ConfirmEmail) error
	SendWelcome(ctx context.Context, email WelcomeEmail) error
}

// Links builds public URLs for emails.
type Links interface {
	// SiteURL is the public site origin that hosts the newsletter pages.
	SiteURL(ctx context.Context) string
	// SiteName is shown in email footers.
	SiteName(ctx context.Context) string
	// APIURL is the API origin used for the one-click unsubscribe header.
	APIURL() string
}

// Markdown renders issue bodies to sanitized HTML.
type Markdown interface {
	Render(source string) string
}

// Captcha verifies the optional public subscribe challenge.
type Captcha interface {
	Verify(ctx context.Context, token, remoteIP string) (bool, error)
}

// IdentityHasher keys the hashes of client IPs kept as consent evidence, so a leaked table
// cannot be reversed by hashing the small IPv4 space.
type IdentityHasher interface {
	MAC(value string) string
}

// Account is the signed-in user's address.
type Account struct {
	Email         string
	Name          string
	EmailVerified bool
}

// Accounts looks up signed-in users.
type Accounts interface {
	Account(ctx context.Context, userID uuid.UUID) (Account, error)
}
