package config

import "testing"

func TestLoadSearchLanguage(t *testing.T) {
	setJWTKeys(t)
	t.Setenv("SEARCH_LANGUAGE", "")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	if cfg.SearchLanguage != "simple" {
		t.Fatalf("default SEARCH_LANGUAGE = %q, want simple", cfg.SearchLanguage)
	}

	t.Setenv("SEARCH_LANGUAGE", " English ")

	if cfg, err = Load(); err != nil || cfg.SearchLanguage != "english" {
		t.Fatalf("SEARCH_LANGUAGE=' English ' -> %q, %v", cfg.SearchLanguage, err)
	}

	for _, bad := range []string{"simple'; DROP TABLE posts", "pg_catalog.english", "1english"} {
		t.Setenv("SEARCH_LANGUAGE", bad)

		if _, err := Load(); err == nil {
			t.Fatalf("expected error for SEARCH_LANGUAGE=%q", bad)
		}
	}
}
