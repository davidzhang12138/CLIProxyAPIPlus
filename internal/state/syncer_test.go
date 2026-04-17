package state

import (
	"context"
	"sync"
	"testing"
	"time"
)

// mockStateStore is an in-memory StateStore for testing.
type mockStateStore struct {
	mu            sync.Mutex
	cooldowns     []CooldownEntry
	metrics       []TokenMetricsEntry
	usageSnapshot []byte
	initErr       error
	saveErr       error
	loadErr       error
}

func (m *mockStateStore) Init(ctx context.Context) error { return m.initErr }
func (m *mockStateStore) Close() error                   { return nil }

func (m *mockStateStore) SaveCooldowns(_ context.Context, c []CooldownEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.saveErr != nil {
		return m.saveErr
	}
	m.cooldowns = make([]CooldownEntry, len(c))
	copy(m.cooldowns, c)
	return nil
}

func (m *mockStateStore) LoadCooldowns(_ context.Context) ([]CooldownEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.loadErr != nil {
		return nil, m.loadErr
	}
	out := make([]CooldownEntry, len(m.cooldowns))
	copy(out, m.cooldowns)
	return out, nil
}

func (m *mockStateStore) SaveTokenMetrics(_ context.Context, met []TokenMetricsEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.saveErr != nil {
		return m.saveErr
	}
	m.metrics = make([]TokenMetricsEntry, len(met))
	copy(m.metrics, met)
	return nil
}

func (m *mockStateStore) LoadTokenMetrics(_ context.Context) ([]TokenMetricsEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.loadErr != nil {
		return nil, m.loadErr
	}
	out := make([]TokenMetricsEntry, len(m.metrics))
	copy(out, m.metrics)
	return out, nil
}

func (m *mockStateStore) SaveUsageSnapshot(_ context.Context, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.saveErr != nil {
		return m.saveErr
	}
	m.usageSnapshot = make([]byte, len(data))
	copy(m.usageSnapshot, data)
	return nil
}

func (m *mockStateStore) LoadUsageSnapshot(_ context.Context) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.loadErr != nil {
		return nil, m.loadErr
	}
	if m.usageSnapshot == nil {
		return nil, nil
	}
	out := make([]byte, len(m.usageSnapshot))
	copy(out, m.usageSnapshot)
	return out, nil
}

func TestSyncer_FlushAndLoad(t *testing.T) {
	store := &mockStateStore{}

	cooldowns := []CooldownEntry{
		{TokenKey: "tok-1", ExpiresAt: time.Now().Add(10 * time.Minute), Reason: "rate_limit"},
	}
	usageJSON := []byte(`{"total_requests":100,"success_count":90}`)

	var imported []CooldownEntry
	var importedUsage []byte

	syncer := NewSyncer(store, SyncerConfig{FlushInterval: 50 * time.Millisecond})
	syncer.ExportCooldowns = func() []CooldownEntry { return cooldowns }
	syncer.ImportCooldowns = func(e []CooldownEntry) { imported = e }
	syncer.ExportUsage = func() []byte { return usageJSON }
	syncer.ImportUsage = func(data []byte) { importedUsage = data }

	syncer.Start()
	time.Sleep(120 * time.Millisecond)
	syncer.Stop()

	store.mu.Lock()
	if len(store.cooldowns) != 1 {
		t.Errorf("expected 1 cooldown in store, got %d", len(store.cooldowns))
	}
	if len(store.usageSnapshot) == 0 {
		t.Error("expected usage snapshot in store")
	}
	store.mu.Unlock()

	// Simulate restart
	syncer2 := NewSyncer(store, SyncerConfig{FlushInterval: time.Hour})
	syncer2.ImportCooldowns = func(e []CooldownEntry) { imported = e }
	syncer2.ImportUsage = func(data []byte) { importedUsage = data }

	syncer2.LoadState(context.Background())

	if len(imported) != 1 {
		t.Errorf("expected 1 imported cooldown, got %d", len(imported))
	}
	if imported[0].TokenKey != "tok-1" {
		t.Errorf("expected token key tok-1, got %s", imported[0].TokenKey)
	}
	if len(importedUsage) == 0 {
		t.Error("expected usage snapshot to be restored")
	}
}

func TestSyncer_NilExporters(t *testing.T) {
	store := &mockStateStore{}
	syncer := NewSyncer(store, SyncerConfig{FlushInterval: 50 * time.Millisecond})

	syncer.Start()
	time.Sleep(80 * time.Millisecond)
	syncer.Stop()

	syncer2 := NewSyncer(store, SyncerConfig{})
	syncer2.LoadState(context.Background())
}

func TestSyncer_DefaultFlushInterval(t *testing.T) {
	syncer := NewSyncer(&mockStateStore{}, SyncerConfig{})
	if syncer.cfg.FlushInterval != 30*time.Second {
		t.Errorf("expected default flush interval 30s, got %s", syncer.cfg.FlushInterval)
	}
}

func TestSyncer_GracefulShutdown(t *testing.T) {
	store := &mockStateStore{}
	flushCount := 0

	syncer := NewSyncer(store, SyncerConfig{FlushInterval: time.Hour})
	syncer.ExportCooldowns = func() []CooldownEntry {
		flushCount++
		return []CooldownEntry{{TokenKey: "tok", ExpiresAt: time.Now().Add(time.Hour), Reason: "test"}}
	}

	syncer.Start()
	syncer.Stop()

	if flushCount != 1 {
		t.Errorf("expected 1 final flush on stop, got %d", flushCount)
	}
	store.mu.Lock()
	if len(store.cooldowns) != 1 {
		t.Errorf("expected 1 cooldown after final flush, got %d", len(store.cooldowns))
	}
	store.mu.Unlock()
}
