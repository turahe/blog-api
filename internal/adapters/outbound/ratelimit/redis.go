// Package ratelimit implements fixed-window request limits backed by Redis.
package ratelimit

import (
	"context"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// Redis is a fixed-window counter: one key per (key, window), expiring with the window.
type Redis struct {
	client goredis.Cmdable
	prefix string
}

// NewRedis returns a limiter storing counters under the ratelimit: key prefix.
func NewRedis(client goredis.Cmdable) *Redis {
	return &Redis{client: client, prefix: "ratelimit:"}
}

// Allow counts one hit for key and reports whether it is within limit for the current window.
// retryAfter is the time left in the window when the hit is rejected.
func (r *Redis) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, time.Duration, error) {
	fullKey := r.prefix + key
	pipe := r.client.TxPipeline()
	incr := pipe.Incr(ctx, fullKey)
	pipe.ExpireNX(ctx, fullKey, window)

	ttl := pipe.PTTL(ctx, fullKey)
	if _, err := pipe.Exec(ctx); err != nil {
		return false, 0, err
	}

	if incr.Val() <= int64(limit) {
		return true, 0, nil
	}

	retryAfter := ttl.Val()
	if retryAfter <= 0 {
		retryAfter = window
	}

	return false, retryAfter, nil
}
