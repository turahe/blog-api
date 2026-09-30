package service

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/core/event"
	"github.com/turahe/blog-api/internal/core/event/eventtest"
	"github.com/turahe/blog-api/internal/core/newsletter/domain"
	"github.com/turahe/blog-api/internal/core/newsletter/ports"
)

var (
	testNow = time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	errBoom = errors.New("boom")
)

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.now = c.now.Add(d)
}

type randomIDs struct{}

func (randomIDs) New() uuid.UUID { return uuid.New() }

// failure makes a repository method return err from its from-th call on (every call when from
// is zero).
type failure struct {
	err  error
	from int
}

// memRepo is an in-memory ports.Repository. hooks run inside a method, with the lock held,
// before it does its work, which lets a test simulate a concurrent writer.
type memRepo struct {
	mu       sync.Mutex
	calls    map[string]int
	failures map[string]failure
	hooks    map[string]func(r *memRepo)

	lists       []domain.List
	cfg         *domain.Config
	subs        map[uuid.UUID]domain.Subscriber
	tokens      []domain.Token
	nextTokenID int64
	consent     []domain.ConsentEvent
	issues      map[uuid.UUID]domain.Issue
	queue       map[uuid.UUID][]domain.Recipient
	claims      []domain.Claim
	deliveries  []domain.DeliveryResult

	subscriberFilter domain.SubscriberFilter
	issueFilter      domain.IssueFilter
	historyLimit     int
	prunedBefore     time.Time
}

var _ ports.Repository = (*memRepo)(nil)

func newMemRepo() *memRepo {
	archived := testNow.Add(-24 * time.Hour)

	return &memRepo{
		calls:    map[string]int{},
		failures: map[string]failure{},
		hooks:    map[string]func(r *memRepo){},
		lists: []domain.List{
			{UUID: uuid.New(), Slug: "weekly", Name: "Weekly digest", IsDefault: true, Position: 1},
			{UUID: uuid.New(), Slug: "product", Name: "Product news", Position: 2},
			{UUID: uuid.New(), Slug: "old", Name: "Old list", Position: 3, ArchivedAt: &archived},
		},
		subs:   map[uuid.UUID]domain.Subscriber{},
		issues: map[uuid.UUID]domain.Issue{},
		queue:  map[uuid.UUID][]domain.Recipient{},
	}
}

func (r *memRepo) failOn(method string, err error) { r.failAt(method, 0, err) }

func (r *memRepo) failAt(method string, from int, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.failures[method] = failure{err: err, from: from}
}

func (r *memRepo) hook(method string, fn func(r *memRepo)) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.hooks[method] = fn
}

func (r *memRepo) callCount(method string) int {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.calls[method]
}

// hit records a call and returns the configured failure. The caller holds r.mu.
func (r *memRepo) hit(method string) error {
	r.calls[method]++

	if fn := r.hooks[method]; fn != nil {
		fn(r)
	}

	f, ok := r.failures[method]
	if !ok || (f.from > 0 && r.calls[method] < f.from) {
		return nil
	}

	return f.err
}

// detach copies s without its load-time modification flag, as a real store returns it.
func detach(s domain.Subscriber) domain.Subscriber {
	return domain.Subscriber{
		UUID: s.UUID, Email: s.Email, DisplayName: s.DisplayName, UserID: s.UserID, Status: s.Status,
		Format: s.Format, Source: s.Source, IPHash: s.IPHash, UserAgent: s.UserAgent,
		ConfirmSends: s.ConfirmSends, ConfirmWindowStart: s.ConfirmWindowStart, OptedInAt: s.OptedInAt,
		UnsubscribedAt: s.UnsubscribedAt, BouncedAt: s.BouncedAt, ComplainedAt: s.ComplainedAt,
		ErasedAt: s.ErasedAt, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt,
		Memberships: slices.Clone(s.Memberships),
	}
}

