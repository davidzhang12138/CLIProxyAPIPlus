package state

import (
	"path/filepath"
	"strings"
)

// IsSQLiteDSN returns true if the DSN looks like a SQLite path.
// Matches: "sqlite://...", or paths ending in ".db" or ".sqlite".
func IsSQLiteDSN(dsn string) bool {
	if strings.HasPrefix(dsn, "sqlite://") {
		return true
	}
	ext := strings.ToLower(filepath.Ext(dsn))
	return ext == ".db" || ext == ".sqlite"
}

// ParseSQLitePath extracts the file path from a SQLite DSN.
func ParseSQLitePath(dsn string) string {
	if strings.HasPrefix(dsn, "sqlite://") {
		return strings.TrimPrefix(dsn, "sqlite://")
	}
	return dsn
}
