package config

import (
	"testing"
	"time"
)

func TestLoadImpersonationTTL(t *testing.T) {
	setJWTKeys(t)
	t.Setenv("IMPERSONATION_TTL", "")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	if cfg.ImpersonationTTL != time.Hour {
		t.Fatalf("default IMPERSONATION_TTL = %s, want 1h", cfg.ImpersonationTTL)
	}

	for _, bad := range []string{"4m", "121m", "0s"} {
		t.Setenv("IMPERSONATION_TTL", bad)

		if _, err := Load(); err == nil {
			t.Fatalf("expected error for IMPERSONATION_TTL=%s", bad)
		}
	}
}
