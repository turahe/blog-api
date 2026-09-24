package rbac

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// PolicyChannel is the Redis channel on which instances announce policy writes.
const PolicyChannel = "rbac:policy:reload"

// PolicySync keeps an instance's enforcer current when other instances change
// the policy: it reloads on every announcement from a peer and, as a fallback
// for missed messages or direct edits to casbin_rules, on a fixed interval.
type PolicySync struct {
	enforcer *Enforcer
	client   *redis.Client
	interval time.Duration
	logger   *slog.Logger
	instance string

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewPolicySync returns a PolicySync. A nil client disables announcements and
// interval <= 0 disables the timer.
func NewPolicySync(enforcer *Enforcer, client *redis.Client, interval time.Duration, logger *slog.Logger) *PolicySync {
	return &PolicySync{
		enforcer: enforcer, client: client, interval: interval, logger: logger,
		instance: uuid.NewString(),
	}
}

// Notify announces a policy write to the other instances. Delivery is best
// effort: a failure is logged and peers catch up on their next interval reload.
func (s *PolicySync) Notify(ctx context.Context) {
	if s.client == nil {
		return
	}

	if err := s.client.Publish(ctx, PolicyChannel, s.instance).Err(); err != nil {
		s.logger.WarnContext(ctx, "rbac policy announcement failed", "error", err)
	}
}

// Start runs the subscriber and the interval reloader until Stop.
func (s *PolicySync) Start(ctx context.Context) {
	ctx, s.cancel = context.WithCancel(ctx)

	if s.client != nil {
		sub := s.client.Subscribe(ctx, PolicyChannel)

		s.wg.Go(func() {
			defer func() { _ = sub.Close() }()

			messages := sub.Channel()

			for {
				select {
				case <-ctx.Done():
					return
				case msg, ok := <-messages:
					if !ok {
						return
					}

					if msg.Payload != s.instance {
						s.reload(ctx, "announcement")
					}
				}
			}
		})
	}

	if s.interval > 0 {
		s.wg.Go(func() {
			ticker := time.NewTicker(s.interval)
			defer ticker.Stop()

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					s.reload(ctx, "interval")
				}
			}
		})
	}
}

// Stop ends the background reloads and waits for them to exit.
func (s *PolicySync) Stop() {
	if s.cancel == nil {
		return
	}

	s.cancel()
	s.wg.Wait()
}

func (s *PolicySync) reload(ctx context.Context, trigger string) {
	if err := s.enforcer.Reload(); err != nil {
		s.logger.WarnContext(ctx, "rbac policy reload failed", "trigger", trigger, "error", err)
	}
}
