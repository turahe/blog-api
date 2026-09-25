// Package service implements newsletter subscriptions with double opt-in, token-based
// preferences and unsubscribe, admin management, and issue dispatch through a provider.
package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/core/event"
	"github.com/turahe/blog-api/internal/core/newsletter/domain"
	"github.com/turahe/blog-api/internal/core/newsletter/ports"
)

// IDGenerator returns new UUIDs.
type IDGenerator interface {
	New() uuid.UUID
}

// Clock returns the current time.
type Clock interface {
	Now() time.Time
}

// Config tunes the service.
type Config struct {
	// BatchSize is how many recipients one dispatch step claims.
	BatchSize int
	// MaxAttempts bounds sends per recipient before the delivery counts as failed.
	MaxAttempts int
	// ClaimTimeout is how long a claim stays exclusive before another dispatch may retake it.
	ClaimTimeout time.Duration
	// LinkTTL is how long unsubscribe and preferences links keep working.
	LinkTTL time.Duration
}

// Defaults for Config fields left zero.
const (
	DefaultBatchSize    = 50
	DefaultMaxAttempts  = 3
	DefaultClaimTimeout = 10 * time.Minute
	// DefaultLinkTTL covers 30 days after a send plus 7 days of grace.
	DefaultLinkTTL = 37 * 24 * time.Hour

	// confirmWindow and confirmPerWindow cap confirmation emails per address.
	confirmWindow    = 30 * time.Minute
	confirmPerWindow = 3
	maxTokenLength   = 128
	maxNameLength    = 100
	historyLimit     = 100
)

// Deps are the collaborators. Repo, IDs, and Clock are required; a nil Mailer skips
// confirmation emails, a nil Sender disables dispatch, a nil Webhooks rejects provider
// webhooks, and nil Contacts and Captcha are skipped.
type Deps struct {
	Repo     ports.Repository
	IDs      IDGenerator
	Clock    Clock
	Mailer   ports.Mailer
	Links    ports.Links
	Markdown ports.Markdown
	Sender   ports.Sender
	Contacts ports.ContactSync
	Webhooks ports.WebhookVerifier
	Captcha  ports.Captcha
	Accounts ports.Accounts
	Events   event.Unit
	Logger   *slog.Logger
	// IdentityHasher keys stored IP hashes; nil falls back to plain SHA-256.
	IdentityHasher ports.IdentityHasher
}

// Service implements the newsletter use cases.
type Service struct {
	Deps

	cfg Config
}

// New returns a Service.
func New(deps Deps, cfg Config) *Service {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}

	if cfg.BatchSize <= 0 {
		cfg.BatchSize = DefaultBatchSize
	}

	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = DefaultMaxAttempts
	}

	if cfg.ClaimTimeout <= 0 {
		cfg.ClaimTimeout = DefaultClaimTimeout
	}

	if cfg.LinkTTL <= 0 {
		cfg.LinkTTL = DefaultLinkTTL
	}

	return &Service{Deps: deps, cfg: cfg}
}

// SendingEnabled reports whether issues can be dispatched.
func (s *Service) SendingEnabled() bool { return s.Sender != nil }

// SubscribeInput is a public subscribe request.
type SubscribeInput struct {
	Email        string
	Name         string
	Lists        []string
	Format       string
	Honeypot     string
	CaptchaToken string
	IP           string
	UserAgent    string
}

// Subscribe records a double opt-in request and emails a confirmation link. The result never
// says whether the address was already known: new, pending, active, and suppressed addresses
// all return nil.
func (s *Service) Subscribe(ctx context.Context, in SubscribeInput) error {
	if in.Honeypot != "" {
		return nil
	}

	email, err := domain.NormalizeEmail(in.Email)
	if err != nil {
		return err
	}

	name, err := cleanName(in.Name)
	if err != nil {
		return err
	}

	format, err := domain.ParseFormat(in.Format)
	if err != nil {
		return err
	}

	if err := s.verifyCaptcha(ctx, in.CaptchaToken, in.IP); err != nil {
		return err
	}

	lists, err := s.resolveLists(ctx, in.Lists)
	if err != nil {
		return err
	}

	cfg, err := s.Repo.Config(ctx)
	if err != nil {
		return err
	}

	var confirm *ports.ConfirmEmail

	err = s.Events.InTx(ctx, func(ctx context.Context) error {
		sub, found, err := s.byEmail(ctx, email)
		if err != nil {
			return err
		}

		if !found {
			sub = s.newSubscriber(email, name, format, domain.SourcePublic, s.hashIdentity(in.IP), in.UserAgent)
		}

		if sub.Suppressed() {
			return nil
		}

		confirm, err = s.requestConfirmation(ctx, &sub, lists, cfg, !found, s.hashIdentity(in.IP), string(domain.SourcePublic))

		return err
	})
	if err != nil {
		return err
	}

	s.sendConfirm(ctx, confirm)

	return nil
}

