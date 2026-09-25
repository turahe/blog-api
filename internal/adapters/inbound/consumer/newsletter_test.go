package consumer_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/adapters/inbound/consumer"
	nldomain "github.com/turahe/blog-api/internal/core/newsletter/domain"
	"github.com/turahe/blog-api/internal/platform/messaging"
)

type fakeNewsletter struct {
	got uuid.UUID
	err error
}

func (f *fakeNewsletter) Dispatch(_ context.Context, id uuid.UUID) error {
	f.got = id
	return f.err
}

func (f *fakeNewsletter) SyncSubscriber(_ context.Context, id uuid.UUID) error {
	f.got = id
	return f.err
}

func TestNewsletterDispatchPassesIssueID(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	nl := &fakeNewsletter{}
	msg := message.NewMessage(uuid.NewString(), fmt.Appendf(nil, `{"issue_id":%q}`, id))

	require.NoError(t, consumer.NewsletterDispatch(nl)(msg))
	require.Equal(t, id, nl.got)
}

func TestNewsletterDispatchErrors(t *testing.T) {
	t.Parallel()

	payload := fmt.Appendf(nil, `{"issue_id":%q}`, uuid.New())
	cases := map[string]struct {
		payload   []byte
		err       error
		permanent bool
	}{
		"bad json":         {[]byte(`not json`), nil, true},
		"missing id":       {[]byte(`{"issue_id":"x"}`), nil, true},
		"not configured":   {payload, nldomain.ErrNotConfigured, true},
		"pending retries":  {payload, fmt.Errorf("issue: %w", nldomain.ErrDeliveriesPending), false},
		"transient":        {payload, errors.New("db down"), false},
		"permanent sender": {payload, fmt.Errorf("%w: rejected", nldomain.ErrPermanent), true},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := consumer.NewsletterDispatch(&fakeNewsletter{err: tc.err})(message.NewMessage(uuid.NewString(), tc.payload))
			require.Error(t, err)
			require.Equal(t, tc.permanent, errors.Is(err, messaging.ErrPermanent))
		})
	}
}

func TestNewsletterSyncPassesSubscriberID(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	nl := &fakeNewsletter{}
	msg := message.NewMessage(uuid.NewString(), fmt.Appendf(nil, `{"subscriber_id":%q,"status":"active"}`, id))

	require.NoError(t, consumer.NewsletterSync(nl)(msg))
	require.Equal(t, id, nl.got)
}