func (r *memRepo) Lists(_ context.Context, includeArchived bool) ([]domain.List, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	method := "Lists"
	if includeArchived {
		method = "Lists(archived)"
	}

	if err := r.hit(method); err != nil {
		return nil, err
	}

	var out []domain.List

	for _, l := range r.lists {
		if includeArchived || l.ArchivedAt == nil {
			out = append(out, l)
		}
	}

	return out, nil
}

func (r *memRepo) SaveLists(_ context.Context, lists []domain.ListInput, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.hit("SaveLists"); err != nil {
		return err
	}

	for i, in := range lists {
		j := slices.IndexFunc(r.lists, func(l domain.List) bool { return l.Slug == in.Slug })
		if j < 0 {
			r.lists = append(r.lists, domain.List{UUID: uuid.New(), Slug: in.Slug})
			j = len(r.lists) - 1
		}

		l := &r.lists[j]
		l.Name, l.Description, l.IsDefault, l.Position, l.ArchivedAt = in.Name, in.Description, in.IsDefault, i+1, nil
	}

	for i := range r.lists {
		l := &r.lists[i]
		kept := slices.ContainsFunc(lists, func(in domain.ListInput) bool { return in.Slug == l.Slug })

		if !kept && l.ArchivedAt == nil {
			l.ArchivedAt = &at
		}
	}

	return nil
}

func (r *memRepo) Config(context.Context) (domain.Config, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.hit("Config"); err != nil {
		return domain.Config{}, err
	}

	if r.cfg == nil {
		return domain.DefaultConfig(), nil
	}

	return *r.cfg, nil
}

func (r *memRepo) SaveConfig(_ context.Context, cfg domain.Config) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.hit("SaveConfig"); err != nil {
		return err
	}

	r.cfg = &cfg

	return nil
}

func (r *memRepo) Subscriber(_ context.Context, id uuid.UUID) (domain.Subscriber, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.hit("Subscriber"); err != nil {
		return domain.Subscriber{}, err
	}

	sub, ok := r.subs[id]
	if !ok {
		return domain.Subscriber{}, domain.ErrNotFound
	}

	return detach(sub), nil
}

func (r *memRepo) SubscriberByEmail(_ context.Context, normalized string) (domain.Subscriber, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.hit("SubscriberByEmail"); err != nil {
		return domain.Subscriber{}, err
	}

	for _, sub := range r.subs {
		if sub.Email == normalized {
			return detach(sub), nil
		}
	}

	return domain.Subscriber{}, domain.ErrNotFound
}

func (r *memRepo) SubscriberByUser(_ context.Context, userID uuid.UUID) (domain.Subscriber, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.hit("SubscriberByUser"); err != nil {
		return domain.Subscriber{}, err
	}

	for _, sub := range r.subs {
		if sub.UserID != nil && *sub.UserID == userID {
			return detach(sub), nil
		}
	}

	return domain.Subscriber{}, domain.ErrNotFound
}

func (r *memRepo) CreateSubscriber(_ context.Context, s domain.Subscriber) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.hit("CreateSubscriber"); err != nil {
		return err
	}

	for _, sub := range r.subs {
		if sub.Email == s.Email {
			return domain.ErrConflict
		}
	}

	r.subs[s.UUID] = detach(s)

	return nil
}

func (r *memRepo) UpdateSubscriber(_ context.Context, s domain.Subscriber) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.hit("UpdateSubscriber"); err != nil {
		return err
	}

	r.subs[s.UUID] = detach(s)

	return nil
}

func (r *memRepo) ListSubscribers(_ context.Context, filter domain.SubscriberFilter) (domain.SubscriberPage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.hit("ListSubscribers"); err != nil {
		return domain.SubscriberPage{}, err
	}

	r.subscriberFilter = filter

	total := int64(len(r.subs))
	limit := filter.PageRequest.Limit
	page := filter.PageRequest.Page
	return domain.SubscriberPage{
		Total:         &total,
		Limit:         limit,
		OffsetPage:    page,
		OffsetPerPage: limit,
	}, nil
}