// requestConfirmation marks lists pending, saves sub, and issues a confirmation token unless
// every list is already active or the address hit its confirmation limit. It returns the email
// to send after commit, or nil.
func (s *Service) requestConfirmation(
	ctx context.Context, sub *domain.Subscriber, lists []domain.List, cfg domain.Config, create bool, ipHash, source string,
) (*ports.ConfirmEmail, error) {
	now := s.Clock.Now()

	for _, l := range lists {
		if m, ok := sub.Membership(l.Slug); !ok || m.State != domain.MembershipActive {
			sub.SetMembership(l.Slug, domain.MembershipPending, now)
		}
	}

	pending := pendingLists(*sub)
	send := len(pending) > 0 && allowConfirm(sub, now)
	sub.UpdatedAt = now

	if create {
		err := s.Repo.CreateSubscriber(ctx, *sub)
		if errors.Is(err, domain.ErrConflict) {
			// A concurrent request created the address; answering the same way hides that.
			return nil, nil
		}

		if err != nil {
			return nil, err
		}
	} else if err := s.Repo.UpdateSubscriber(ctx, *sub); err != nil {
		return nil, err
	}

	if !send {
		return nil, nil
	}

	if err := s.Repo.RevokeTokens(ctx, sub.UUID, domain.PurposeConfirm); err != nil {
		return nil, err
	}

	raw, err := s.issueToken(ctx, sub.UUID, domain.PurposeConfirm, nil, now.Add(cfg.ConfirmTTL))
	if err != nil {
		return nil, err
	}

	if err := s.Repo.AppendConsent(ctx, s.consent(*sub, domain.ConsentConfirmSent, "", source, ipHash, now)); err != nil {
		return nil, err
	}

	return &ports.ConfirmEmail{
		To: sub.Email, Name: sub.DisplayName, Token: raw, ExpiresAt: now.Add(cfg.ConfirmTTL), Lists: s.listNames(ctx, pending),
	}, nil
}

// Confirm consumes a confirmation token and activates the pending lists.
func (s *Service) Confirm(ctx context.Context, raw, ip string) (domain.Subscriber, error) {
	token, err := s.lookupToken(ctx, raw, domain.PurposeConfirm)
	if err != nil {
		return domain.Subscriber{}, err
	}

	if token.UsedAt != nil {
		return domain.Subscriber{}, domain.ErrTokenUsed
	}

	var (
		sub     domain.Subscriber
		welcome *ports.WelcomeEmail
	)

	err = s.Events.InTx(ctx, func(ctx context.Context) error {
		now := s.Clock.Now()

		used, err := s.Repo.UseToken(ctx, token.ID, now)
		if err != nil {
			return err
		}

		if !used {
			return domain.ErrTokenUsed
		}

		if sub, err = s.Repo.Subscriber(ctx, token.SubscriberID); err != nil {
			return err
		}

		if sub.Status == domain.StatusErased {
			return domain.ErrTokenInvalid
		}

		joined := pendingLists(sub)
		for _, slug := range joined {
			sub.SetMembership(slug, domain.MembershipActive, now)
		}

		events := []domain.ConsentEvent{s.consent(sub, domain.ConsentConfirmed, "", domain.ConsentSourceToken, s.hashIdentity(ip), now)}
		events = append(events, s.listEvents(sub, domain.ConsentListJoined, joined, domain.ConsentSourceToken, now)...)

		if sub.Status != domain.StatusActive {
			activate(&sub, now)
		}

		welcome, err = s.saveChange(ctx, &sub, "confirmed", events, now)

		return err
	})
	if err != nil {
		return domain.Subscriber{}, err
	}

	s.sendWelcome(ctx, welcome)

	return sub, nil
}

func (s *Service) sendWelcome(ctx context.Context, welcome *ports.WelcomeEmail) {
	if welcome == nil || s.Mailer == nil {
		return
	}

	if err := s.Mailer.SendWelcome(ctx, *welcome); err != nil {
		s.Logger.WarnContext(ctx, "newsletter: welcome email failed", "error", err)
	}
}

