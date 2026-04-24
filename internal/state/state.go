package state

import (
	"context"
	"time"
)

// StateStore defines the persistence interface for runtime state.
type StateStore interface {
	// Init creates required tables/schema.
	Init(ctx context.Context) error

	// Cooldowns
	SaveCooldowns(ctx context.Context, cooldowns []CooldownEntry) error
	LoadCooldowns(ctx context.Context) ([]CooldownEntry, error)

	// Token Metrics
	SaveTokenMetrics(ctx context.Context, metrics []TokenMetricsEntry) error
	LoadTokenMetrics(ctx context.Context) ([]TokenMetricsEntry, error)

	// Usage Snapshot (full JSON blob)
	SaveUsageSnapshot(ctx context.Context, snapshot []byte) error
	LoadUsageSnapshot(ctx context.Context) ([]byte, error)

	// Auth Cooldown State (full JSON blob)
	SaveAuthCooldowns(ctx context.Context, data []byte) error
	LoadAuthCooldowns(ctx context.Context) ([]byte, error)

	// Unhealthy URLs (full JSON blob)
	SaveUnhealthyURLs(ctx context.Context, data []byte) error
	LoadUnhealthyURLs(ctx context.Context) ([]byte, error)

	// Close releases resources.
	Close() error
}

// CooldownEntry represents a persisted cooldown record.
type CooldownEntry struct {
	TokenKey  string
	ExpiresAt time.Time
	Reason    string
}

// TokenMetricsEntry represents persisted token performance metrics.
type TokenMetricsEntry struct {
	TokenKey       string
	SuccessRate    float64
	AvgLatency     float64
	QuotaRemaining float64
	LastUsed       time.Time
	FailCount      int
	TotalRequests  int
	SuccessCount   int
	TotalLatency   float64
}
