package state

import (
	"context"
	"testing"
	"time"
)

func newTestSQLiteStateStore(t *testing.T) *SQLiteStateStore {
	t.Helper()
	store, err := NewSQLiteStateStore(":memory:")
	if err != nil {
		t.Fatalf("failed to create SQLite state store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	if err := store.Init(context.Background()); err != nil {
		t.Fatalf("failed to init SQLite state store: %v", err)
	}
	return store
}

func TestSQLiteStateStore_CooldownsRoundTrip(t *testing.T) {
	store := newTestSQLiteStateStore(t)
	ctx := context.Background()

	cooldowns := []CooldownEntry{
		{TokenKey: "key1", ExpiresAt: time.Now().Add(1 * time.Hour).UTC().Truncate(time.Second), Reason: "rate-limited"},
		{TokenKey: "key2", ExpiresAt: time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second), Reason: "quota-exceeded"},
	}

	if err := store.SaveCooldowns(ctx, cooldowns); err != nil {
		t.Fatalf("SaveCooldowns: %v", err)
	}

	loaded, err := store.LoadCooldowns(ctx)
	if err != nil {
		t.Fatalf("LoadCooldowns: %v", err)
	}

	if len(loaded) != 2 {
		t.Fatalf("expected 2 cooldowns, got %d", len(loaded))
	}

	// Build a map for order-independent comparison.
	m := make(map[string]CooldownEntry)
	for _, c := range loaded {
		m[c.TokenKey] = c
	}

	for _, want := range cooldowns {
		got, ok := m[want.TokenKey]
		if !ok {
			t.Fatalf("missing cooldown for key %s", want.TokenKey)
		}
		if !got.ExpiresAt.Equal(want.ExpiresAt) {
			t.Errorf("key %s: expires_at = %v, want %v", want.TokenKey, got.ExpiresAt, want.ExpiresAt)
		}
		if got.Reason != want.Reason {
			t.Errorf("key %s: reason = %q, want %q", want.TokenKey, got.Reason, want.Reason)
		}
	}
}

func TestSQLiteStateStore_CooldownsEmpty(t *testing.T) {
	store := newTestSQLiteStateStore(t)
	ctx := context.Background()

	// Save nil (clears any existing data).
	if err := store.SaveCooldowns(ctx, nil); err != nil {
		t.Fatalf("SaveCooldowns(nil): %v", err)
	}

	loaded, err := store.LoadCooldowns(ctx)
	if err != nil {
		t.Fatalf("LoadCooldowns: %v", err)
	}

	if len(loaded) != 0 {
		t.Fatalf("expected 0 cooldowns, got %d", len(loaded))
	}
}

func TestSQLiteStateStore_ExpiredCooldownsFiltered(t *testing.T) {
	store := newTestSQLiteStateStore(t)
	ctx := context.Background()

	cooldowns := []CooldownEntry{
		{TokenKey: "expired", ExpiresAt: time.Now().Add(-1 * time.Hour).UTC().Truncate(time.Second), Reason: "old"},
		{TokenKey: "active", ExpiresAt: time.Now().Add(1 * time.Hour).UTC().Truncate(time.Second), Reason: "fresh"},
	}

	if err := store.SaveCooldowns(ctx, cooldowns); err != nil {
		t.Fatalf("SaveCooldowns: %v", err)
	}

	loaded, err := store.LoadCooldowns(ctx)
	if err != nil {
		t.Fatalf("LoadCooldowns: %v", err)
	}

	if len(loaded) != 1 {
		t.Fatalf("expected 1 active cooldown, got %d", len(loaded))
	}
	if loaded[0].TokenKey != "active" {
		t.Errorf("expected key 'active', got %q", loaded[0].TokenKey)
	}
}

