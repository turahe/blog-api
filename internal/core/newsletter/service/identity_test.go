package service

import "testing"

type prefixHasher struct{}

func (prefixHasher) MAC(value string) string { return "mac:" + value }

func TestHashIdentity(t *testing.T) {
	t.Parallel()

	keyed := New(Deps{IdentityHasher: prefixHasher{}}, Config{})
	if got := keyed.hashIdentity("203.0.113.9"); got != "mac:203.0.113.9" {
		t.Errorf("keyed hash = %q, want the hasher's MAC", got)
	}

	plain := New(Deps{}, Config{})
	if got := plain.hashIdentity("203.0.113.9"); len(got) != 64 || got == "203.0.113.9" {
		t.Errorf("unkeyed hash = %q, want a SHA-256 hex digest", got)
	}

	if got := keyed.hashIdentity(""); got != "" {
		t.Errorf("empty identity hashed to %q, want empty", got)
	}
}