// saveChange stores sub, appends events, records a subscriber-changed event, and, for a
// confirmation, prepares the welcome email with a preferences link.
func (s *Service) saveChange(
	ctx context.Context, sub *domain.Subscriber, change string, events []domain.ConsentEvent, now time.Time,
) (*ports.WelcomeEmail, error) {
	sub.UpdatedAt = now
	if err := s.Repo.UpdateSubscriber(ctx, *sub); err != nil {
		return nil, err
	}

	if err := s.Repo.AppendConsent(ctx, events...); err != nil {
		return nil, err
	}

	if err := s.Events.Record(ctx, subscriberEvent(*sub, change, nil, now)); err != nil {
		return nil, err
	}

	if change != "confirmed" {
		return nil, nil
	}

	raw, err := s.issueToken(ctx, sub.UUID, domain.PurposePreferences, nil, now.Add(s.cfg.LinkTTL))
	if err != nil {
		return nil, err
	}

	return &ports.WelcomeEmail{
		To: sub.Email, Name: sub.DisplayName, PreferencesToken: raw, Lists: s.listNames(ctx, sub.ActiveLists()),
	}, nil
}

// ResendConfirm emails a new confirmation link to an address with pending lists. Like
// Subscribe, it returns nil whether or not the address is known.
func (s *Service) ResendConfirm(ctx context.Context, rawEmail, ip string) error {
	email, err := domain.NormalizeEmail(rawEmail)
	if err != nil {
		return err
	}

	cfg, err := s.Repo.Config(ctx)
	if err != nil {
		return err
	}

	var confirm *ports.ConfirmEmail

	err = s.Events.InTx(ctx, func(ctx context.Context) error {
		sub, found, err := s.byEmail(ctx, email)
		if err != nil || !found || sub.Suppressed() || len(pendingLists(sub)) == 0 {
			return err
		}

		confirm, err = s.requestConfirmation(ctx, &sub, nil, cfg, false, s.hashIdentity(ip), string(domain.SourcePublic))

		return err
	})
	if err != nil {
		return err
	}

	s.sendConfirm(ctx, confirm)

	return nil
}

// UnsubscribeInput is an unsubscribe by token, from an email footer or the one-click header.
type UnsubscribeInput struct {
	Token      string
	ReasonCode string
	Feedback   string
	IP         string
}

// Unsubscribe stops all mail to the token's subscriber. Repeating it is harmless.
func (s *Service) Unsubscribe(ctx context.Context, in UnsubscribeInput) error {
	if err := domain.ValidateFeedback(in.ReasonCode, in.Feedback); err != nil {
		return err
	}

	token, err := s.lookupToken(ctx, in.Token, domain.PurposeUnsubscribe)
	if err != nil {
		return err
	}

	return s.Events.InTx(ctx, func(ctx context.Context) error {
		sub, err := s.Repo.Subscriber(ctx, token.SubscriberID)
		if err != nil {
			return err
		}

		if sub.Status == domain.StatusErased {
			return domain.ErrTokenInvalid
		}

		return s.unsubscribeAll(ctx, &sub, domain.ConsentSourceToken, in.ReasonCode, in.Feedback, s.hashIdentity(in.IP), nil)
	})
}

// unsubscribeAll leaves every list and, unless already suppressed, sets status unsubscribed.
func (s *Service) unsubscribeAll(
	ctx context.Context, sub *domain.Subscriber, source, reason, feedback, ipHash string, actor *uuid.UUID,
) error {
	now := s.Clock.Now()

	for _, m := range sub.Memberships {
		if m.State != domain.MembershipLeft {
			sub.SetMembership(m.ListSlug, domain.MembershipLeft, now)
		}
	}

	changedStatus := sub.Status == domain.StatusActive || sub.Status == domain.StatusPending
	if changedStatus {
		sub.Status, sub.UnsubscribedAt = domain.StatusUnsubscribed, &now
	}

	if !changedStatus && !sub.MembershipsModified() && reason == "" && feedback == "" {
		return nil
	}

	sub.UpdatedAt = now
	if err := s.Repo.UpdateSubscriber(ctx, *sub); err != nil {
		return err
	}

	if err := s.Repo.RevokeTokens(ctx, sub.UUID, domain.PurposeConfirm); err != nil {
		return err
	}

	event := s.consent(*sub, domain.ConsentUnsubscribed, "", source, ipHash, now)
	event.ReasonCode, event.Feedback = reason, feedback

	if err := s.Repo.AppendConsent(ctx, event); err != nil {
		return err
	}

	return s.Events.Record(ctx, subscriberEvent(*sub, "unsubscribed", actor, now))
}

