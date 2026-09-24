package config

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestLoadEncryptionKeyOptional(t *testing.T) {
	setJWTKeys(t)
	t.Setenv("APP_ENCRYPTION_KEY", "")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	if cfg.EncryptionKey != "" || cfg.TwoFactorIssuer != "Blog" {
		t.Fatalf("key=%q issuer=%q", cfg.EncryptionKey, cfg.TwoFactorIssuer)
	}
}

func TestLoadEncryptionKeyValidated(t *testing.T) {
	setJWTKeys(t)
	t.Setenv("APP_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(make([]byte, 16)))

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "APP_ENCRYPTION_KEY") {
		t.Fatalf("err=%v, want a 32-byte key error", err)
	}

	t.Setenv("APP_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(make([]byte, 32)))

	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
}
