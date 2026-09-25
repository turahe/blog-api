package config

import (
	"strings"
	"testing"
)

func TestLoadBackgroundDoesNotNeedJWTKeys(t *testing.T) {
	t.Setenv("APP_JWT_PRIVATE_KEY", "")
	t.Setenv("APP_JWT_PRIVATE_KEY_PATH", "")
	t.Setenv("APP_JWT_PUBLIC_KEY", "")
	t.Setenv("APP_JWT_PUBLIC_KEY_PATH", "")

	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "JWT ES256 keys are required") {
		t.Fatalf("Load err=%v, want missing JWT keys", err)
	}

	cfg, err := LoadBackground()
	if err != nil {
		t.Fatal(err)
	}

	if cfg.JWTPrivateKey != "" || cfg.JWTPublicKey != "" {
		t.Fatal("background config must not carry JWT keys")
	}
}
