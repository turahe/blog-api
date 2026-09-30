package persistence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/core/newsletter/domain"
	"github.com/turahe/blog-api/internal/shared/pagination"
	"gorm.io/gorm"
)

func insertNewsletterList(t *testing.T, tx *gorm.DB, position int, archivedAt *time.Time) string {
	t.Helper()

	slug := uniqueSlug("nl")
	require.NoError(t, tx.Exec(`INSERT INTO newsletter_lists (slug, name, position, archived_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, now(), now())`, slug, "List "+slug, position, archivedAt).Error)

	return slug
}

func newSubscriber(mutate func(*domain.Subscriber)) domain.Subscriber {
	now := time.Now().UTC().Truncate(time.Microsecond)

	s := domain.Subscriber{
		UUID: uuid.New(), Email: uniqueSlug("sub") + "@example.test", Status: domain.StatusActive,
		Format: domain.FormatHTML, Source: domain.SourcePublic, CreatedAt: now, UpdatedAt: now,
	}
	if mutate != nil {
		mutate(&s)
	}

	return s
}

func createSubscriber(t *testing.T, repo *NewsletterRepository, mutate func(*domain.Subscriber)) domain.Subscriber {
	t.Helper()

	s := newSubscriber(mutate)
	require.NoError(t, repo.CreateSubscriber(t.Context(), s))

	return s
}

func member(slug string, state domain.MembershipState) domain.Membership {
	return domain.Membership{ListSlug: slug, State: state}
}

func tokenHash() string {
	sum := sha256.Sum256([]byte(uuid.NewString()))
	return hex.EncodeToString(sum[:])
}

func listSlugs(lists []domain.List, keep ...string) []string {
	var out []string

	for _, l := range lists {
		for _, slug := range keep {
			if l.Slug == slug {
				out = append(out, l.Slug)
			}
		}
	}

	return out
}

func subscriberIDs(subs []domain.Subscriber) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(subs))
	for _, s := range subs {
		out = append(out, s.UUID)
	}

	return out
}

func TestNewsletterRepositoryLists(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewNewsletterRepository(tx)
	archivedAt := time.Now().UTC()
	second := insertNewsletterList(t, tx, 2, nil)
	first := insertNewsletterList(t, tx, 1, nil)
	archived := insertNewsletterList(t, tx, 0, &archivedAt)

	active, err := repo.Lists(t.Context(), false)
	require.NoError(t, err)
	require.Equal(t, []string{first, second}, listSlugs(active, first, second, archived))

	all, err := repo.Lists(t.Context(), true)
	require.NoError(t, err)
	require.Equal(t, []string{first, second, archived}, listSlugs(all, first, second, archived), "archived lists come last")

	_, err = repo.Lists(canceledContext(t), true)
	require.ErrorIs(t, err, context.Canceled)
}

func TestNewsletterRepositorySaveLists(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewNewsletterRepository(tx)
	ctx := t.Context()
	alpha, beta := uniqueSlug("nl"), uniqueSlug("nl")
	at := time.Now().UTC().Truncate(time.Microsecond)

	find := func(slug string) domain.List {
		t.Helper()

		lists, err := repo.Lists(ctx, true)
		require.NoError(t, err)

		for _, l := range lists {
			if l.Slug == slug {
				return l
			}
		}

		require.FailNow(t, "list not found", slug)

		return domain.List{}
	}

	require.NoError(t, repo.SaveLists(ctx, []domain.ListInput{
		{Slug: alpha, Name: "  Alpha  ", Description: "The first", IsDefault: true},
		{Slug: beta, Name: "Beta"},
	}, at))

	got := find(alpha)
	require.Equal(t, "Alpha", got.Name, "names are trimmed")
	require.Equal(t, "The first", got.Description)
	require.True(t, got.IsDefault)
	require.Zero(t, got.Position)
	require.Nil(t, got.ArchivedAt)
	require.Equal(t, 1, find(beta).Position)

	require.NoError(t, repo.SaveLists(ctx, []domain.ListInput{{Slug: beta, Name: "Beta 2", IsDefault: true}}, at.Add(time.Minute)))

	got = find(alpha)
	require.NotNil(t, got.ArchivedAt, "lists left out are archived")
	require.False(t, got.IsDefault, "an archived list is never the default")

	got = find(beta)
	require.Equal(t, "Beta 2", got.Name)
	require.Zero(t, got.Position)
	require.True(t, got.IsDefault)

	require.NoError(t, repo.SaveLists(ctx, []domain.ListInput{{Slug: alpha, Name: "Alpha", IsDefault: true}}, at.Add(2*time.Minute)))
	require.Nil(t, find(alpha).ArchivedAt, "saving an archived list restores it")

	inSavepoint(t, tx, func() {
		require.ErrorContains(t, repo.SaveLists(ctx, []domain.ListInput{{Slug: "Not A Slug", Name: "x"}}, at), "save newsletter list")
	})
}

