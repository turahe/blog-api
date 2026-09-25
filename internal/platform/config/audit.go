package config

import (
	"errors"
	"time"
)

const hoursPerDay = 24

// AuditRetention is how long audit entries are kept before pruning.
func (c Config) AuditRetention() time.Duration {
	return time.Duration(c.AuditRetentionDays) * hoursPerDay * time.Hour
}

// ValidateAudit requires a positive retention and queue size.
func (c Config) ValidateAudit() error {
	if c.AuditRetentionDays < 1 {
		return errors.New("AUDIT_RETENTION_DAYS must be positive")
	}

	if c.AuditQueueSize < 1 {
		return errors.New("AUDIT_QUEUE_SIZE must be positive")
	}

	return nil
}
