package state

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	log "github.com/sirupsen/logrus"
)

var validSchemaName = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

// PostgresStateStore implements StateStore using PostgreSQL.
type PostgresStateStore struct {
	db     *sql.DB
	schema string
	ownsDB bool // true if we opened the connection and should close it
}

// NewPostgresStateStore creates a new PostgresStateStore with its own connection.
func NewPostgresStateStore(dsn string, schema string) (*PostgresStateStore, error) {
	if schema != "" && !validSchemaName.MatchString(schema) {
		return nil, fmt.Errorf("state store: invalid schema name %q (must match [a-zA-Z0-9_]+)", schema)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("state store: open database: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err = db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("state store: ping database: %w", err)
	}
	return &PostgresStateStore{db: db, schema: schema, ownsDB: true}, nil
}

// NewPostgresStateStoreFromDB creates a PostgresStateStore using an existing *sql.DB.
func NewPostgresStateStoreFromDB(db *sql.DB, schema string) (*PostgresStateStore, error) {
	if schema != "" && !validSchemaName.MatchString(schema) {
		return nil, fmt.Errorf("state store: invalid schema name %q (must match [a-zA-Z0-9_]+)", schema)
	}
	return &PostgresStateStore{db: db, schema: schema, ownsDB: false}, nil
}

func (s *PostgresStateStore) tableName(base string) string {
	if s.schema != "" {
		return fmt.Sprintf("%s.%s", s.schema, base)
	}
	return base
}

// Init creates the required tables.
func (s *PostgresStateStore) Init(ctx context.Context) error {
	schemaSQL := ""
	if s.schema != "" {
		schemaSQL = fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s;", s.schema)
	}

	ddl := schemaSQL + fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS %s (
    token_key   TEXT PRIMARY KEY,
    expires_at  TIMESTAMPTZ NOT NULL,
    reason      TEXT NOT NULL,
    updated_at  TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS %s (
    token_key       TEXT PRIMARY KEY,
    success_rate    DOUBLE PRECISION,
    avg_latency     DOUBLE PRECISION,
    quota_remaining DOUBLE PRECISION,
    last_used       TIMESTAMPTZ,
    fail_count      INT,
    total_requests  INT,
    success_count   INT DEFAULT 0,
    total_latency   DOUBLE PRECISION DEFAULT 0,
    updated_at      TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS %s (
    stat_date       TEXT NOT NULL PRIMARY KEY,
    total_requests  BIGINT DEFAULT 0,
    success_count   BIGINT DEFAULT 0,
    failure_count   BIGINT DEFAULT 0,
    total_tokens    BIGINT DEFAULT 0,
    updated_at      TIMESTAMPTZ DEFAULT NOW()
);`,
		s.tableName("runtime_cooldowns"),
		s.tableName("runtime_token_metrics"),
		s.tableName("runtime_request_stats"),
	)

	_, err := s.db.ExecContext(ctx, ddl)
	if err != nil {
		return fmt.Errorf("state store: init tables: %w", err)
	}
	log.Info("state store: tables initialized")
	return nil
}

// Close releases the database connection if we own it.
func (s *PostgresStateStore) Close() error {
	if s == nil || s.db == nil || !s.ownsDB {
		return nil
	}
	return s.db.Close()
}

// SaveCooldowns replaces all cooldown entries in a single transaction.
func (s *PostgresStateStore) SaveCooldowns(ctx context.Context, cooldowns []CooldownEntry) error {
	if len(cooldowns) == 0 {
		_, err := s.db.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s", s.tableName("runtime_cooldowns")))
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("state store: begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err = tx.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s", s.tableName("runtime_cooldowns"))); err != nil {
		return fmt.Errorf("state store: truncate cooldowns: %w", err)
	}

	stmt, err := tx.PrepareContext(ctx, fmt.Sprintf(
		"INSERT INTO %s (token_key, expires_at, reason, updated_at) VALUES ($1, $2, $3, NOW())",
		s.tableName("runtime_cooldowns"),
	))
	if err != nil {
		return fmt.Errorf("state store: prepare cooldown insert: %w", err)
	}
	defer stmt.Close()

	for _, c := range cooldowns {
		if _, err = stmt.ExecContext(ctx, c.TokenKey, c.ExpiresAt, c.Reason); err != nil {
			return fmt.Errorf("state store: insert cooldown %s: %w", c.TokenKey, err)
		}
	}

	return tx.Commit()
}

// LoadCooldowns reads all non-expired cooldown entries.
func (s *PostgresStateStore) LoadCooldowns(ctx context.Context) ([]CooldownEntry, error) {
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(
		"SELECT token_key, expires_at, reason FROM %s WHERE expires_at > NOW()",
		s.tableName("runtime_cooldowns"),
	))
	if err != nil {
		return nil, fmt.Errorf("state store: query cooldowns: %w", err)
	}
	defer rows.Close()

	var entries []CooldownEntry
	for rows.Next() {
		var e CooldownEntry
		if err = rows.Scan(&e.TokenKey, &e.ExpiresAt, &e.Reason); err != nil {
			return nil, fmt.Errorf("state store: scan cooldown: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// SaveTokenMetrics replaces all token metrics entries.
func (s *PostgresStateStore) SaveTokenMetrics(ctx context.Context, metrics []TokenMetricsEntry) error {
	if len(metrics) == 0 {
		_, err := s.db.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s", s.tableName("runtime_token_metrics")))
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("state store: begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err = tx.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s", s.tableName("runtime_token_metrics"))); err != nil {
		return fmt.Errorf("state store: truncate token metrics: %w", err)
	}

	stmt, err := tx.PrepareContext(ctx, fmt.Sprintf(
		`INSERT INTO %s (token_key, success_rate, avg_latency, quota_remaining, last_used, fail_count, total_requests, success_count, total_latency, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW())`,
		s.tableName("runtime_token_metrics"),
	))
	if err != nil {
		return fmt.Errorf("state store: prepare metrics insert: %w", err)
	}
	defer stmt.Close()

	for _, m := range metrics {
		if _, err = stmt.ExecContext(ctx, m.TokenKey, m.SuccessRate, m.AvgLatency, m.QuotaRemaining, m.LastUsed, m.FailCount, m.TotalRequests, m.SuccessCount, m.TotalLatency); err != nil {
			return fmt.Errorf("state store: insert metrics %s: %w", m.TokenKey, err)
		}
	}

	return tx.Commit()
}

// LoadTokenMetrics reads all token metrics entries.
func (s *PostgresStateStore) LoadTokenMetrics(ctx context.Context) ([]TokenMetricsEntry, error) {
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(
		"SELECT token_key, success_rate, avg_latency, quota_remaining, last_used, fail_count, total_requests, COALESCE(success_count, 0), COALESCE(total_latency, 0) FROM %s",
		s.tableName("runtime_token_metrics"),
	))
	if err != nil {
		return nil, fmt.Errorf("state store: query token metrics: %w", err)
	}
	defer rows.Close()

	var entries []TokenMetricsEntry
	for rows.Next() {
		var e TokenMetricsEntry
		if err = rows.Scan(&e.TokenKey, &e.SuccessRate, &e.AvgLatency, &e.QuotaRemaining, &e.LastUsed, &e.FailCount, &e.TotalRequests, &e.SuccessCount, &e.TotalLatency); err != nil {
			return nil, fmt.Errorf("state store: scan token metrics: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// SaveRequestStats replaces all daily request statistics.
func (s *PostgresStateStore) SaveRequestStats(ctx context.Context, stats []RequestStatsEntry) error {
	if len(stats) == 0 {
		_, err := s.db.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s", s.tableName("runtime_request_stats")))
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("state store: begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err = tx.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s", s.tableName("runtime_request_stats"))); err != nil {
		return fmt.Errorf("state store: truncate request stats: %w", err)
	}

	stmt, err := tx.PrepareContext(ctx, fmt.Sprintf(
		`INSERT INTO %s (stat_date, total_requests, success_count, failure_count, total_tokens, updated_at)
		 VALUES ($1, $2, $3, $4, $5, NOW())`,
		s.tableName("runtime_request_stats"),
	))
	if err != nil {
		return fmt.Errorf("state store: prepare stats insert: %w", err)
	}
	defer stmt.Close()

	for _, st := range stats {
		if _, err = stmt.ExecContext(ctx, st.StatDate, st.TotalRequests, st.SuccessCount, st.FailureCount, st.TotalTokens); err != nil {
			return fmt.Errorf("state store: insert stats %s: %w", st.StatDate, err)
		}
	}

	return tx.Commit()
}

// LoadRequestStats reads all request statistics entries.
func (s *PostgresStateStore) LoadRequestStats(ctx context.Context) ([]RequestStatsEntry, error) {
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(
		"SELECT stat_date, total_requests, success_count, failure_count, total_tokens FROM %s",
		s.tableName("runtime_request_stats"),
	))
	if err != nil {
		return nil, fmt.Errorf("state store: query request stats: %w", err)
	}
	defer rows.Close()

	var entries []RequestStatsEntry
	for rows.Next() {
		var e RequestStatsEntry
		if err = rows.Scan(&e.StatDate, &e.TotalRequests, &e.SuccessCount, &e.FailureCount, &e.TotalTokens); err != nil {
			return nil, fmt.Errorf("state store: scan request stats: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