func TestNewsletterRepositoryConfig(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewNewsletterRepository(tx)
	ctx := t.Context()
	editor := insertUser(t, tx)

	require.NoError(t, tx.Exec("DELETE FROM newsletter_provider_config").Error)

	cfg, err := repo.Config(ctx)
	require.NoError(t, err)
	require.Equal(t, domain.DefaultConfig(), cfg, "defaults before anything is saved")

	saved := domain.Config{
		FromName: "Blog", FromEmail: "news@example.test", ReplyTo: "reply@example.test", PostalAddress: "1 Main St",
		ConfirmTTL: 24 * time.Hour, DoubleOptInRequired: false, UpdatedBy: &editor,
	}
	require.NoError(t, repo.SaveConfig(ctx, saved))

	cfg, err = repo.Config(ctx)
	require.NoError(t, err)
	require.NotNil(t, cfg.UpdatedAt)
	require.WithinDuration(t, time.Now(), *cfg.UpdatedAt, time.Minute, "UpdatedAt defaults to now")
	saved.UpdatedAt = cfg.UpdatedAt
	require.Equal(t, saved, cfg)

	updatedAt := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
	saved.UpdatedAt, saved.UpdatedBy, saved.ConfirmTTL = &updatedAt, nil, 2*time.Hour
	require.NoError(t, repo.SaveConfig(ctx, saved))

	cfg, err = repo.Config(ctx)
	require.NoError(t, err)
	require.WithinDuration(t, updatedAt, *cfg.UpdatedAt, 0)
	require.Nil(t, cfg.UpdatedBy)
	require.Equal(t, 2*time.Hour, cfg.ConfirmTTL)

	inSavepoint(t, tx, func() {
		saved.ConfirmTTL = time.Minute
		require.ErrorContains(t, repo.SaveConfig(ctx, saved), "save newsletter config")
	})

	_, err = repo.Config(canceledContext(t))
	require.ErrorIs(t, err, context.Canceled)
}

func TestNewsletterRepositorySubscriberLookups(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewNewsletterRepository(tx)
	ctx := t.Context()
	second, first := insertNewsletterList(t, tx, 2, nil), insertNewsletterList(t, tx, 1, nil)
	user := insertUser(t, tx)
	joined := time.Now().UTC().Truncate(time.Microsecond)

	sub := createSubscriber(t, repo, func(s *domain.Subscriber) {
		s.DisplayName, s.UserID, s.IPHash, s.UserAgent, s.Format = "Ann", &user, "ip-hash", "Firefox", domain.FormatPlaintext
		s.ConfirmSends, s.ConfirmWindowStart, s.OptedInAt = 2, &joined, &joined
		s.Memberships = []domain.Membership{
			{ListSlug: second, State: domain.MembershipActive, JoinedAt: &joined},
			member(first, domain.MembershipPending),
		}
	})

	tests := []struct {
		name    string
		find    func() (domain.Subscriber, error)
		wantErr error
	}{
		{name: "by id", find: func() (domain.Subscriber, error) { return repo.Subscriber(ctx, sub.UUID) }},
		{name: "by email", find: func() (domain.Subscriber, error) { return repo.SubscriberByEmail(ctx, sub.Email) }},
		{name: "by user", find: func() (domain.Subscriber, error) { return repo.SubscriberByUser(ctx, user) }},
		{name: "unknown id", find: func() (domain.Subscriber, error) { return repo.Subscriber(ctx, uuid.New()) }, wantErr: domain.ErrNotFound},
		{name: "unknown email", find: func() (domain.Subscriber, error) {
			return repo.SubscriberByEmail(ctx, "missing-"+sub.Email)
		}, wantErr: domain.ErrNotFound},
		{name: "unknown user", find: func() (domain.Subscriber, error) { return repo.SubscriberByUser(ctx, uuid.New()) }, wantErr: domain.ErrNotFound},
		{name: "database error", find: func() (domain.Subscriber, error) {
			return repo.Subscriber(canceledContext(t), sub.UUID)
		}, wantErr: context.Canceled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.find()
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			require.Equal(t, sub.UUID, got.UUID)
			require.Equal(t, sub.Email, got.Email)
			require.Equal(t, "Ann", got.DisplayName)
			require.Equal(t, &user, got.UserID)
			require.Equal(t, domain.StatusActive, got.Status)
			require.Equal(t, domain.FormatPlaintext, got.Format)
			require.Equal(t, domain.SourcePublic, got.Source)
			require.Equal(t, "ip-hash", got.IPHash)
			require.Equal(t, "Firefox", got.UserAgent)
			require.Equal(t, 2, got.ConfirmSends)
			require.WithinDuration(t, joined, *got.OptedInAt, 0)
			require.Len(t, got.Memberships, 2)
			require.Equal(t, first, got.Memberships[0].ListSlug, "memberships follow list position")
			require.Equal(t, "List "+first, got.Memberships[0].ListName)
			require.Equal(t, domain.MembershipPending, got.Memberships[0].State)
			require.Equal(t, domain.MembershipActive, got.Memberships[1].State)
			require.WithinDuration(t, joined, *got.Memberships[1].JoinedAt, 0)
		})
	}
}