// Preferences is what the preferences page shows.
type Preferences struct {
	Subscriber domain.Subscriber
	Lists      []domain.List
}

// Preferences returns the subscriber behind a preferences token.
func (s *Service) Preferences(ctx context.Context, raw string) (Preferences, error) {
	token, err := s.lookupToken(ctx, raw, domain.PurposePreferences)
	if err != nil {
		return Preferences{}, err
	}

	return s.preferences(ctx, token.SubscriberID)
}

func (s *Service) preferences(ctx context.Context, id uuid.UUID) (Preferences, error) {
	sub, err := s.Repo.Subscriber(ctx, id)
	if err != nil {
		return Preferences{}, err
	}

	if sub.Status == domain.StatusErased {
		return Preferences{}, domain.ErrTokenInvalid
	}

	lists, err := s.Repo.Lists(ctx, false)
	if err != nil {
		return Preferences{}, err
	}

	return Preferences{Subscriber: sub, Lists: lists}, nil
}

// PreferencesInput changes a subscription by preferences token. Nil fields stay unchanged;
// Lists is the complete set of lists to receive, and an empty set unsubscribes.
type PreferencesInput struct {
	Format         *string
	Lists          *[]string
	UnsubscribeAll bool
	IP             string
}

// UpdatePreferences applies in. The token proves the subscriber controls the inbox, so lists
// chosen here are active at once, including after an earlier unsubscribe.
func (s *Service) UpdatePreferences(ctx context.Context, raw string, in PreferencesInput) (Preferences, error) {
	token, err := s.lookupToken(ctx, raw, domain.PurposePreferences)
	if err != nil {
		return Preferences{}, err
	}

	err = s.Events.InTx(ctx, func(ctx context.Context) error {
		sub, err := s.Repo.Subscriber(ctx, token.SubscriberID)
		if err != nil {
			return err
		}

		if sub.Status == domain.StatusErased {
			return domain.ErrTokenInvalid
		}

		ipHash := s.hashIdentity(in.IP)
		if in.UnsubscribeAll {
			return s.unsubscribeAll(ctx, &sub, domain.ConsentSourceToken, "", "", ipHash, nil)
		}

		return s.applyChoices(ctx, &sub, in.Format, in.Lists, domain.ConsentSourceToken, ipHash, nil)
	})
	if err != nil {
		return Preferences{}, err
	}

	return s.preferences(ctx, token.SubscriberID)
}

// applyChoices sets format and the exact list set chosen by someone who proved control of the
// address, activating or unsubscribing the subscriber as needed.
func (s *Service) applyChoices(
	ctx context.Context, sub *domain.Subscriber, format *string, lists *[]string, source, ipHash string, actor *uuid.UUID,
) error {
	now := s.Clock.Now()

	var events []domain.ConsentEvent

	if format != nil {
		f, err := domain.ParseFormat(*format)
		if err != nil {
			return err
		}

		if f != sub.Format {
			sub.Format = f
			events = append(events, s.consent(*sub, domain.ConsentFormatChanged, "", source, ipHash, now))
		}
	}

	change := "preferences_updated"

	if lists != nil {
		wanted, err := s.resolveExact(ctx, *lists)
		if err != nil {
			return err
		}

		joined, left := setLists(sub, wanted, now)
		events = append(events, s.listEvents(*sub, domain.ConsentListJoined, joined, source, now)...)
		events = append(events, s.listEvents(*sub, domain.ConsentListLeft, left, source, now)...)

		switch {
		case len(wanted) > 0 && sub.Status != domain.StatusActive:
			activate(sub, now)
			events = append(events, s.consent(*sub, domain.ConsentResubscribed, "", source, ipHash, now))
			change = "resubscribed"
		case len(wanted) == 0 && sub.Status == domain.StatusActive:
			sub.Status, sub.UnsubscribedAt = domain.StatusUnsubscribed, &now
			events = append(events, s.consent(*sub, domain.ConsentUnsubscribed, "", source, ipHash, now))
			change = "unsubscribed"
		}
	}

	if len(events) == 0 {
		return nil
	}

	sub.UpdatedAt = now
	if err := s.Repo.UpdateSubscriber(ctx, *sub); err != nil {
		return err
	}

	if err := s.Repo.AppendConsent(ctx, events...); err != nil {
		return err
	}

	return s.Events.Record(ctx, subscriberEvent(*sub, change, actor, now))
}

