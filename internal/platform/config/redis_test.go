package config

import "testing"

func TestRedisURLFromSplitConfig(t *testing.T) {
	t.Parallel()

	cfg := Config{
		RedisDriver:   "valkey",
		RedisHost:     "cache.example.com",
		RedisPort:     6380,
		RedisPassword: "p@ss word",
		RedisDB:       2,
	}

	got := cfg.RedisURL()
	// Valkey speaks the Redis protocol; go-redis still uses the redis:// URL scheme.
	want := "redis://:p%40ss%20word@cache.example.com:6380/2"
	if got != want {
		t.Fatalf("RedisURL()=%q want %q", got, want)
	}
}

func TestValidateRedisAcceptsDrivers(t *testing.T) {
	t.Parallel()

	for _, driver := range []string{"redis", "valkey"} {
		cfg := Config{RedisDriver: driver, RedisHost: "127.0.0.1", RedisPort: 6379}
		if err := cfg.ValidateRedis(); err != nil {
			t.Fatalf("driver %q: %v", driver, err)
		}
	}
}

func TestValidateRedisRejectsRedissDriver(t *testing.T) {
	t.Parallel()
	cfg := Config{RedisDriver: "rediss", RedisHost: "127.0.0.1", RedisPort: 6379}
	if err := cfg.ValidateRedis(); err == nil {
		t.Fatal("expected error for REDIS_DRIVER=rediss")
	}
}

func TestLoadRedisFromSplitEnvironment(t *testing.T) {
	t.Setenv("REDIS_DRIVER", "valkey")
	t.Setenv("REDIS_HOST", "redis.internal")
	t.Setenv("REDIS_PORT", "6381")
	t.Setenv("REDIS_PASSWORD", "secret")
	t.Setenv("REDIS_DB", "3")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RedisDriver != "valkey" {
		t.Fatalf("RedisDriver=%q", cfg.RedisDriver)
	}
	if cfg.RedisHost != "redis.internal" {
		t.Fatalf("RedisHost=%q", cfg.RedisHost)
	}
	if cfg.RedisPort != 6381 {
		t.Fatalf("RedisPort=%d", cfg.RedisPort)
	}
	if cfg.RedisPassword != "secret" {
		t.Fatalf("RedisPassword=%q", cfg.RedisPassword)
	}
	if cfg.RedisDB != 3 {
		t.Fatalf("RedisDB=%d", cfg.RedisDB)
	}
}

func TestLoadRejectsInvalidRedisConfig(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
	}{
		{name: "driver", key: "REDIS_DRIVER", value: "http"},
		{name: "port", key: "REDIS_PORT", value: "70000"},
		{name: "database", key: "REDIS_DB", value: "-1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tt.key, tt.value)
			if _, err := Load(); err == nil {
				t.Fatal("expected Redis configuration error")
			}
		})
	}
}

func TestValidateRedisRequiresHost(t *testing.T) {
	t.Parallel()
	cfg := Config{RedisDriver: "redis", RedisPort: 6379}
	if err := cfg.ValidateRedis(); err == nil {
		t.Fatal("expected Redis host error")
	}
}