func TestNewsletterRepositoryCreateSubscriberConflict(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewNewsletterRepository(tx)
	ctx := t.Context()
	sub := createSubscriber(t, repo, nil)

	err := repo.CreateSubscriber(ctx, newSubscriber(func(s *domain.Subscriber) { s.Email = sub.Email }))
	require.ErrorIs(t, err, domain.ErrConflict)

	_, err = repo.Subscriber(ctx, sub.UUID)
	require.NoError(t, err, "a taken address leaves the enclosing transaction usable")

	err = repo.CreateSubscriber(ctx, newSubscriber(func(s *domain.Subscriber) { s.Status = "bogus" }))
	require.ErrorContains(t, err, "create newsletter subscriber")
	require.NotErrorIs(t, err, domain.ErrConflict)
}

func TestNewsletterRepositoryUpdateSubscriber(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewNewsletterRepository(tx)
	ctx := t.Context()
	list := insertNewsletterList(t, tx, 0, nil)
	sub := createSubscriber(t, repo, func(s *domain.Subscriber) {
		s.Memberships = []domain.Membership{member(list, domain.MembershipPending)}
	})
	at := time.Now().UTC().Add(time.Minute).Truncate(time.Microsecond)

	loaded, err := repo.Subscriber(ctx, sub.UUID)
	require.NoError(t, err)

	loaded.DisplayName, loaded.Status, loaded.Format = "Renamed", domain.StatusUnsubscribed, domain.FormatPlaintext
	loaded.UnsubscribedAt, loaded.UpdatedAt = &at, at
	loaded.Memberships[0].State = domain.MembershipActive
	require.NoError(t, repo.UpdateSubscriber(ctx, loaded))

	got, err := repo.Subscriber(ctx, sub.UUID)
	require.NoError(t, err)
	require.Equal(t, "Renamed", got.DisplayName)
	require.Equal(t, domain.StatusUnsubscribed, got.Status)
	require.Equal(t, domain.FormatPlaintext, got.Format)
	require.WithinDuration(t, at, *got.UnsubscribedAt, 0)
	require.Equal(t, domain.MembershipPending, got.Memberships[0].State, "unmodified memberships are not written")

	got.SetMembership(list, domain.MembershipActive, at)
	require.NoError(t, repo.UpdateSubscriber(ctx, got))

	got, err = repo.Subscriber(ctx, sub.UUID)
	require.NoError(t, err)
	require.Equal(t, domain.MembershipActive, got.Memberships[0].State)
	require.WithinDuration(t, at, *got.Memberships[0].JoinedAt, 0)

	inSavepoint(t, tx, func() {
		invalid := got
		invalid.Status = "bogus"
		require.ErrorContains(t, repo.UpdateSubscriber(ctx, invalid), "update newsletter subscriber")
	})

	inSavepoint(t, tx, func() {
		got.SetMembership(list, "bogus", at)
		require.ErrorContains(t, repo.UpdateSubscriber(ctx, got), "save newsletter membership")
	})
}

