package ratelimit

import (
	"context"
	"time"

	goredis "github.com/redis/go-redis/v9"
	authports "github.com/turahe/blog-api/internal/core/auth/ports"
)

var _ authports.LoginAttempts = (*LoginLockout)(nil)

// LoginLockout locks a key for lockFor after maxFailures failed logins. The
// failure counter lives as long as one lock period, so failures spread wider
// than that never lock.
type LoginLockout struct {
	client      goredis.Cmdable
	maxFailures int
	lockFor     time.Duration
}

// NewLoginLockout returns a Redis-backed authports.LoginAttempts.
func NewLoginLockout(client goredis.Cmdable, maxFailures int, lockFor time.Duration) *LoginLockout {
	if lockFor <= 0 {
		lockFor = 15 * time.Minute
	}

	return &LoginLockout{client: client, maxFailures: maxFailures, lockFor: lockFor}
}

func (l *LoginLockout) failKey(key string) string { return "login:fail:" + key }
func (l *LoginLockout) lockKey(key string) string { return "login:lock:" + key }

// Locked implements authports.LoginAttempts.
func (l *LoginLockout) Locked(ctx context.Context, key string) (time.Duration, error) {
	ttl, err := l.client.PTTL(ctx, l.lockKey(key)).Result()
	if err != nil {
		return 0, err
	}

	if ttl < 0 {
		return 0, nil
	}

	return ttl, nil
}

// Fail implements authports.LoginAttempts.
func (l *LoginLockout) Fail(ctx context.Context, key string) (time.Duration, error) {
	pipe := l.client.TxPipeline()
	incr := pipe.Incr(ctx, l.failKey(key))
	pipe.ExpireNX(ctx, l.failKey(key), l.lockFor)

	if _, err := pipe.Exec(ctx); err != nil {
		return 0, err
	}

	if incr.Val() < int64(l.maxFailures) {
		return 0, nil
	}

	pipe = l.client.TxPipeline()
	pipe.Set(ctx, l.lockKey(key), "1", l.lockFor)
	pipe.Del(ctx, l.failKey(key))

	if _, err := pipe.Exec(ctx); err != nil {
		return 0, err
	}

	return l.lockFor, nil
}

// Reset implements authports.LoginAttempts.
func (l *LoginLockout) Reset(ctx context.Context, key string) error {
	return l.client.Del(ctx, l.failKey(key)).Err()
}
