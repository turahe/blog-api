package messaging

import (
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/getsentry/sentry-go"
)

// Tracing gives each consumed message its own Sentry hub and a transaction
// named after the router handler. It is a no-op when Sentry is not initialised.
func Tracing(h message.HandlerFunc) message.HandlerFunc {
	return func(msg *message.Message) ([]*message.Message, error) {
		if sentry.CurrentHub().Client() == nil {
			return h(msg)
		}

		ctx := sentry.SetHubOnContext(msg.Context(), sentry.CurrentHub().Clone())
		tx := sentry.StartTransaction(ctx, message.HandlerNameFromCtx(msg.Context()),
			sentry.WithOpName("queue.process"),
			sentry.WithTransactionSource(sentry.SourceTask),
		)
		tx.SetData("messaging.message.id", msg.UUID)

		defer tx.Finish()

		msg.SetContext(tx.Context())

		msgs, err := h(msg)
		if err != nil {
			tx.Status = sentry.SpanStatusInternalError
		} else {
			tx.Status = sentry.SpanStatusOK
		}

		return msgs, err
	}
}