func TestNewsletterRepositoryListSubscribers(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewNewsletterRepository(tx)
	list, other := insertNewsletterList(t, tx, 0, nil), insertNewsletterList(t, tx, 1, nil)
	prefix := uniqueSlug("find")
	base := time.Now().UTC().Truncate(time.Microsecond)

	add := func(minute int, email, name string, status domain.Status, memberships ...domain.Membership) domain.Subscriber {
		return createSubscriber(t, repo, func(s *domain.Subscriber) {
			s.Email, s.DisplayName, s.Status, s.Memberships = email, name, status, memberships
			s.CreatedAt = base.Add(time.Duration(minute) * time.Minute)
		})
	}

	underscore := add(0, prefix+"_a@example.test", "Zed", domain.StatusActive, member(list, domain.MembershipActive))
	letter := add(1, prefix+"xb@example.test", "", domain.StatusActive, member(list, domain.MembershipPending))
	named := add(2, uniqueSlug("c")+"@example.test", strings.ToUpper(prefix)+" Carol", domain.StatusUnsubscribed,
		member(list, domain.MembershipActive))
	add(3, prefix+"_d@example.test", "", domain.StatusActive, member(list, domain.MembershipLeft))
	add(4, prefix+"_e@example.test", "", domain.StatusActive, member(other, domain.MembershipActive))

	tests := []struct {
		name      string
		filter    domain.SubscriberFilter
		want      []uuid.UUID
		wantTotal int64
	}{
		{name: "list members newest first", want: []uuid.UUID{named.UUID, letter.UUID, underscore.UUID}, wantTotal: 3},
		{name: "status", filter: domain.SubscriberFilter{Status: domain.StatusActive}, want: []uuid.UUID{letter.UUID, underscore.UUID}, wantTotal: 2},
		{name: "email prefix treats underscore literally", filter: domain.SubscriberFilter{Query: prefix + "_"}, want: []uuid.UUID{underscore.UUID}, wantTotal: 1},
		{name: "percent is literal", filter: domain.SubscriberFilter{Query: prefix + "%"}, want: []uuid.UUID{}},
		{name: "display name prefix ignores case", filter: domain.SubscriberFilter{Query: " " + strings.ToUpper(prefix) + " CAR "}, want: []uuid.UUID{named.UUID}, wantTotal: 1},
		{name: "second page", filter: domain.SubscriberFilter{PageRequest: pagination.PageRequest{Page: 2, Limit: 2}}, want: []uuid.UUID{underscore.UUID}, wantTotal: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter := tt.filter
			filter.List = list

			if filter.PageRequest.Page == 0 {
				filter.PageRequest.Page, filter.PageRequest.Limit = 1, 10
			}

			page, err := repo.ListSubscribers(t.Context(), filter)
			require.NoError(t, err)
			require.Equal(t, tt.wantTotal, *page.Total)
			require.Equal(t, tt.want, subscriberIDs(page.Items))
			require.Equal(t, filter.PageRequest.Page, page.OffsetPage)
			require.Equal(t, filter.PageRequest.Limit, page.OffsetPerPage)
		})
	}

	_, err := repo.ListSubscribers(canceledContext(t), domain.SubscriberFilter{PageRequest: pagination.PageRequest{Page: 1, Limit: 10}})
	require.ErrorIs(t, err, context.Canceled)
}

func TestNewsletterRepositoryEraseSubscriber(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewNewsletterRepository(tx)
	ctx := t.Context()
	list := insertNewsletterList(t, tx, 0, nil)
	user := insertUser(t, tx)
	now := time.Now().UTC().Truncate(time.Microsecond)

	sub := createSubscriber(t, repo, func(s *domain.Subscriber) {
		s.DisplayName, s.UserID, s.IPHash, s.UserAgent = "Ann", &user, "ip-hash", "Firefox"
		s.Memberships = []domain.Membership{member(list, domain.MembershipActive)}
	})
	hash := tokenHash()
	require.NoError(t, repo.CreateToken(ctx, domain.Token{
		SubscriberID: sub.UUID, Purpose: domain.PurposePreferences, Hash: hash, ExpiresAt: now.Add(time.Hour), CreatedAt: now,
	}))
	require.NoError(t, repo.AppendConsent(ctx, domain.ConsentEvent{
		SubscriberID: sub.UUID, Event: domain.ConsentUnsubscribed, Source: domain.ConsentSourceToken,
		Feedback: "too many emails", IPHash: "ip-hash", OccurredAt: now,
	}))

	erasedAt := now.Add(time.Minute)
	require.NoError(t, repo.EraseSubscriber(ctx, sub.UUID, erasedAt))

	got, err := repo.Subscriber(ctx, sub.UUID)
	require.NoError(t, err)
	require.Equal(t, domain.StatusErased, got.Status)
	require.Empty(t, got.Email)
	require.Empty(t, got.DisplayName)
	require.Nil(t, got.UserID)
	require.Empty(t, got.IPHash)
	require.Empty(t, got.UserAgent)
	require.WithinDuration(t, erasedAt, *got.ErasedAt, 0)
	require.Equal(t, domain.MembershipLeft, got.Memberships[0].State)
	require.WithinDuration(t, erasedAt, *got.Memberships[0].LeftAt, 0)

	_, err = repo.Token(ctx, hash)
	require.ErrorIs(t, err, domain.ErrNotFound)

	history, err := repo.ConsentHistory(ctx, sub.UUID, 10)
	require.NoError(t, err)
	require.Len(t, history, 1)
	require.Equal(t, domain.ConsentUnsubscribed, history[0].Event, "the consent record itself is kept")
	require.Empty(t, history[0].Feedback)
	require.Empty(t, history[0].IPHash)

	_, err = repo.SubscriberByUser(ctx, user)
	require.ErrorIs(t, err, domain.ErrNotFound)

	require.ErrorIs(t, repo.EraseSubscriber(canceledContext(t), sub.UUID, erasedAt), context.Canceled)
}

