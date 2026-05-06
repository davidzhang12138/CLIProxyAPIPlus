package state

import "testing"

func TestIsSQLiteDSN(t *testing.T) {
	tests := []struct {
		dsn  string
		want bool
	}{
		{"sqlite:///data/app.db", true},
		{"sqlite://app.db", true},
		{"/data/app.db", true},
		{"./local.sqlite", true},
		{"data.db", true},
		{"postgresql://user:pass@localhost/db", false},
		{"host=localhost dbname=test", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := IsSQLiteDSN(tt.dsn); got != tt.want {
			t.Errorf("IsSQLiteDSN(%q) = %v, want %v", tt.dsn, got, tt.want)
		}
	}
}

func TestParseSQLitePath(t *testing.T) {
	tests := []struct {
		dsn  string
		want string
	}{
		{"sqlite:///data/app.db", "/data/app.db"},
		{"sqlite://app.db", "app.db"},
		{"/data/app.db", "/data/app.db"},
		{"./local.db", "./local.db"},
	}
	for _, tt := range tests {
		if got := ParseSQLitePath(tt.dsn); got != tt.want {
			t.Errorf("ParseSQLitePath(%q) = %q, want %q", tt.dsn, got, tt.want)
		}
	}
}
