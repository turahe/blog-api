// Package domain holds newsletter lists, subscribers, consent events, and issues.
package domain

import (
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Permissions checked by the admin routes. Subscribing and managing one's own subscription
// are open to everyone (rate limited), so they have no permission key.
const (
	PermSubscribersRead   = "newsletter.subscribers.read"
	PermSubscribersExport = "newsletter.subscribers.export"
	PermSubscribersErase  = "newsletter.subscribers.erase"
	PermIssuesRead        = "newsletter.issues.read"
	PermIssuesEdit        = "newsletter.issues.edit"
	PermIssuesSend        = "newsletter.issues.send"
	PermConfigRead        = "newsletter.provider_config.read"
	PermConfigUpdate      = "newsletter.provider_config.update"
)

// Audit resource types.
const (
	ResourceSubscriber = "newsletter_subscriber"
	ResourceIssue      = "newsletter_issue"
)

// Newsletter errors.
var (
	ErrValidation = errors.New("newsletter validation failed")
	ErrNotFound   = errors.New("newsletter record not found")
	// ErrTokenInvalid is an unknown token or one for another purpose.
	ErrTokenInvalid = errors.New("newsletter token is invalid")
	ErrTokenExpired = errors.New("newsletter token has expired")
	ErrTokenUsed    = errors.New("newsletter token was already used")
	// ErrCaptcha means the public subscribe challenge failed.
	ErrCaptcha = errors.New("newsletter challenge failed")
	// ErrConflict is a state transition the record does not allow.
	ErrConflict = errors.New("newsletter state conflict")
	// ErrNotConfigured means sending needs provider settings that are missing.
	ErrNotConfigured = errors.New("newsletter sending is not configured")
	// ErrSignature is a provider webhook with a missing, stale, or wrong signature.
	ErrSignature = errors.New("newsletter webhook signature invalid")
	// ErrPermanent marks a delivery the provider will never accept (bad address, rejected).
	ErrPermanent = errors.New("newsletter delivery permanently rejected")
	// ErrDeliveriesPending means an issue still has retryable failed deliveries.
	ErrDeliveriesPending = errors.New("newsletter issue has deliveries to retry")
)

// Invalid wraps ErrValidation with a client-facing message.
func Invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrValidation, fmt.Sprintf(format, args...))
}

// Status is a subscriber's consent state.
type Status string

// Subscriber statuses. Only active subscribers receive issues.
const (
	StatusPending      Status = "pending_confirm"
	StatusActive       Status = "active"
	StatusUnsubscribed Status = "unsubscribed"
	StatusBounced      Status = "bounced"
	StatusComplained   Status = "complained"
	StatusErased       Status = "erased"
)

// Statuses lists the statuses in display order.
var Statuses = []Status{StatusPending, StatusActive, StatusUnsubscribed, StatusBounced, StatusComplained, StatusErased}

// Format is the body a subscriber receives.
type Format string

// Formats.
const (
	FormatHTML      Format = "html"
	FormatPlaintext Format = "plaintext"
)

// ParseFormat returns the format for value; empty is html.
func ParseFormat(value string) (Format, error) {
	switch Format(value) {
	case "", FormatHTML:
		return FormatHTML, nil
	case FormatPlaintext:
		return FormatPlaintext, nil
	default:
		return "", Invalid("format must be html or plaintext")
	}
}

// Source says how a subscriber was added.
type Source string

// Sources.
const (
	SourcePublic  Source = "public"
	SourceAccount Source = "account"
	SourceAdmin   Source = "admin"
)

// MembershipState is a subscriber's state on one list.
type MembershipState string

// Membership states.
const (
	MembershipPending MembershipState = "pending"
	MembershipActive  MembershipState = "active"
	MembershipLeft    MembershipState = "left"
)

// List is a named newsletter an audience opts into.
type List struct {
	UUID        uuid.UUID
	Slug        string
	Name        string
	Description string
	IsDefault   bool
	Position    int
	ArchivedAt  *time.Time
}

// ListInput is one list in a provider-config update.
type ListInput struct {
	Slug        string
	Name        string
	Description string
	IsDefault   bool
}

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// MaxLists bounds the lists in one request or configuration.
const MaxLists = 20