// setLists makes wanted the subscriber's active lists and returns the slugs joined and left.
func setLists(sub *domain.Subscriber, wanted []string, now time.Time) (joined, left []string) {
	for _, slug := range wanted {
		if m, ok := sub.Membership(slug); !ok || m.State != domain.MembershipActive {
			sub.SetMembership(slug, domain.MembershipActive, now)
			joined = append(joined, slug)
		}
	}

	for _, m := range slices.Clone(sub.Memberships) {
		if m.State != domain.MembershipLeft && !slices.Contains(wanted, m.ListSlug) {
			sub.SetMembership(m.ListSlug, domain.MembershipLeft, now)

			if m.State == domain.MembershipActive {
				left = append(left, m.ListSlug)
			}
		}
	}

	return joined, left
}

// activate opts sub in (again) from any non-erased status.
func activate(sub *domain.Subscriber, now time.Time) {
	sub.Status, sub.OptedInAt, sub.UnsubscribedAt = domain.StatusActive, &now, nil
}

func pendingLists(sub domain.Subscriber) []string {
	var out []string

	for _, m := range sub.Memberships {
		if m.State == domain.MembershipPending {
			out = append(out, m.ListSlug)
		}
	}

	return out
}

// allowConfirm counts a confirmation email against the address's window and reports whether
// it may be sent.
func allowConfirm(sub *domain.Subscriber, now time.Time) bool {
	if sub.ConfirmWindowStart == nil || now.Sub(*sub.ConfirmWindowStart) >= confirmWindow {
		sub.ConfirmWindowStart, sub.ConfirmSends = &now, 0
	}

	if sub.ConfirmSends >= confirmPerWindow {
		return false
	}

	sub.ConfirmSends++

	return true
}

func (s *Service) sendConfirm(ctx context.Context, email *ports.ConfirmEmail) {
	if email == nil || s.Mailer == nil {
		return
	}

	if err := s.Mailer.SendConfirm(ctx, *email); err != nil {
		s.Logger.WarnContext(ctx, "newsletter: confirmation email failed", "error", err)
	}
}

func (s *Service) verifyCaptcha(ctx context.Context, token, ip string) error {
	if s.Captcha == nil {
		return nil
	}

	ok, err := s.Captcha.Verify(ctx, token, ip)
	if err != nil {
		return fmt.Errorf("verify challenge: %w", err)
	}

	if !ok {
		return domain.ErrCaptcha
	}

	return nil
}

func (s *Service) newSubscriber(email, name string, format domain.Format, source domain.Source, ipHash, userAgent string) domain.Subscriber {
	now := s.Clock.Now()

	return domain.Subscriber{
		UUID: s.IDs.New(), Email: email, DisplayName: name, Status: domain.StatusPending, Format: format,
		Source: source, IPHash: ipHash, UserAgent: truncate(userAgent, 512), CreatedAt: now, UpdatedAt: now,
	}
}

func (s *Service) byEmail(ctx context.Context, email string) (domain.Subscriber, bool, error) {
	sub, err := s.Repo.SubscriberByEmail(ctx, email)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.Subscriber{}, false, nil
	}

	return sub, err == nil, err
}

// resolveLists returns the requested active lists, or the default lists when none are named.
func (s *Service) resolveLists(ctx context.Context, slugs []string) ([]domain.List, error) {
	all, err := s.Repo.Lists(ctx, false)
	if err != nil {
		return nil, err
	}

	if len(slugs) == 0 {
		var defaults []domain.List

		for _, l := range all {
			if l.IsDefault {
				defaults = append(defaults, l)
			}
		}

		if len(defaults) == 0 {
			return nil, fmt.Errorf("%w: no newsletter lists are configured", domain.ErrNotConfigured)
		}

		return defaults, nil
	}

	return pickLists(all, slugs)
}