func (r *memRepo) EraseSubscriber(_ context.Context, id uuid.UUID, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.hit("EraseSubscriber"); err != nil {
		return err
	}

	sub := r.subs[id]
	sub.Email, sub.DisplayName, sub.IPHash, sub.UserAgent, sub.UserID = "", "", "", "", nil
	sub.Status, sub.ErasedAt = domain.StatusErased, &at
	r.subs[id] = sub

	r.tokens = slices.DeleteFunc(r.tokens, func(t domain.Token) bool { return t.SubscriberID == id })

	return nil
}

func (r *memRepo) CreateToken(_ context.Context, t domain.Token) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.hit("CreateToken"); err != nil {
		return err
	}

	r.nextTokenID++
	t.ID = r.nextTokenID
	r.tokens = append(r.tokens, t)

	return nil
}

func (r *memRepo) Token(_ context.Context, hash string) (domain.Token, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.hit("Token"); err != nil {
		return domain.Token{}, err
	}

	for _, t := range r.tokens {
		if t.Hash == hash {
			return t, nil
		}
	}

	return domain.Token{}, domain.ErrNotFound
}

func (r *memRepo) UseToken(_ context.Context, id int64, at time.Time) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.hit("UseToken"); err != nil {
		return false, err
	}

	for i := range r.tokens {
		if r.tokens[i].ID == id {
			if r.tokens[i].UsedAt != nil {
				return false, nil
			}

			r.tokens[i].UsedAt = &at

			return true, nil
		}
	}

	return false, nil
}

func (r *memRepo) RevokeTokens(_ context.Context, subscriberID uuid.UUID, purpose domain.TokenPurpose) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.hit("RevokeTokens"); err != nil {
		return err
	}

	r.tokens = slices.DeleteFunc(r.tokens, func(t domain.Token) bool {
		return t.SubscriberID == subscriberID && t.Purpose == purpose
	})

	return nil
}

func (r *memRepo) PruneTokens(_ context.Context, before time.Time) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.hit("PruneTokens"); err != nil {
		return 0, err
	}

	r.prunedBefore = before
	n := len(r.tokens)
	r.tokens = slices.DeleteFunc(r.tokens, func(t domain.Token) bool { return t.ExpiresAt.Before(before) })

	return int64(n - len(r.tokens)), nil
}

func (r *memRepo) AppendConsent(_ context.Context, events ...domain.ConsentEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.hit("AppendConsent"); err != nil {
		return err
	}

	r.consent = append(r.consent, events...)

	return nil
}

func (r *memRepo) ConsentHistory(_ context.Context, subscriberID uuid.UUID, limit int) ([]domain.ConsentEvent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.hit("ConsentHistory"); err != nil {
		return nil, err
	}

	r.historyLimit = limit

	var out []domain.ConsentEvent

	for _, e := range r.consent {
		if e.SubscriberID == subscriberID {
			out = append(out, e)
		}
	}

	return out, nil
}

func (r *memRepo) CreateIssue(_ context.Context, issue domain.Issue) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.hit("CreateIssue"); err != nil {
		return err
	}

	r.issues[issue.UUID] = issue

	return nil
}

func (r *memRepo) Issue(_ context.Context, id uuid.UUID) (domain.Issue, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.hit("Issue"); err != nil {
		return domain.Issue{}, err
	}

	issue, ok := r.issues[id]
	if !ok {
		return domain.Issue{}, domain.ErrNotFound
	}

	return issue, nil
}

func (r *memRepo) UpdateIssue(_ context.Context, issue domain.Issue, expected domain.IssueStatus) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.hit("UpdateIssue"); err != nil {
		return false, err
	}

	if r.issues[issue.UUID].Status != expected {
		return false, nil
	}

	r.issues[issue.UUID] = issue

	return true, nil
}

