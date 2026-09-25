package config

import (
	"fmt"
	"time"
)

// Impersonation session lifetime bounds; sessions are never renewed.
const (
	MinImpersonationTTL = 5 * time.Minute
	MaxImpersonationTTL = 2 * time.Hour
)

// ValidateImpersonation keeps IMPERSONATION_TTL within the hard ceiling.
func (c Config) ValidateImpersonation() error {
	if c.ImpersonationTTL < MinImpersonationTTL || c.ImpersonationTTL > MaxImpersonationTTL {
		return fmt.Errorf("IMPERSONATION_TTL must be between %s and %s (got %s)",
			MinImpersonationTTL, MaxImpersonationTTL, c.ImpersonationTTL)
	}

	return nil
}
