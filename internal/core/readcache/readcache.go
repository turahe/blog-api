// Package readcache is the port for caching public read results. Services read
// through it and invalidate whole families after writes; a nil Cache disables caching.
package readcache

import (
	"context"
	"fmt"
	"strings"
)

// Family groups keys that are invalidated together.
type Family string

// Read families.
const (
	Posts      Family = "posts"
	Categories Family = "categories"
	Tags       Family = "tags"
	Users      Family = "users"
	Settings   Family = "settings"
)

// Families lists every family, e.g. for TTL configuration and diagnostics.
var Families = []Family{Posts, Categories, Tags, Users, Settings}

// Cache stores JSON-serialisable read results. Implementations fail open: lookup
// errors are misses and write/invalidate errors are logged, never returned.
type Cache interface {
	// Get decodes the cached value for key into dst and reports whether it was found.
	// On a miss, fill stores a freshly loaded value for the family's TTL. fill is bound to
	// the family state seen by Get, so a value loaded before a concurrent Invalidate is
	// never served after it. fill may be nil when the value should not be stored.
	Get(ctx context.Context, family Family, key string, dst any) (hit bool, fill func(value any))
	// Invalidate drops every key of the given families.
	Invalidate(ctx context.Context, families ...Family)
}

type bypassKey struct{}

// WithBypass marks ctx so reads skip the cache entirely (no lookup, no fill).
func WithBypass(ctx context.Context) context.Context {
	return context.WithValue(ctx, bypassKey{}, true)
}

// Bypassed reports whether ctx was marked with WithBypass.
func Bypassed(ctx context.Context) bool {
	bypass, _ := ctx.Value(bypassKey{}).(bool)
	return bypass
}

// Through returns the cached value for key, or calls load and caches a successful result.
func Through[T any](ctx context.Context, cache Cache, family Family, key string, load func() (T, error)) (T, error) {
	if cache == nil || Bypassed(ctx) {
		return load()
	}

	var cached T

	hit, fill := cache.Get(ctx, family, key, &cached)
	if hit {
		return cached, nil
	}

	value, err := load()
	if err == nil && fill != nil {
		fill(value)
	}

	return value, err
}

// Invalidate drops the families when cache is configured.
func Invalidate(ctx context.Context, cache Cache, families ...Family) {
	if cache != nil {
		cache.Invalidate(ctx, families...)
	}
}

// Key joins normalised query parts into a stable key, e.g. Key("list", "page", 1) = "list:page=1".
// Parts after the name come in name/value pairs; nil and empty values render as "-".
func Key(name string, pairs ...any) string {
	var b strings.Builder

	b.WriteString(name)

	for i := 0; i+1 < len(pairs); i += 2 {
		value := fmt.Sprint(pairs[i+1])
		if value == "" || value == "<nil>" {
			value = "-"
		}

		fmt.Fprintf(&b, ":%v=%s", pairs[i], value)
	}

	return b.String()
}
