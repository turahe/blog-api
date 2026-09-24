package database

import "testing"

func TestNormalizeDriver(t *testing.T) {
	t.Parallel()

	for _, in := range []string{"", "postgres", "PostgreSQL", "pg"} {
		got, err := NormalizeDriver(in)
		if err != nil {
			t.Fatalf("NormalizeDriver(%q): %v", in, err)
		}

		if got != DriverPostgres {
			t.Fatalf("NormalizeDriver(%q)=%q want %q", in, got, DriverPostgres)
		}
	}

	for _, in := range []string{"mysql", "mariadb", "sqlserver", "mssql", "oracle"} {
		if _, err := NormalizeDriver(in); err == nil {
			t.Fatalf("NormalizeDriver(%q): expected error", in)
		}
	}
}
