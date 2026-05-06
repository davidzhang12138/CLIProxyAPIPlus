package state

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"

	log "github.com/sirupsen/logrus"
)

// SQLiteStateStore implements StateStore using SQLite.
type SQLiteStateStore struct {
	db     *sql.DB
	ownsDB bool // true if we opened the connection and should close it
}

// NewSQLiteStateStore creates a new SQLiteStateStore with its own connection.
func NewSQLiteStateStore(dbPath string) (*SQLiteStateStore, error) {
	dsn := dbPath + "?_busy_timeout=5000&_journal_mode=WAL&_synchronous=NORMAL&_foreign_keys=ON"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("sqlite state store: open database: %w", err)
	}
	db.SetMaxOpenConns(1)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err = db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sqlite state store: ping database: %w", err)
	}
	return &SQLiteStateStore{db: db, ownsDB: true}, nil
}

// NewSQLiteStateStoreFromDB creates a SQLiteStateStore using an existing *sql.DB.
func NewSQLiteStateStoreFromDB(db *sql.DB) (*SQLiteStateStore, error) {
	return &SQLiteStateStore{db: db, ownsDB: false}, nil
}

// DB returns the underlying *sql.DB.
func (s *SQLiteStateStore) DB() *sql.DB {
	return s.db
}

// Init creates the required tables.
func (s *SQLiteStateStore) Init(ctx context.Context) error {
	ddl := `
CREATE TABLE IF NOT EXISTS runtime_cooldowns (
    token_key   TEXT PRIMARY KEY,
    expires_at  TEXT NOT NULL,
    reason      TEXT NOT NULL,
    updated_at  TEXT DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS runtime_token_metrics (
    token_key       TEXT PRIMARY KEY,
    success_rate    REAL,
    avg_latency     REAL,
    quota_remaining REAL,
    last_used       TEXT,
    fail_count      INTEGER,
    total_requests  INTEGER,
    success_count   INTEGER DEFAULT 0,
    total_latency   REAL DEFAULT 0,
    updated_at      TEXT DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS runtime_usage_snapshot (
    id          INTEGER PRIMARY KEY CHECK (id = 1),
    snapshot    TEXT NOT NULL,
    updated_at  TEXT DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS runtime_auth_cooldowns (
    id          INTEGER PRIMARY KEY CHECK (id = 1),
    snapshot    TEXT NOT NULL,
    updated_at  TEXT DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS runtime_unhealthy_urls (
    id          INTEGER PRIMARY KEY CHECK (id = 1),
    snapshot    TEXT NOT NULL,
    updated_at  TEXT DEFAULT (datetime('now'))
);`

	_, err := s.db.ExecContext(ctx, ddl)
	if err != nil {
		return fmt.Errorf("sqlite state store: init tables: %w", err)
	}
	log.Info("sqlite state store: tables initialized")
	return nil
}

// Close releases the database connection if we own it.
func (s *SQLiteStateStore) Close() error {
	if s == nil || s.db == nil || !s.ownsDB {
		return nil
	}
	return s.db.Close()
}

