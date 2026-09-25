package config

import (
	"testing"
	"time"
)

func TestLoadAuditDefaults(t *testing.T) {
	setJWTKeys(t)
	t.Setenv("AUDIT_RETENTION_DAYS", "")
	t.Setenv("AUDIT_QUEUE_SIZE", "")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	if cfg.AuditRetention() != 395*24*time.Hour || cfg.AuditQueueSize != 1024 {
		t.Fatalf("retention=%s queue=%d", cfg.AuditRetention(), cfg.AuditQueueSize)
	}
}

func TestLoadRejectsNonPositiveAuditSettings(t *testing.T) {
	setJWTKeys(t)
	t.Setenv("AUDIT_RETENTION_DAYS", "0")
	t.Setenv("AUDIT_QUEUE_SIZE", "10")

	if _, err := Load(); err == nil {
		t.Fatal("expected error for AUDIT_RETENTION_DAYS=0")
	}

	t.Setenv("AUDIT_RETENTION_DAYS", "30")
	t.Setenv("AUDIT_QUEUE_SIZE", "-1")

	if _, err := Load(); err == nil {
		t.Fatal("expected error for AUDIT_QUEUE_SIZE=-1")
	}
}