func TestNewsletterRepositoryTokens(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewNewsletterRepository(tx)
	ctx := t.Context()
	list := insertNewsletterList(t, tx, 0, nil)
	sub := createSubscriber(t, repo, nil)
	issue := createIssue(t, repo, []string{list}, nil)
	now := time.Now().UTC().Truncate(time.Microsecond)

	confirm := domain.Token{SubscriberID: sub.UUID, Purpose: domain.PurposeConfirm, Hash: tokenHash(), ExpiresAt: now.Add(time.Hour), CreatedAt: now}
	unsubscribe := domain.Token{
		SubscriberID: sub.UUID, Purpose: domain.PurposeUnsubscribe, Hash: tokenHash(), IssueID: &issue.UUID,
		ExpiresAt: now.Add(time.Hour), CreatedAt: now,
	}
	require.NoError(t, repo.CreateToken(ctx, confirm))
	require.NoError(t, repo.CreateToken(ctx, unsubscribe))

	got, err := repo.Token(ctx, confirm.Hash)
	require.NoError(t, err)
	require.NotZero(t, got.ID)
	require.Equal(t, sub.UUID, got.SubscriberID)
	require.Equal(t, domain.PurposeConfirm, got.Purpose)
	require.Nil(t, got.IssueID)
	require.WithinDuration(t, confirm.ExpiresAt, got.ExpiresAt, 0)
	require.Nil(t, got.UsedAt)

	gotUnsubscribe, err := repo.Token(ctx, unsubscribe.Hash)
	require.NoError(t, err)
	require.Equal(t, &issue.UUID, gotUnsubscribe.IssueID)

	used, err := repo.UseToken(ctx, got.ID, now)
	require.NoError(t, err)
	require.True(t, used)

	used, err = repo.UseToken(ctx, got.ID, now.Add(time.Minute))
	require.NoError(t, err)
	require.False(t, used, "a token is used once")

	got, err = repo.Token(ctx, confirm.Hash)
	require.NoError(t, err)
	require.WithinDuration(t, now, *got.UsedAt, 0)

	require.NoError(t, repo.RevokeTokens(ctx, sub.UUID, domain.PurposeConfirm))

	_, err = repo.Token(ctx, confirm.Hash)
	require.ErrorIs(t, err, domain.ErrNotFound)

	_, err = repo.Token(ctx, unsubscribe.Hash)
	require.NoError(t, err, "other purposes are kept")

	inSavepoint(t, tx, func() {
		unknown := confirm
		unknown.SubscriberID, unknown.Hash = uuid.New(), tokenHash()
		require.ErrorContains(t, repo.CreateToken(ctx, unknown), "create newsletter token")
	})
}