func (r *memRepo) ListIssues(_ context.Context, filter domain.IssueFilter) (domain.IssuePage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.hit("ListIssues"); err != nil {
		return domain.IssuePage{}, err
	}

	r.issueFilter = filter

	total := int64(len(r.issues))
	limit := filter.PageRequest.Limit
	page := filter.PageRequest.Page
	return domain.IssuePage{
		Total:         &total,
		Limit:         limit,
		OffsetPage:    page,
		OffsetPerPage: limit,
	}, nil
}

func (r *memRepo) DueIssues(_ context.Context, now time.Time, limit int) ([]domain.Issue, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.hit("DueIssues"); err != nil {
		return nil, err
	}

	var due []domain.Issue

	for _, issue := range r.issues {
		if issue.Status == domain.IssueScheduled && issue.SendAt != nil && !issue.SendAt.After(now) {
			due = append(due, issue)
		}
	}

	slices.SortFunc(due, func(a, b domain.Issue) int { return a.SendAt.Compare(*b.SendAt) })

	return due[:min(limit, len(due))], nil
}

func (r *memRepo) MoveIssue(
	_ context.Context, id uuid.UUID, from []domain.IssueStatus, to domain.IssueStatus, at time.Time,
) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.hit("MoveIssue"); err != nil {
		return false, err
	}

	issue, ok := r.issues[id]
	if !ok || !slices.Contains(from, issue.Status) {
		return false, nil
	}

	issue.Status, issue.UpdatedAt = to, at
	r.issues[id] = issue

	return true, nil
}

func (r *memRepo) ClaimRecipients(_ context.Context, issueID uuid.UUID, claim domain.Claim) ([]domain.Recipient, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.hit("ClaimRecipients"); err != nil {
		return nil, err
	}

	r.claims = append(r.claims, claim)

	queue := r.queue[issueID]
	n := min(claim.Limit, len(queue))
	r.queue[issueID] = queue[n:]

	return queue[:n], nil
}

func (r *memRepo) RecordDelivery(_ context.Context, result domain.DeliveryResult) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.hit("RecordDelivery"); err != nil {
		return err
	}

	r.deliveries = append(r.deliveries, result)

	return nil
}

func (r *memRepo) DeliveryCounts(context.Context, uuid.UUID, int) (domain.DeliveryCounts, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.hit("DeliveryCounts"); err != nil {
		return domain.DeliveryCounts{}, err
	}

	var counts domain.DeliveryCounts

	for _, d := range r.deliveries {
		switch {
		case d.Sent:
			counts.Sent++
		case d.Permanent:
			counts.Failed++
		default:
			counts.Retryable++
		}
	}

	return counts, nil
}

func (r *memRepo) FinishIssue(_ context.Context, id uuid.UUID, counts domain.DeliveryCounts, at time.Time) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.hit("FinishIssue"); err != nil {
		return false, err
	}

	issue := r.issues[id]
	if issue.Status != domain.IssueSending {
		return false, nil
	}

	issue.Status, issue.SentCount, issue.FailedCount, issue.CompletedAt = domain.IssueSent, counts.Sent, counts.Failed, &at
	r.issues[id] = issue

	return true, nil
}

func (r *memRepo) subscriber(t *testing.T, id uuid.UUID) domain.Subscriber {
	t.Helper()

	r.mu.Lock()
	defer r.mu.Unlock()

	sub, ok := r.subs[id]
	require.True(t, ok, "subscriber %s is stored", id)

	return sub
}

func (r *memRepo) onlySubscriber(t *testing.T) domain.Subscriber {
	t.Helper()

	r.mu.Lock()
	defer r.mu.Unlock()

	require.Len(t, r.subs, 1)

	for _, sub := range r.subs {
		return sub
	}

	return domain.Subscriber{}
}

func (r *memRepo) subscriberCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	return len(r.subs)
}

func (r *memRepo) consentKinds(id uuid.UUID) []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	var out []string

	for _, e := range r.consent {
		if e.SubscriberID == id {
			out = append(out, e.Event)
		}
	}

	return out
}

