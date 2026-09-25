package domain

import "time"

// SaltBytes is the length of a daily visitor salt.
const SaltBytes = 32

// SaltDay is the UTC day whose salt keys anonymous visitor hashes at now.
func SaltDay(now time.Time) string {
	return now.UTC().Format(time.DateOnly)
}

// SaltKeepFrom is the oldest UTC day whose salt is kept at now: yesterday, so replicas whose
// clocks straddle midnight still agree. Older salts are destroyed.
func SaltKeepFrom(now time.Time) string {
	return now.UTC().AddDate(0, 0, -1).Format(time.DateOnly)
}