// SaveCooldowns replaces all cooldown entries in a single transaction.
func (s *SQLiteStateStore) SaveCooldowns(ctx context.Context, cooldowns []CooldownEntry) error {
	if len(cooldowns) == 0 {
		_, err := s.db.ExecContext(ctx, "DELETE FROM runtime_cooldowns")
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlite state store: begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err = tx.ExecContext(ctx, "DELETE FROM runtime_cooldowns"); err != nil {
		return fmt.Errorf("sqlite state store: truncate cooldowns: %w", err)
	}

	stmt, err := tx.PrepareContext(ctx,
		"INSERT INTO runtime_cooldowns (token_key, expires_at, reason, updated_at) VALUES (?, ?, ?, ?)",
	)
	if err != nil {
		return fmt.Errorf("sqlite state store: prepare cooldown insert: %w", err)
	}
	defer stmt.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	for _, c := range cooldowns {
		if _, err = stmt.ExecContext(ctx, c.TokenKey, c.ExpiresAt.UTC().Format(time.RFC3339), c.Reason, now); err != nil {
			return fmt.Errorf("sqlite state store: insert cooldown %s: %w", c.TokenKey, err)
		}
	}

	return tx.Commit()
}

// LoadCooldowns reads all non-expired cooldown entries.
func (s *SQLiteStateStore) LoadCooldowns(ctx context.Context) ([]CooldownEntry, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	rows, err := s.db.QueryContext(ctx,
		"SELECT token_key, expires_at, reason FROM runtime_cooldowns WHERE expires_at > ?", now,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite state store: query cooldowns: %w", err)
	}
	defer rows.Close()

	var entries []CooldownEntry
	for rows.Next() {
		var e CooldownEntry
		var expiresStr string
		if err = rows.Scan(&e.TokenKey, &expiresStr, &e.Reason); err != nil {
			return nil, fmt.Errorf("sqlite state store: scan cooldown: %w", err)
		}
		e.ExpiresAt, err = time.Parse(time.RFC3339, expiresStr)
		if err != nil {
			return nil, fmt.Errorf("sqlite state store: parse cooldown expires_at: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// SaveTokenMetrics replaces all token metrics entries.
func (s *SQLiteStateStore) SaveTokenMetrics(ctx context.Context, metrics []TokenMetricsEntry) error {
	if len(metrics) == 0 {
		_, err := s.db.ExecContext(ctx, "DELETE FROM runtime_token_metrics")
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlite state store: begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err = tx.ExecContext(ctx, "DELETE FROM runtime_token_metrics"); err != nil {
		return fmt.Errorf("sqlite state store: truncate token metrics: %w", err)
	}

	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO runtime_token_metrics (token_key, success_rate, avg_latency, quota_remaining, last_used, fail_count, total_requests, success_count, total_latency, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
	)
	if err != nil {
		return fmt.Errorf("sqlite state store: prepare metrics insert: %w", err)
	}
	defer stmt.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	for _, m := range metrics {
		if _, err = stmt.ExecContext(ctx, m.TokenKey, m.SuccessRate, m.AvgLatency, m.QuotaRemaining, m.LastUsed.UTC().Format(time.RFC3339), m.FailCount, m.TotalRequests, m.SuccessCount, m.TotalLatency, now); err != nil {
			return fmt.Errorf("sqlite state store: insert metrics %s: %w", m.TokenKey, err)
		}
	}

	return tx.Commit()
}

// LoadTokenMetrics reads all token metrics entries.
func (s *SQLiteStateStore) LoadTokenMetrics(ctx context.Context) ([]TokenMetricsEntry, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT token_key, success_rate, avg_latency, quota_remaining, last_used, fail_count, total_requests, COALESCE(success_count, 0), COALESCE(total_latency, 0) FROM runtime_token_metrics",
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite state store: query token metrics: %w", err)
	}
	defer rows.Close()

	var entries []TokenMetricsEntry
	for rows.Next() {
		var e TokenMetricsEntry
		var lastUsedStr string
		if err = rows.Scan(&e.TokenKey, &e.SuccessRate, &e.AvgLatency, &e.QuotaRemaining, &lastUsedStr, &e.FailCount, &e.TotalRequests, &e.SuccessCount, &e.TotalLatency); err != nil {
			return nil, fmt.Errorf("sqlite state store: scan token metrics: %w", err)
		}
		e.LastUsed, err = time.Parse(time.RFC3339, lastUsedStr)
		if err != nil {
			return nil, fmt.Errorf("sqlite state store: parse token metrics last_used: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// SaveUsageSnapshot upserts the full usage statistics as a JSON blob.
func (s *SQLiteStateStore) SaveUsageSnapshot(ctx context.Context, snapshot []byte) error {
	if len(snapshot) == 0 {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO runtime_usage_snapshot (id, snapshot, updated_at) VALUES (1, ?, ?)
		 ON CONFLICT (id) DO UPDATE SET snapshot = excluded.snapshot, updated_at = excluded.updated_at`,
		string(snapshot), now,
	)
	if err != nil {
		return fmt.Errorf("sqlite state store: save usage snapshot: %w", err)
	}
	return nil
}

// LoadUsageSnapshot reads the stored usage snapshot JSON blob.
func (s *SQLiteStateStore) LoadUsageSnapshot(ctx context.Context) ([]byte, error) {
	var data string
	err := s.db.QueryRowContext(ctx,
		"SELECT snapshot FROM runtime_usage_snapshot WHERE id = 1",
	).Scan(&data)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("sqlite state store: load usage snapshot: %w", err)
	}
	return []byte(data), nil
}

// SaveAuthCooldowns upserts the auth cooldown state as a JSON blob.
func (s *SQLiteStateStore) SaveAuthCooldowns(ctx context.Context, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO runtime_auth_cooldowns (id, snapshot, updated_at) VALUES (1, ?, ?)
		 ON CONFLICT (id) DO UPDATE SET snapshot = excluded.snapshot, updated_at = excluded.updated_at`,
		string(data), now,
	)
	if err != nil {
		return fmt.Errorf("sqlite state store: save auth cooldowns: %w", err)
	}
	return nil
}

// LoadAuthCooldowns reads the stored auth cooldown state JSON blob.
func (s *SQLiteStateStore) LoadAuthCooldowns(ctx context.Context) ([]byte, error) {
	var data string
	err := s.db.QueryRowContext(ctx,
		"SELECT snapshot FROM runtime_auth_cooldowns WHERE id = 1",
	).Scan(&data)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("sqlite state store: load auth cooldowns: %w", err)
	}
	return []byte(data), nil
}

// SaveUnhealthyURLs upserts the unhealthy URL state as a JSON blob.
func (s *SQLiteStateStore) SaveUnhealthyURLs(ctx context.Context, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO runtime_unhealthy_urls (id, snapshot, updated_at) VALUES (1, ?, ?)
		 ON CONFLICT (id) DO UPDATE SET snapshot = excluded.snapshot, updated_at = excluded.updated_at`,
		string(data), now,
	)
	if err != nil {
		return fmt.Errorf("sqlite state store: save unhealthy urls: %w", err)
	}
	return nil
}

// LoadUnhealthyURLs reads the stored unhealthy URL state JSON blob.
func (s *SQLiteStateStore) LoadUnhealthyURLs(ctx context.Context) ([]byte, error) {
	var data string
	err := s.db.QueryRowContext(ctx,
		"SELECT snapshot FROM runtime_unhealthy_urls WHERE id = 1",
	).Scan(&data)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("sqlite state store: load unhealthy urls: %w", err)
	}
	return []byte(data), nil
}