func (r *memRepo) consentEvents() []domain.ConsentEvent {
	r.mu.Lock()
	defer r.mu.Unlock()

	return slices.Clone(r.consent)
}

func (r *memRepo) tokensFor(id uuid.UUID, purpose domain.TokenPurpose) []domain.Token {
	r.mu.Lock()
	defer r.mu.Unlock()

	var out []domain.Token

	for _, t := range r.tokens {
		if t.SubscriberID == id && t.Purpose == purpose {
			out = append(out, t)
		}
	}

	return out
}

func (r *memRepo) putSubscriber(sub domain.Subscriber) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.subs[sub.UUID] = detach(sub)
}

func (r *memRepo) putIssue(issue domain.Issue) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.issues[issue.UUID] = issue
}

func (r *memRepo) storedIssue(id uuid.UUID) domain.Issue {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.issues[id]
}

func (r *memRepo) enqueue(issueID uuid.UUID, recipients ...domain.Recipient) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.queue[issueID] = append(r.queue[issueID], recipients...)
}

func (r *memRepo) recordedDeliveries() []domain.DeliveryResult {
	r.mu.Lock()
	defer r.mu.Unlock()

	return slices.Clone(r.deliveries)
}

// putToken stores a token for raw and returns raw.
func (r *memRepo) putToken(raw string, t domain.Token) string {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.nextTokenID++
	t.ID, t.Hash = r.nextTokenID, hashToken(raw)
	r.tokens = append(r.tokens, t)

	return raw
}

type captureMailer struct {
	mu       sync.Mutex
	confirms []ports.ConfirmEmail
	welcomes []ports.WelcomeEmail
	err      error
}

func (m *captureMailer) SendConfirm(_ context.Context, email ports.ConfirmEmail) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.confirms = append(m.confirms, email)

	return m.err
}

func (m *captureMailer) SendWelcome(_ context.Context, email ports.WelcomeEmail) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.welcomes = append(m.welcomes, email)

	return m.err
}

func (m *captureMailer) sentConfirms() []ports.ConfirmEmail {
	m.mu.Lock()
	defer m.mu.Unlock()

	return slices.Clone(m.confirms)
}

func (m *captureMailer) sentWelcomes() []ports.WelcomeEmail {
	m.mu.Lock()
	defer m.mu.Unlock()

	return slices.Clone(m.welcomes)
}

func (m *captureMailer) lastConfirmToken(t *testing.T) string {
	t.Helper()

	confirms := m.sentConfirms()
	require.NotEmpty(t, confirms, "a confirmation email was sent")

	return confirms[len(confirms)-1].Token
}

type fakeLinks struct {
	site, name, api string
}

func (l fakeLinks) SiteURL(context.Context) string  { return l.site }
func (l fakeLinks) SiteName(context.Context) string { return l.name }
func (l fakeLinks) APIURL() string                  { return l.api }

type fakeMarkdown struct{}

func (fakeMarkdown) Render(source string) string { return "<p>" + source + "</p>" }

// captureSender records emails; fn, when set, decides each send's result.
type captureSender struct {
	mu     sync.Mutex
	emails []ports.Email
	fn     func(ports.Email) error
}

func (s *captureSender) Send(_ context.Context, email ports.Email) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.emails = append(s.emails, email)

	if s.fn != nil {
		return s.fn(email)
	}

	return nil
}

func (s *captureSender) sent() []ports.Email {
	s.mu.Lock()
	defer s.mu.Unlock()

	return slices.Clone(s.emails)
}

type fakeContacts struct {
	mu       sync.Mutex
	contacts []ports.Contact
	err      error
}

func (c *fakeContacts) SyncContact(_ context.Context, contact ports.Contact) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.contacts = append(c.contacts, contact)

	return c.err
}

type fakeVerifier struct{ err error }

func (v fakeVerifier) Verify(string, string, []byte) error { return v.err }

