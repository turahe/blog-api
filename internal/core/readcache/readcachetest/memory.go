// Package readcachetest provides an in-memory readcache.Cache for service tests.
package readcachetest

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/turahe/blog-api/internal/core/readcache"
)

// Memory is a JSON round-tripping cache that records invalidations per family.
type Memory struct {
	mu          sync.Mutex
	entries     map[readcache.Family]map[string][]byte
	invalidated map[readcache.Family]int
}

// New returns an empty Memory cache.
func New() *Memory {
	return &Memory{entries: map[readcache.Family]map[string][]byte{}, invalidated: map[readcache.Family]int{}}
}

// Get implements readcache.Cache.
func (m *Memory) Get(_ context.Context, family readcache.Family, key string, dst any) (bool, func(any)) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if raw, ok := m.entries[family][key]; ok {
		return json.Unmarshal(raw, dst) == nil, nil
	}

	generation := m.invalidated[family]

	return false, func(value any) {
		raw, err := json.Marshal(value)
		if err != nil {
			return
		}

		m.mu.Lock()
		defer m.mu.Unlock()

		if m.invalidated[family] != generation {
			return
		}

		if m.entries[family] == nil {
			m.entries[family] = map[string][]byte{}
		}

		m.entries[family][key] = raw
	}
}

// Invalidate implements readcache.Cache.
func (m *Memory) Invalidate(_ context.Context, families ...readcache.Family) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, family := range families {
		delete(m.entries, family)
		m.invalidated[family]++
	}
}

// Invalidations returns how many times family was invalidated.
func (m *Memory) Invalidations(family readcache.Family) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.invalidated[family]
}

// Keys returns the cached keys of family.
func (m *Memory) Keys(family readcache.Family) []string {
	m.mu.Lock()
	defer m.mu.Unlock()

	keys := make([]string, 0, len(m.entries[family]))
	for key := range m.entries[family] {
		keys = append(keys, key)
	}

	return keys
}