func TestSQLiteStateStore_TokenMetricsRoundTrip(t *testing.T) {
	store := newTestSQLiteStateStore(t)
	ctx := context.Background()

	lastUsed := time.Now().Add(-10 * time.Minute).UTC().Truncate(time.Second)
	metrics := []TokenMetricsEntry{
		{
			TokenKey:       "tok1",
			SuccessRate:    0.95,
			AvgLatency:     123.45,
			QuotaRemaining: 500,
			LastUsed:       lastUsed,
			FailCount:      2,
			TotalRequests:  100,
			SuccessCount:   98,
			TotalLatency:   12345.0,
		},
	}

	if err := store.SaveTokenMetrics(ctx, metrics); err != nil {
		t.Fatalf("SaveTokenMetrics: %v", err)
	}

	loaded, err := store.LoadTokenMetrics(ctx)
	if err != nil {
		t.Fatalf("LoadTokenMetrics: %v", err)
	}

	if len(loaded) != 1 {
		t.Fatalf("expected 1 metric entry, got %d", len(loaded))
	}

	got := loaded[0]
	if got.TokenKey != "tok1" {
		t.Errorf("TokenKey = %q, want %q", got.TokenKey, "tok1")
	}
	if got.SuccessRate != 0.95 {
		t.Errorf("SuccessRate = %v, want 0.95", got.SuccessRate)
	}
	if got.AvgLatency != 123.45 {
		t.Errorf("AvgLatency = %v, want 123.45", got.AvgLatency)
	}
	if got.QuotaRemaining != 500 {
		t.Errorf("QuotaRemaining = %v, want 500", got.QuotaRemaining)
	}
	if !got.LastUsed.Equal(lastUsed) {
		t.Errorf("LastUsed = %v, want %v", got.LastUsed, lastUsed)
	}
	if got.FailCount != 2 {
		t.Errorf("FailCount = %d, want 2", got.FailCount)
	}
	if got.TotalRequests != 100 {
		t.Errorf("TotalRequests = %d, want 100", got.TotalRequests)
	}
	if got.SuccessCount != 98 {
		t.Errorf("SuccessCount = %d, want 98", got.SuccessCount)
	}
	if got.TotalLatency != 12345.0 {
		t.Errorf("TotalLatency = %v, want 12345.0", got.TotalLatency)
	}
}

func TestSQLiteStateStore_UsageSnapshotRoundTrip(t *testing.T) {
	store := newTestSQLiteStateStore(t)
	ctx := context.Background()

	snapshot := []byte(`{"total_requests":42,"total_tokens":12345}`)

	if err := store.SaveUsageSnapshot(ctx, snapshot); err != nil {
		t.Fatalf("SaveUsageSnapshot: %v", err)
	}

	loaded, err := store.LoadUsageSnapshot(ctx)
	if err != nil {
		t.Fatalf("LoadUsageSnapshot: %v", err)
	}

	if string(loaded) != string(snapshot) {
		t.Errorf("snapshot = %s, want %s", loaded, snapshot)
	}
}

func TestSQLiteStateStore_UsageSnapshotEmpty(t *testing.T) {
	store := newTestSQLiteStateStore(t)
	ctx := context.Background()

	loaded, err := store.LoadUsageSnapshot(ctx)
	if err != nil {
		t.Fatalf("LoadUsageSnapshot: %v", err)
	}

	if loaded != nil {
		t.Errorf("expected nil snapshot, got %s", loaded)
	}
}

func TestSQLiteStateStore_AuthCooldownsRoundTrip(t *testing.T) {
	store := newTestSQLiteStateStore(t)
	ctx := context.Background()

	data := []byte(`{"provider":"openai","cooldown_until":"2025-12-31T23:59:59Z"}`)

	if err := store.SaveAuthCooldowns(ctx, data); err != nil {
		t.Fatalf("SaveAuthCooldowns: %v", err)
	}

	loaded, err := store.LoadAuthCooldowns(ctx)
	if err != nil {
		t.Fatalf("LoadAuthCooldowns: %v", err)
	}

	if string(loaded) != string(data) {
		t.Errorf("auth cooldowns = %s, want %s", loaded, data)
	}
}

func TestSQLiteStateStore_UnhealthyURLsRoundTrip(t *testing.T) {
	store := newTestSQLiteStateStore(t)
	ctx := context.Background()

	data := []byte(`["https://api.example.com/v1","https://api.example.com/v2"]`)

	if err := store.SaveUnhealthyURLs(ctx, data); err != nil {
		t.Fatalf("SaveUnhealthyURLs: %v", err)
	}

	loaded, err := store.LoadUnhealthyURLs(ctx)
	if err != nil {
		t.Fatalf("LoadUnhealthyURLs: %v", err)
	}

	if string(loaded) != string(data) {
		t.Errorf("unhealthy urls = %s, want %s", loaded, data)
	}
}

func TestSQLiteStateStore_UpsertOverwrites(t *testing.T) {
	store := newTestSQLiteStateStore(t)
	ctx := context.Background()

	v1 := []byte(`{"version":1}`)
	v2 := []byte(`{"version":2}`)

	if err := store.SaveUsageSnapshot(ctx, v1); err != nil {
		t.Fatalf("SaveUsageSnapshot v1: %v", err)
	}
	if err := store.SaveUsageSnapshot(ctx, v2); err != nil {
		t.Fatalf("SaveUsageSnapshot v2: %v", err)
	}

	loaded, err := store.LoadUsageSnapshot(ctx)
	if err != nil {
		t.Fatalf("LoadUsageSnapshot: %v", err)
	}

	if string(loaded) != string(v2) {
		t.Errorf("snapshot = %s, want %s (v2 should overwrite v1)", loaded, v2)
	}
}