type fakeCaptcha struct {
	ok  bool
	err error
}

func (c fakeCaptcha) Verify(context.Context, string, string) (bool, error) { return c.ok, c.err }

type fakeAccounts struct {
	accounts map[uuid.UUID]ports.Account
	err      error
}

func (a fakeAccounts) Account(_ context.Context, userID uuid.UUID) (ports.Account, error) {
	if a.err != nil {
		return ports.Account{}, a.err
	}

	account, ok := a.accounts[userID]
	if !ok {
		return ports.Account{}, domain.ErrNotFound
	}

	return account, nil
}

// syncBuffer is a log sink safe for concurrent writers.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

type fixture struct {
	svc    *Service
	repo   *memRepo
	clock  *fakeClock
	mailer *captureMailer
	sender *captureSender
	events *eventtest.Recorder
	logs   *syncBuffer
}

// newFixture returns a service over fresh fakes with a sendable config. edit, when given,
// adjusts the dependencies and config before the service is built.
func newFixture(edit ...func(*Deps, *Config)) *fixture {
	repo := newMemRepo()
	repo.cfg = &domain.Config{
		FromName: "Example", FromEmail: "news@example.test", ReplyTo: "reply@example.test",
		PostalAddress: "1 Example Street", ConfirmTTL: domain.DefaultConfirmTTL, DoubleOptInRequired: true,
	}

	f := &fixture{
		repo: repo, clock: &fakeClock{now: testNow}, mailer: &captureMailer{}, sender: &captureSender{},
		events: &eventtest.Recorder{}, logs: &syncBuffer{},
	}

	deps := Deps{
		Repo: repo, IDs: randomIDs{}, Clock: f.clock, Mailer: f.mailer, Sender: f.sender,
		Links:    fakeLinks{site: "https://blog.example.test/", name: "Example Blog", api: "https://api.example.test/"},
		Markdown: fakeMarkdown{}, Events: f.events.Unit(),
		Logger: slog.New(slog.NewTextHandler(f.logs, nil)),
	}
	cfg := Config{}

	for _, fn := range edit {
		fn(&deps, &cfg)
	}

	f.svc = New(deps, cfg)

	return f
}

// activeSubscriber stores an active subscriber of lists and returns it.
func (f *fixture) activeSubscriber(email string, lists ...string) domain.Subscriber {
	now := f.clock.Now()
	sub := domain.Subscriber{
		UUID: uuid.New(), Email: email, Status: domain.StatusActive, Format: domain.FormatHTML,
		Source: domain.SourcePublic, OptedInAt: &now, CreatedAt: now, UpdatedAt: now,
	}

	for _, slug := range lists {
		sub.SetMembership(slug, domain.MembershipActive, now)
	}

	f.repo.putSubscriber(sub)

	return sub
}

// token stores a token of purpose for the subscriber and returns its raw value.
func (f *fixture) token(subscriberID uuid.UUID, purpose domain.TokenPurpose) string {
	return f.repo.putToken("raw-"+uuid.NewString(), domain.Token{
		SubscriberID: subscriberID, Purpose: purpose, ExpiresAt: f.clock.Now().Add(time.Hour), CreatedAt: f.clock.Now(),
	})
}

// changes returns the change of each recorded subscriber-changed event.
func (f *fixture) changes() []string {
	var out []string

	for _, e := range f.events.Events() {
		if e.Type != event.NewsletterSubscriberChanged {
			continue
		}

		payload, _ := e.Payload.(map[string]any)
		change, _ := payload["change"].(string)
		out = append(out, change)
	}

	return out
}

// unsubscribeToken extracts the raw token from an email's one-click header.
func unsubscribeToken(t *testing.T, email ports.Email) string {
	t.Helper()

	header := strings.Trim(email.Headers["List-Unsubscribe"], "<>")
	u, err := url.Parse(header)
	require.NoError(t, err)

	return u.Query().Get("token")
}
