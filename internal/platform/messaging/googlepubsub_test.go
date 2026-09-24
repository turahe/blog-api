package messaging

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestGoogleCredentialsOptions(t *testing.T) {
	t.Parallel()

	t.Run("empty source uses adc", func(t *testing.T) {
		t.Parallel()

		opts, err := googleCredentialsOptions("")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(opts) != 0 {
			t.Fatalf("expected no client options, got %d", len(opts))
		}
	})

	t.Run("missing credentials file", func(t *testing.T) {
		t.Parallel()

		missing := filepath.Join(t.TempDir(), "missing.json")

		opts, err := googleCredentialsOptions("path:" + missing)
		if err == nil {
			t.Fatal("expected error")
		}

		if opts != nil {
			t.Fatalf("expected nil client options, got %v", opts)
		}

		if !strings.Contains(err.Error(), "credentials file") {
			t.Fatalf("expected missing file error, got %v", err)
		}
	})
}
