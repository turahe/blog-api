package messaging

import (
	"errors"
	"log/slog"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/sony/gobreaker"
)

// ErrCircuitOpen is returned without running the handler while its breaker is open. The
// message is neither retried nor dead-lettered: it is nacked and redelivered later.
var ErrCircuitOpen = errors.New("consumer circuit open")

// maxOpenPause caps how long a handler holds a message before nacking it while the breaker is
// open, so shutdown and redelivery stay responsive.
const maxOpenPause = 5 * time.Second

// BreakerConfig trips a consumer's breaker after Failures consecutive transient errors and
// keeps it open for OpenFor before letting one trial message through.
type BreakerConfig struct {
	Failures int
	OpenFor  time.Duration
}

// CircuitBreaker pauses a consumer whose downstream (SMTP, database) keeps failing, instead of
// burning each message's retries and dead-lettering it. Permanent errors do not count as
// failures. Failures below 1 disables the breaker.
func CircuitBreaker(name string, cfg BreakerConfig, logger *slog.Logger) message.HandlerMiddleware {
	if cfg.Failures < 1 {
		return func(h message.HandlerFunc) message.HandlerFunc { return h }
	}

	openFor := cfg.OpenFor
	if openFor <= 0 {
		openFor = 30 * time.Second
	}

	pause := min(openFor, maxOpenPause)
	breaker := gobreaker.NewCircuitBreaker(gobreaker.Settings{
		Name:    name,
		Timeout: openFor,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return int(counts.ConsecutiveFailures) >= cfg.Failures
		},
		IsSuccessful: func(err error) bool {
			return err == nil || errors.Is(err, ErrPermanent)
		},
		OnStateChange: func(name string, from, to gobreaker.State) {
			logger.Warn("consumer circuit breaker changed state",
				"consumer", name, "from", from.String(), "to", to.String())
		},
	})

	return func(h message.HandlerFunc) message.HandlerFunc {
		return func(msg *message.Message) ([]*message.Message, error) {
			var produced []*message.Message

			_, err := breaker.Execute(func() (any, error) {
				var handlerErr error

				produced, handlerErr = h(msg)

				return nil, handlerErr
			})
			if errors.Is(err, gobreaker.ErrOpenState) || errors.Is(err, gobreaker.ErrTooManyRequests) {
				select {
				case <-msg.Context().Done():
				case <-time.After(pause):
				}

				return nil, ErrCircuitOpen
			}

			return produced, err
		}
	}
}