// resolveExact checks that every slug is an active list and returns them deduplicated.
func (s *Service) resolveExact(ctx context.Context, slugs []string) ([]string, error) {
	all, err := s.Repo.Lists(ctx, false)
	if err != nil {
		return nil, err
	}

	picked, err := pickLists(all, slugs)
	if err != nil {
		return nil, err
	}

	out := make([]string, 0, len(picked))
	for _, l := range picked {
		out = append(out, l.Slug)
	}

	return out, nil
}

func pickLists(all []domain.List, slugs []string) ([]domain.List, error) {
	if len(slugs) > domain.MaxLists {
		return nil, domain.Invalid("at most %d lists", domain.MaxLists)
	}

	var out []domain.List

	for _, slug := range slugs {
		i := slices.IndexFunc(all, func(l domain.List) bool { return l.Slug == slug })
		if i < 0 {
			return nil, domain.Invalid("unknown list %q", slug)
		}

		if !slices.ContainsFunc(out, func(l domain.List) bool { return l.Slug == slug }) {
			out = append(out, all[i])
		}
	}

	return out, nil
}

func (s *Service) listNames(ctx context.Context, slugs []string) []string {
	all, err := s.Repo.Lists(ctx, true)
	if err != nil {
		return slugs
	}

	out := make([]string, 0, len(slugs))

	for _, slug := range slugs {
		name := slug
		if i := slices.IndexFunc(all, func(l domain.List) bool { return l.Slug == slug }); i >= 0 {
			name = all[i].Name
		}

		out = append(out, name)
	}

	return out
}

func (s *Service) consent(sub domain.Subscriber, kind, list, source, ipHash string, now time.Time) domain.ConsentEvent {
	return domain.ConsentEvent{
		SubscriberID: sub.UUID, Event: kind, ListSlug: list, Source: source, IPHash: ipHash, OccurredAt: now,
	}
}

func (s *Service) listEvents(sub domain.Subscriber, kind string, slugs []string, source string, now time.Time) []domain.ConsentEvent {
	out := make([]domain.ConsentEvent, 0, len(slugs))
	for _, slug := range slugs {
		out = append(out, s.consent(sub, kind, slug, source, "", now))
	}

	return out
}

// issueToken stores the hash of a new random token and returns the raw value.
func (s *Service) issueToken(
	ctx context.Context, subscriberID uuid.UUID, purpose domain.TokenPurpose, issueID *uuid.UUID, expiresAt time.Time,
) (string, error) {
	raw, hash, err := newToken()
	if err != nil {
		return "", err
	}

	err = s.Repo.CreateToken(ctx, domain.Token{
		SubscriberID: subscriberID, Purpose: purpose, Hash: hash, IssueID: issueID,
		ExpiresAt: expiresAt, CreatedAt: s.Clock.Now(),
	})

	return raw, err
}

// lookupToken finds an unexpired token for purpose. Unknown tokens and tokens for another
// purpose look the same.
func (s *Service) lookupToken(ctx context.Context, raw string, purpose domain.TokenPurpose) (domain.Token, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > maxTokenLength {
		return domain.Token{}, domain.ErrTokenInvalid
	}

	token, err := s.Repo.Token(ctx, hashToken(raw))
	if errors.Is(err, domain.ErrNotFound) || (err == nil && token.Purpose != purpose) {
		return domain.Token{}, domain.ErrTokenInvalid
	}

	if err != nil {
		return domain.Token{}, err
	}

	if !s.Clock.Now().Before(token.ExpiresAt) {
		return domain.Token{}, domain.ErrTokenExpired
	}

	return token, nil
}

// newToken returns a random 256-bit URL-safe token and its SHA-256 hex digest.
func newToken() (raw, hash string, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("newsletter token: %w", err)
	}

	raw = base64.RawURLEncoding.EncodeToString(buf)

	return raw, hashToken(raw), nil
}

func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func (s *Service) hashIdentity(value string) string {
	if value == "" {
		return ""
	}

	if s.IdentityHasher != nil {
		return s.IdentityHasher.MAC(value)
	}

	sum := sha256.Sum256([]byte(value))

	return hex.EncodeToString(sum[:])
}

func cleanName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if utf8.RuneCountInString(name) > maxNameLength || strings.ContainsAny(name, "\r\n<>") {
		return "", domain.Invalid("name must be at most %d characters without angle brackets or line breaks", maxNameLength)
	}

	return name, nil
}

func truncate(value string, n int) string {
	if len(value) <= n {
		return value
	}

	for n > 0 && !utf8.RuneStart(value[n]) {
		n--
	}

	return value[:n]
}
