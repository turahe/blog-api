package domain_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/core/newsletter/domain"
)

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
