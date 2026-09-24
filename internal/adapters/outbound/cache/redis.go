// Package cache implements readcache.Cache on Redis with per-family generation keys.
package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"github.com/turahe/blog-api/internal/core/readcache"
)

// SchemaVersion prefixes every key; bump it when a cached value's shape changes so
// old entries are ignored instead of decoded into the new shape.
const SchemaVersion = 1

const keyPrefix = "cache:public"

// Redis stores entries at cache:public:v{SchemaVersion}:{family}:g{generation}:{key}.
// Invalidate moves the family to a new generation; old entries expire by TTL.
type Redis struct {
	client goredis.Cmdable
	ttl    map[readcache.Family]time.Duration
	logger *slog.Logger
	now    func() time.Time
}

// NewRedis returns a cache with the per-family TTLs; families with a zero TTL are not cached.
func NewRedis(client goredis.Cmdable, ttl map[readcache.Family]time.Duration, logger *slog.Logger) *Redis {
	if logger == nil {
		logger = slog.Default()
	}

	return &Redis{client: client, ttl: ttl, logger: logger, now: time.Now}
}

// Get implements readcache.Cache.
func (r *Redis) Get(ctx context.Context, family readcache.Family, key string, dst any) (bool, func(any)) {
	ttl := r.ttl[family]
	if ttl <= 0 {
		return false, nil
	}

	generation, err := r.generation(ctx, family)
	if err != nil {
		r.warn("cache generation lookup failed", family, err)
		return false, nil
	}

	entryKey := r.entryKey(family, generation, key)

	raw, err := r.client.Get(ctx, entryKey).Bytes()
	switch {
	case err == nil:
		if err := json.Unmarshal(raw, dst); err == nil {
			return true, nil
		}

		r.warn("cache entry decode failed", family, err)
	case !errors.Is(err, goredis.Nil):
		r.warn("cache lookup failed", family, err)
		return false, nil
	}

	return false, func(value any) {
		encoded, err := json.Marshal(value)
		if err != nil {
			r.warn("cache entry encode failed", family, err)
			return
		}

		if err := r.client.Set(ctx, entryKey, encoded, ttl).Err(); err != nil {
			r.warn("cache fill failed", family, err)
		}
	}
}

// Invalidate implements readcache.Cache.
func (r *Redis) Invalidate(ctx context.Context, families ...readcache.Family) {
	if len(families) == 0 {
		return
	}

	pipe := r.client.Pipeline()
	for _, family := range families {
		genKey := r.generationKey(family)
		pipe.SetNX(ctx, genKey, r.seed(), 0)
		pipe.Incr(ctx, genKey)
	}

	if _, err := pipe.Exec(ctx); err != nil {
		r.logger.WarnContext(ctx, "cache invalidation failed; entries expire by TTL", "families", families, "error", err)
	}
}

// Probe writes, reads, and deletes a throwaway key to prove the cache is usable.
func (r *Redis) Probe(ctx context.Context) error {
	key := fmt.Sprintf("%s:v%d:probe:%d", keyPrefix, SchemaVersion, r.now().UnixNano())
	if err := r.client.Set(ctx, key, "ok", time.Minute).Err(); err != nil {
		return fmt.Errorf("cache probe write: %w", err)
	}

	got, err := r.client.Get(ctx, key).Result()
	if err != nil {
		return fmt.Errorf("cache probe read: %w", err)
	}

	if got != "ok" {
		return fmt.Errorf("cache probe read back %q", got)
	}

	return r.client.Del(ctx, key).Err()
}

// generation returns the family's current generation, seeding it when absent. Seeds
// come from the clock so a lost generation key never reuses a number whose entries live on.
func (r *Redis) generation(ctx context.Context, family readcache.Family) (int64, error) {
	genKey := r.generationKey(family)

	generation, err := r.client.Get(ctx, genKey).Int64()
	if !errors.Is(err, goredis.Nil) {
		return generation, err
	}

	if err := r.client.SetNX(ctx, genKey, r.seed(), 0).Err(); err != nil {
		return 0, err
	}

	return r.client.Get(ctx, genKey).Int64()
}

func (r *Redis) seed() string {
	return strconv.FormatInt(r.now().UnixNano(), 10)
}

func (r *Redis) generationKey(family readcache.Family) string {
	return fmt.Sprintf("%s:v%d:%s:gen", keyPrefix, SchemaVersion, family)
}

func (r *Redis) entryKey(family readcache.Family, generation int64, key string) string {
	return fmt.Sprintf("%s:v%d:%s:g%d:%s", keyPrefix, SchemaVersion, family, generation, key)
}

func (r *Redis) warn(msg string, family readcache.Family, err error) {
	r.logger.Warn(msg, "family", family, "error", err)
}
