package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadCacheDefaults(t *testing.T) {
	setJWTKeys(t)
	t.Setenv("CACHE_ENABLED", "")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	if !cfg.CacheEnabled || cfg.CacheBypassHeader {
		t.Fatalf("enabled=%v bypass=%v, want cache on and bypass header off", cfg.CacheEnabled, cfg.CacheBypassHeader)
	}

	if cfg.CacheTTLPosts != time.Minute || cfg.CacheTTLCategories != 10*time.Minute || cfg.CacheTTLTags != 10*time.Minute {
		t.Fatalf("ttls posts=%s categories=%s tags=%s", cfg.CacheTTLPosts, cfg.CacheTTLCategories, cfg.CacheTTLTags)
	}
}

func TestLoadCacheFromEnv(t *testing.T) {
	setJWTKeys(t)
	t.Setenv("CACHE_ENABLED", "false")
	t.Setenv("CACHE_BYPASS_HEADER", "true")
	t.Setenv("CACHE_TTL_POSTS", "0")
	t.Setenv("CACHE_TTL_TAGS", "30s")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	if cfg.CacheEnabled || !cfg.CacheBypassHeader || cfg.CacheTTLPosts != 0 || cfg.CacheTTLTags != 30*time.Second {
		t.Fatalf("unexpected cache config: enabled=%v bypass=%v posts=%s tags=%s",
			cfg.CacheEnabled, cfg.CacheBypassHeader, cfg.CacheTTLPosts, cfg.CacheTTLTags)
	}
}

func TestLoadRejectsNegativeCacheTTL(t *testing.T) {
	setJWTKeys(t)
	t.Setenv("CACHE_TTL_CATEGORIES", "-1s")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "CACHE_TTL_CATEGORIES") {
		t.Fatalf("err=%v", err)
	}
}
