package database

import "testing"

func TestNormalizeDriver(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   string
		want string
	}{
		{"", "postgres"},
		{"postgres", "postgres"},
		{"PostgreSQL", "postgres"},
		{"pg", "postgres"},
		{"mysql", "mysql"},
		{"MariaDB", "mysql"},
		{"sqlserver", "sqlserver"},
		{"mssql", "sqlserver"},
	}
	for _, tc := range cases {
		got, err := NormalizeDriver(tc.in)
		if err != nil {
			t.Fatalf("NormalizeDriver(%q): %v", tc.in, err)
		}

		if got != tc.want {
			t.Fatalf("NormalizeDriver(%q)=%q want %q", tc.in, got, tc.want)
		}
	}

	if _, err := NormalizeDriver("oracle"); err == nil {
		t.Fatal("expected error for unsupported driver")
	}
}

func TestNormalizeMySQLDSN(t *testing.T) {
	t.Parallel()

	if got := normalizeMySQLDSN("mysql://user:pass@tcp(127.0.0.1:3306)/blog"); got != "user:pass@tcp(127.0.0.1:3306)/blog" {
		t.Fatalf("unexpected: %q", got)
	}

	if got := normalizeMySQLDSN("user:pass@tcp(127.0.0.1:3306)/blog"); got != "user:pass@tcp(127.0.0.1:3306)/blog" {
		t.Fatalf("unexpected: %q", got)
	}
}
