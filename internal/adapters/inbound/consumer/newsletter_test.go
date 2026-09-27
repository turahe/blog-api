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

func TestNewsletterSyncErrors(t *testing.T) {
	t.Parallel()

	payload := fmt.Appendf(nil, `{"subscriber_id":%q}`, uuid.New())

	tests := []struct {
		name      string
		payload   []byte
		err       error
		permanent bool
	}{
		{name: "bad json", payload: []byte(`not json`), permanent: true},
		{name: "missing id", payload: []byte(`{"issue_id":"` + uuid.NewString() + `"}`), permanent: true},
		{name: "validation", payload: payload, err: fmt.Errorf("sync: %w", nldomain.ErrValidation), permanent: true},
		{name: "transient", payload: payload, err: errors.New("provider down")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			nl := &fakeNewsletter{err: tt.err}
			err := consumer.NewsletterSync(nl)(message.NewMessage(uuid.NewString(), tt.payload))
			require.Error(t, err)
			require.Equal(t, tt.permanent, errors.Is(err, messaging.ErrPermanent))

			if tt.err != nil {
				require.ErrorIs(t, err, tt.err)
			} else {
				require.Equal(t, uuid.Nil, nl.got, "an invalid payload never reaches the service")
			}
		})
	}
}
