// Package consumer adapts broker messages to core service calls for app worker.
package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/google/uuid"
	nldomain "github.com/turahe/blog-api/internal/core/newsletter/domain"
	"github.com/turahe/blog-api/internal/platform/messaging"
)

// NewsletterDispatcher delivers a queued issue.
type NewsletterDispatcher interface {
	Dispatch(ctx context.Context, issueID uuid.UUID) error
}

// NewsletterSyncer pushes a subscriber's current state to the provider.
type NewsletterSyncer interface {
	SyncSubscriber(ctx context.Context, id uuid.UUID) error
}

// NewsletterDispatch handles blog.newsletter.issue.send_requested. An error wrapping
// ErrDeliveriesPending is retried with the router's backoff; once retries run out the issue
// stays sending until an editor queues it again.
func NewsletterDispatch(nl NewsletterDispatcher) message.NoPublishHandlerFunc {
	return func(msg *message.Message) error {
		id, err := payloadID(msg, "issue_id")
		if err != nil {
			return err
		}

		return permanent(nl.Dispatch(msg.Context(), id))
	}
}

// NewsletterSync handles blog.newsletter.subscriber.changed for providers with a contact store.
func NewsletterSync(nl NewsletterSyncer) message.NoPublishHandlerFunc {
	return func(msg *message.Message) error {
		id, err := payloadID(msg, "subscriber_id")
		if err != nil {
			return err
		}

		return permanent(nl.SyncSubscriber(msg.Context(), id))
	}
}

func payloadID(msg *message.Message, field string) (uuid.UUID, error) {
	var payload map[string]any
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return uuid.Nil, fmt.Errorf("%w: payload is not JSON", messaging.ErrPermanent)
	}

	raw, _ := payload[field].(string)

	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: payload has no valid %s", messaging.ErrPermanent, field)
	}

	return id, nil
}

// permanent marks errors that retrying cannot fix.
func permanent(err error) error {
	if errors.Is(err, nldomain.ErrPermanent) || errors.Is(err, nldomain.ErrNotConfigured) || errors.Is(err, nldomain.ErrValidation) {
		return fmt.Errorf("%w: %w", messaging.ErrPermanent, err)
	}

	return err
}