// ValidateLists checks a provider-config list set: unique valid slugs, names, and at least one
// default list so a public subscribe without lists has somewhere to go.
func ValidateLists(lists []ListInput) error {
	if len(lists) == 0 || len(lists) > MaxLists {
		return Invalid("lists must have between 1 and %d entries", MaxLists)
	}

	seen := map[string]bool{}
	defaults := 0

	for _, l := range lists {
		if !slugPattern.MatchString(l.Slug) || len(l.Slug) > 64 {
			return Invalid("list slug %q must be lowercase letters, digits, and dashes", l.Slug)
		}

		if seen[l.Slug] {
			return Invalid("list slug %q is repeated", l.Slug)
		}

		seen[l.Slug] = true

		if n := utf8.RuneCountInString(strings.TrimSpace(l.Name)); n == 0 || n > 100 {
			return Invalid("list %q needs a name of at most 100 characters", l.Slug)
		}

		if utf8.RuneCountInString(l.Description) > 500 {
			return Invalid("list %q description is longer than 500 characters", l.Slug)
		}

		if l.IsDefault {
			defaults++
		}
	}

	if defaults == 0 {
		return Invalid("at least one list must be a default list")
	}

	return nil
}

// Membership is a subscriber's state on a list.
type Membership struct {
	ListSlug string
	ListName string
	State    MembershipState
	JoinedAt *time.Time
	LeftAt   *time.Time
}

// Subscriber is one email address and its consent state.
type Subscriber struct {
	UUID                uuid.UUID
	Email               string
	DisplayName         string
	UserID              *uuid.UUID
	Status              Status
	Format              Format
	Source              Source
	IPHash              string
	UserAgent           string
	ConfirmSends        int
	ConfirmWindowStart  *time.Time
	OptedInAt           *time.Time
	UnsubscribedAt      *time.Time
	BouncedAt           *time.Time
	ComplainedAt        *time.Time
	ErasedAt            *time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
	Memberships         []Membership
	membershipsModified bool
}

// Membership returns the subscriber's membership of slug.
func (s Subscriber) Membership(slug string) (Membership, bool) {
	for _, m := range s.Memberships {
		if m.ListSlug == slug {
			return m, true
		}
	}

	return Membership{}, false
}

// SetMembership sets the state of slug, adding the membership when missing.
func (s *Subscriber) SetMembership(slug string, state MembershipState, at time.Time) {
	for i := range s.Memberships {
		m := &s.Memberships[i]
		if m.ListSlug != slug {
			continue
		}

		if m.State != state {
			m.State = state
			stamp(m, state, at)

			s.membershipsModified = true
		}

		return
	}

	m := Membership{ListSlug: slug, State: state}
	stamp(&m, state, at)
	s.Memberships = append(s.Memberships, m)
	s.membershipsModified = true
}

func stamp(m *Membership, state MembershipState, at time.Time) {
	switch state {
	case MembershipActive:
		m.JoinedAt, m.LeftAt = &at, nil
	case MembershipLeft:
		m.LeftAt = &at
	case MembershipPending:
		m.LeftAt = nil
	}
}

// ActiveLists returns the slugs of the lists the subscriber receives.
func (s Subscriber) ActiveLists() []string {
	var out []string

	for _, m := range s.Memberships {
		if m.State == MembershipActive {
			out = append(out, m.ListSlug)
		}
	}

	return out
}

// MembershipsModified reports whether SetMembership changed anything since load.
func (s Subscriber) MembershipsModified() bool { return s.membershipsModified }

// Suppressed reports whether the address must not be mailed without a fresh opt-in.
func (s Subscriber) Suppressed() bool {
	return s.Status == StatusBounced || s.Status == StatusComplained || s.Status == StatusErased
}

// NormalizeEmail validates an address and returns it trimmed and lowercased, the form used
// for uniqueness.
func NormalizeEmail(raw string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(raw))
	if email == "" || len(email) > 254 || strings.ContainsAny(email, "\r\n") {
		return "", Invalid("a valid email address is required")
	}

	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email {
		return "", Invalid("a valid email address is required")
	}

	return email, nil
}

// MaskEmail hides most of the local part: "ann@example.com" becomes "a**@example.com".
func MaskEmail(email string) string {
	local, domain, ok := strings.Cut(email, "@")
	if !ok || local == "" {
		return ""
	}

	first, _ := utf8.DecodeRuneInString(local)

	return string(first) + strings.Repeat("*", max(utf8.RuneCountInString(local)-1, 2)) + "@" + domain
}