func TestNewsletterRepositoryPruneTokens(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewNewsletterRepository(tx)
	sub := createSubscriber(t, repo, nil)

	// A century back, so no other row can fall before the cutoff.
	expiry := time.Now().UTC().AddDate(-100, 0, 0).Truncate(time.Microsecond)
	expired := domain.Token{SubscriberID: sub.UUID, Purpose: domain.PurposeConfirm, Hash: tokenHash(), ExpiresAt: expiry, CreatedAt: expiry}
	live := domain.Token{SubscriberID: sub.UUID, Purpose: domain.PurposeConfirm, Hash: tokenHash(), ExpiresAt: time.Now().Add(time.Hour), CreatedAt: expiry}
	require.NoError(t, repo.CreateToken(t.Context(), expired))
	require.NoError(t, repo.CreateToken(t.Context(), live))

	pruned, err := repo.PruneTokens(t.Context(), expiry.Add(time.Second))
	require.NoError(t, err)
	require.Equal(t, int64(1), pruned)

	_, err = repo.Token(t.Context(), expired.Hash)
	require.ErrorIs(t, err, domain.ErrNotFound)

	_, err = repo.Token(t.Context(), live.Hash)
	require.NoError(t, err)
}

func TestNewsletterRepositoryConsentHistory(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewNewsletterRepository(tx)
	ctx := t.Context()
	sub := createSubscriber(t, repo, nil)
	at := time.Now().UTC().Truncate(time.Microsecond)

	subscribed := domain.ConsentEvent{SubscriberID: sub.UUID, Event: domain.ConsentSubscribed, Source: "public", OccurredAt: at}
	joined := domain.ConsentEvent{
		SubscriberID: sub.UUID, Event: domain.ConsentListJoined, ListSlug: "weekly", Source: "public", OccurredAt: at.Add(time.Minute),
	}
	left := domain.ConsentEvent{
		SubscriberID: sub.UUID, Event: domain.ConsentUnsubscribed, Source: domain.ConsentSourceToken, ReasonCode: "other",
		Feedback: "too many", IPHash: "ip-hash", OccurredAt: at.Add(2 * time.Minute),
	}
	require.NoError(t, repo.AppendConsent(ctx, subscribed, joined, left))

	history, err := repo.ConsentHistory(ctx, sub.UUID, 2)
	require.NoError(t, err)
	require.Len(t, history, 2, "limited, newest first")
	require.Equal(t, left.Event, history[0].Event)
	require.Equal(t, "other", history[0].ReasonCode)
	require.Equal(t, "too many", history[0].Feedback)
	require.Equal(t, "ip-hash", history[0].IPHash)
	require.WithinDuration(t, left.OccurredAt, history[0].OccurredAt, 0)
	require.Equal(t, "weekly", history[1].ListSlug)

	history, err = repo.ConsentHistory(ctx, sub.UUID, 10)
	require.NoError(t, err)
	require.Len(t, history, 3)
	require.Equal(t, sub.UUID, history[2].SubscriberID)
	require.Empty(t, history[2].ListSlug)
	require.Empty(t, history[2].ReasonCode)

	inSavepoint(t, tx, func() {
		unknown := subscribed
		unknown.SubscriberID = uuid.New()
		require.ErrorContains(t, repo.AppendConsent(ctx, unknown), "append newsletter consent")
	})
}

func TestNewsletterRepositoryDatabaseErrors(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewNewsletterRepository(tx)
	ctx := canceledContext(t)
	id := uuid.New()

	tests := []struct {
		name string
		call func() error
	}{
		{name: "save lists", call: func() error { return repo.SaveLists(ctx, []domain.ListInput{{Slug: "x", Name: "x"}}, time.Now()) }},
		{name: "save config", call: func() error { return repo.SaveConfig(ctx, domain.DefaultConfig()) }},
		{name: "create subscriber", call: func() error { return repo.CreateSubscriber(ctx, newSubscriber(nil)) }},
		{name: "update subscriber", call: func() error { return repo.UpdateSubscriber(ctx, newSubscriber(nil)) }},
		{name: "create token", call: func() error { return repo.CreateToken(ctx, domain.Token{SubscriberID: id, Hash: tokenHash()}) }},
		{name: "token", call: func() error { _, err := repo.Token(ctx, tokenHash()); return err }},
		{name: "use token", call: func() error { _, err := repo.UseToken(ctx, 1, time.Now()); return err }},
		{name: "revoke tokens", call: func() error { return repo.RevokeTokens(ctx, id, domain.PurposeConfirm) }},
		{name: "prune tokens", call: func() error { _, err := repo.PruneTokens(ctx, time.Now()); return err }},
		{name: "append consent", call: func() error { return repo.AppendConsent(ctx, domain.ConsentEvent{SubscriberID: id}) }},
		{name: "consent history", call: func() error { _, err := repo.ConsentHistory(ctx, id, 1); return err }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.ErrorIs(t, tt.call(), context.Canceled)
		})
	}
}
