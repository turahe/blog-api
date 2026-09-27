package domain_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/core/newsletter/domain"
)

func TestParseFormat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   string
		want    domain.Format
		wantErr error
	}{
		{name: "empty is html", value: "", want: domain.FormatHTML},
		{name: "html", value: "html", want: domain.FormatHTML},
		{name: "plaintext", value: "plaintext", want: domain.FormatPlaintext},
		{name: "unknown", value: "pdf", wantErr: domain.ErrValidation},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := domain.ParseFormat(tt.value)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestNormalizeEmail(t *testing.T) {
	t.Parallel()

	email, err := domain.NormalizeEmail("  Reader@Example.TEST ")
	require.NoError(t, err)
	require.Equal(t, "reader@example.test", email)

	for _, bad := range []string{"", "no-at", "Name <a@example.test>", "a@example.test\r\nBcc: x@y.z", strings.Repeat("a", 250) + "@x.io"} {
		_, err := domain.NormalizeEmail(bad)
		require.ErrorIs(t, err, domain.ErrValidation, bad)
	}
}

func TestMaskEmail(t *testing.T) {
	t.Parallel()

	require.Equal(t, "a**@example.test", domain.MaskEmail("ann@example.test"))
	require.Equal(t, "r*****@example.test", domain.MaskEmail("reader@example.test"))
	require.Empty(t, domain.MaskEmail(""))
}

func TestValidateLists(t *testing.T) {
	t.Parallel()

	weekly := domain.ListInput{Slug: "weekly", Name: "Weekly", IsDefault: true}
	require.NoError(t, domain.ValidateLists([]domain.ListInput{weekly, {Slug: "product-news", Name: "Product"}}))

	for name, lists := range map[string][]domain.ListInput{
		"empty":      nil,
		"no default": {{Slug: "weekly", Name: "Weekly"}},
		"repeated":   {weekly, weekly},
		"bad slug":   {{Slug: "Weekly!", Name: "Weekly", IsDefault: true}},
		"no name":    {{Slug: "weekly", Name: " ", IsDefault: true}},
	} {
		require.ErrorIs(t, domain.ValidateLists(lists), domain.ErrValidation, name)
	}
}

func TestValidateListsBounds(t *testing.T) {
	t.Parallel()

	many := make([]domain.ListInput, domain.MaxLists+1)
	for i := range many {
		many[i] = domain.ListInput{Slug: "list-" + strings.Repeat("a", i+1), Name: "List", IsDefault: true}
	}

	tests := []struct {
		name    string
		lists   []domain.ListInput
		wantErr error
	}{
		{name: "longest slug", lists: []domain.ListInput{{Slug: strings.Repeat("a", 64), Name: "A", IsDefault: true}}},
		{name: "slug too long", lists: []domain.ListInput{{Slug: strings.Repeat("a", 65), Name: "A", IsDefault: true}}, wantErr: domain.ErrValidation},
		{name: "name too long", lists: []domain.ListInput{{Slug: "a", Name: strings.Repeat("n", 101), IsDefault: true}}, wantErr: domain.ErrValidation},
		{
			name:  "longest description",
			lists: []domain.ListInput{{Slug: "a", Name: "A", Description: strings.Repeat("d", 500), IsDefault: true}},
		},
		{
			name:    "description too long",
			lists:   []domain.ListInput{{Slug: "a", Name: "A", Description: strings.Repeat("d", 501), IsDefault: true}},
			wantErr: domain.ErrValidation,
		},
		{name: "too many lists", lists: many, wantErr: domain.ErrValidation},
		{name: "most lists", lists: many[:domain.MaxLists]},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := domain.ValidateLists(tt.lists)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestSubscriberMemberships(t *testing.T) {
	t.Parallel()

	now := time.Now()
	sub := domain.Subscriber{Memberships: []domain.Membership{{ListSlug: "weekly", State: domain.MembershipPending}}}

	sub.SetMembership("weekly", domain.MembershipActive, now)
	sub.SetMembership("news", domain.MembershipActive, now)
	require.True(t, sub.MembershipsModified())
	require.Equal(t, []string{"weekly", "news"}, sub.ActiveLists())

	sub.SetMembership("news", domain.MembershipLeft, now)
	m, ok := sub.Membership("news")
	require.True(t, ok)
	require.Equal(t, domain.MembershipLeft, m.State)
	require.NotNil(t, m.LeftAt)
	require.Equal(t, []string{"weekly"}, sub.ActiveLists())

	require.True(t, domain.Subscriber{Status: domain.StatusComplained}.Suppressed())
	require.False(t, domain.Subscriber{Status: domain.StatusUnsubscribed}.Suppressed())
}

func TestMembershipStamps(t *testing.T) {
	t.Parallel()

	joined := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	left := joined.Add(24 * time.Hour)
	again := left.Add(24 * time.Hour)

	sub := domain.Subscriber{}
	_, ok := sub.Membership("weekly")
	require.False(t, ok, "no membership before joining")
	require.False(t, sub.MembershipsModified())

	sub.SetMembership("weekly", domain.MembershipActive, joined)
	sub.SetMembership("weekly", domain.MembershipActive, left)
	m, _ := sub.Membership("weekly")
	require.Equal(t, joined, *m.JoinedAt, "setting the same state keeps the first stamp")

	sub.SetMembership("weekly", domain.MembershipLeft, left)
	m, _ = sub.Membership("weekly")
	require.Equal(t, left, *m.LeftAt)

	sub.SetMembership("weekly", domain.MembershipPending, again)
	m, _ = sub.Membership("weekly")
	require.Equal(t, domain.MembershipPending, m.State)
	require.Nil(t, m.LeftAt, "a pending rejoin clears the left stamp")
	require.Equal(t, joined, *m.JoinedAt, "pending keeps the earlier join stamp")
	require.Empty(t, sub.ActiveLists())
}

func TestConfig(t *testing.T) {
	t.Parallel()

	cfg := domain.DefaultConfig()
	require.NoError(t, cfg.Validate())
	require.ErrorIs(t, cfg.ReadyToSend(), domain.ErrNotConfigured, "postal address is required to send")

	cfg.PostalAddress = "1 Example Street"
	require.NoError(t, cfg.ReadyToSend())

	cfg.FromName = "Evil\r\nBcc: x"
	require.ErrorIs(t, cfg.Validate(), domain.ErrValidation)

	cfg = domain.DefaultConfig()
	cfg.ConfirmTTL = 10 * 24 * time.Hour
	require.ErrorIs(t, cfg.Validate(), domain.ErrValidation)
}

func TestConfigValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		edit    func(c *domain.Config)
		wantErr error
	}{
		{name: "defaults"},
		{
			name: "full sender settings",
			edit: func(c *domain.Config) {
				c.FromName, c.FromEmail, c.ReplyTo = "Example", "news@example.test", "reply@example.test"
			},
		},
		{name: "name with quotes", edit: func(c *domain.Config) { c.FromName = `"Example"` }, wantErr: domain.ErrValidation},
		{name: "name too long", edit: func(c *domain.Config) { c.FromName = strings.Repeat("n", 101) }, wantErr: domain.ErrValidation},
		{name: "bad from email", edit: func(c *domain.Config) { c.FromEmail = "nope" }, wantErr: domain.ErrValidation},
		{name: "bad reply-to", edit: func(c *domain.Config) { c.ReplyTo = "Reply <r@example.test>" }, wantErr: domain.ErrValidation},
		{name: "postal address too long", edit: func(c *domain.Config) { c.PostalAddress = strings.Repeat("p", 501) }, wantErr: domain.ErrValidation},
		{name: "shortest confirmation window", edit: func(c *domain.Config) { c.ConfirmTTL = domain.MinConfirmTTL }},
		{name: "confirmation window too short", edit: func(c *domain.Config) { c.ConfirmTTL = time.Minute }, wantErr: domain.ErrValidation},
		{name: "longest confirmation window", edit: func(c *domain.Config) { c.ConfirmTTL = domain.MaxConfirmTTL }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := domain.DefaultConfig()
			if tt.edit != nil {
				tt.edit(&cfg)
			}

			err := cfg.Validate()
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestIssueTransitions(t *testing.T) {
	t.Parallel()

	require.True(t, domain.CanMove(domain.IssueDraft, domain.IssueQueued))
	require.True(t, domain.CanMove(domain.IssueSending, domain.IssueQueued), "resume")
	require.False(t, domain.CanMove(domain.IssueQueued, domain.IssueDraft))
	require.False(t, domain.CanMove(domain.IssueSent, domain.IssueCancelled))
	require.True(t, domain.IssueScheduled.Editable())
	require.False(t, domain.IssueQueued.Editable())

	issue := domain.Issue{Subject: "Hello", BodyMarkdown: "Body", Lists: []string{"weekly"}}
	require.NoError(t, issue.Validate())

	issue.Subject = "Two\nlines"
	require.ErrorIs(t, issue.Validate(), domain.ErrValidation)

	issue = domain.Issue{Subject: "Hello", BodyMarkdown: "Body"}
	require.ErrorIs(t, issue.Validate(), domain.ErrValidation, "needs a list")
}

func TestValidateFeedback(t *testing.T) {
	t.Parallel()

	require.NoError(t, domain.ValidateFeedback("", ""))
	require.NoError(t, domain.ValidateFeedback("not_relevant", "too many posts"))
	require.ErrorIs(t, domain.ValidateFeedback("spam", ""), domain.ErrValidation)
	require.ErrorIs(t, domain.ValidateFeedback("", strings.Repeat("x", 1001)), domain.ErrValidation)
}