// ConsentEvent kinds.
const (
	ConsentConfirmSent   = "confirm_sent"
	ConsentConfirmed     = "confirmed"
	ConsentSubscribed    = "subscribed"
	ConsentListJoined    = "list_joined"
	ConsentListLeft      = "list_left"
	ConsentUnsubscribed  = "unsubscribed"
	ConsentResubscribed  = "resubscribed"
	ConsentFormatChanged = "format_changed"
	ConsentBounced       = "bounced"
	ConsentSoftBounce    = "soft_bounced"
	ConsentComplained    = "complained"
	ConsentErased        = "erased"
)

// Consent sources, beyond the subscriber sources.
const (
	ConsentSourceToken    = "token"
	ConsentSourceProvider = "provider"
	// ConsentSourcePrivacy is the account owner's erasure request.
	ConsentSourcePrivacy = "privacy_request"
)

// ConsentEvent is one immutable record in a subscriber's consent history.
type ConsentEvent struct {
	SubscriberID uuid.UUID
	Event        string
	ListSlug     string
	Source       string
	ReasonCode   string
	Feedback     string
	IPHash       string
	OccurredAt   time.Time
}

// ReasonCodes are the accepted unsubscribe reason codes.
var ReasonCodes = []string{"too_frequent", "not_relevant", "never_signed_up", "other"}

// ValidateFeedback checks optional unsubscribe feedback.
func ValidateFeedback(reason, feedback string) error {
	if reason != "" && !slices.Contains(ReasonCodes, reason) {
		return Invalid("reason_code must be one of %s", strings.Join(ReasonCodes, ", "))
	}

	if utf8.RuneCountInString(feedback) > 1000 {
		return Invalid("feedback must be at most 1000 characters")
	}

	return nil
}

// TokenPurpose limits what a token can do.
type TokenPurpose string

// Token purposes.
const (
	PurposeConfirm     TokenPurpose = "confirm"
	PurposeUnsubscribe TokenPurpose = "unsubscribe"
	PurposePreferences TokenPurpose = "preferences"
)

// Token is a stored token; only the SHA-256 of the raw value is kept.
type Token struct {
	ID           int64
	SubscriberID uuid.UUID
	Purpose      TokenPurpose
	Hash         string
	IssueID      *uuid.UUID
	ExpiresAt    time.Time
	UsedAt       *time.Time
	CreatedAt    time.Time
}

// SubscriberFilter selects subscribers for the admin list.
type SubscriberFilter struct {
	Status  Status
	List    string
	Query   string
	Page    int
	PerPage int
}

// SubscriberPage is one page of subscribers.
type SubscriberPage struct {
	Items   []Subscriber
	Page    int
	PerPage int
	Total   int64
}

// Config holds the non-secret sending settings stored in the database.
type Config struct {
	FromName            string
	FromEmail           string
	ReplyTo             string
	PostalAddress       string
	ConfirmTTL          time.Duration
	DoubleOptInRequired bool
	UpdatedAt           *time.Time
	UpdatedBy           *uuid.UUID
}

// Confirmation TTL bounds.
const (
	DefaultConfirmTTL = 48 * time.Hour
	MinConfirmTTL     = time.Hour
	MaxConfirmTTL     = 7 * 24 * time.Hour
)

// DefaultConfig is the configuration before an admin saves one.
func DefaultConfig() Config {
	return Config{ConfirmTTL: DefaultConfirmTTL, DoubleOptInRequired: true}
}

// Validate checks the settings an admin saves.
func (c Config) Validate() error {
	if utf8.RuneCountInString(c.FromName) > 100 || strings.ContainsAny(c.FromName, "\r\n\"<>") {
		return Invalid("from_name must be at most 100 characters without quotes, angle brackets, or line breaks")
	}

	for field, value := range map[string]string{"from_email": c.FromEmail, "reply_to": c.ReplyTo} {
		if value == "" {
			continue
		}

		if _, err := NormalizeEmail(value); err != nil {
			return Invalid("%s must be a valid email address", field)
		}
	}

	if utf8.RuneCountInString(c.PostalAddress) > 500 {
		return Invalid("postal_address must be at most 500 characters")
	}

	if c.ConfirmTTL < MinConfirmTTL || c.ConfirmTTL > MaxConfirmTTL {
		return Invalid("confirm_ttl_hours must be between 1 and 168")
	}

	return nil
}

// ReadyToSend reports whether issues can go out: CAN-SPAM needs a postal address in every
// footer.
func (c Config) ReadyToSend() error {
	if strings.TrimSpace(c.PostalAddress) == "" {
		return fmt.Errorf("%w: set postal_address in the provider config first", ErrNotConfigured)
	}

	return nil
}
