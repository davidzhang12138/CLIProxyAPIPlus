package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/state"
)

type stubStateStore struct{}

func (s *stubStateStore) Init(context.Context) error                                 { return nil }
func (s *stubStateStore) SaveCooldowns(context.Context, []state.CooldownEntry) error { return nil }
func (s *stubStateStore) LoadCooldowns(context.Context) ([]state.CooldownEntry, error) {
	return nil, nil
}
func (s *stubStateStore) SaveTokenMetrics(context.Context, []state.TokenMetricsEntry) error {
	return nil
}
func (s *stubStateStore) LoadTokenMetrics(context.Context) ([]state.TokenMetricsEntry, error) {
	return nil, nil
}
func (s *stubStateStore) SaveUsageSnapshot(context.Context, []byte) error { return nil }
func (s *stubStateStore) LoadUsageSnapshot(context.Context) ([]byte, error) {
	return nil, nil
}
func (s *stubStateStore) SaveAuthCooldowns(context.Context, []byte) error { return nil }
func (s *stubStateStore) LoadAuthCooldowns(context.Context) ([]byte, error) {
	return nil, nil
}
func (s *stubStateStore) SaveUnhealthyURLs(context.Context, []byte) error { return nil }
func (s *stubStateStore) LoadUnhealthyURLs(context.Context) ([]byte, error) {
	return nil, nil
}
func (s *stubStateStore) Close() error { return nil }

func TestNewStateStoreWithRetryConfigRetriesUntilSuccess(t *testing.T) {
	t.Parallel()

	var attempts int
	var sleeps []time.Duration
	store, err := newStateStoreWithRetryConfig("dsn", "public", stateStoreRetryConfig{
		Attempts:     4,
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     250 * time.Millisecond,
		Factory: func(string, string) (state.StateStore, error) {
			attempts++
			if attempts < 3 {
				return nil, errors.New("temporary failure")
			}
			return &stubStateStore{}, nil
		},
		Sleep: func(d time.Duration) {
			sleeps = append(sleeps, d)
		},
	})
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if store == nil {
		t.Fatal("expected non-nil store")
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
	if len(sleeps) != 2 {
		t.Fatalf("sleep calls = %d, want 2", len(sleeps))
	}
	if sleeps[0] != 100*time.Millisecond {
		t.Fatalf("first sleep = %s, want 100ms", sleeps[0])
	}
	if sleeps[1] != 200*time.Millisecond {
		t.Fatalf("second sleep = %s, want 200ms", sleeps[1])
	}
}

func TestNewStateStoreWithRetryConfigReturnsLastError(t *testing.T) {
	t.Parallel()

	var attempts int
	var sleeps []time.Duration
	_, err := newStateStoreWithRetryConfig("dsn", "public", stateStoreRetryConfig{
		Attempts:     3,
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     150 * time.Millisecond,
		Factory: func(string, string) (state.StateStore, error) {
			attempts++
			return nil, errors.New("db down")
		},
		Sleep: func(d time.Duration) {
			sleeps = append(sleeps, d)
		},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
	if len(sleeps) != 2 {
		t.Fatalf("sleep calls = %d, want 2", len(sleeps))
	}
	if !strings.Contains(err.Error(), "db down") {
		t.Fatalf("error = %q, want to contain db down", err.Error())
	}
	if !strings.Contains(err.Error(), "after 3 attempts") {
		t.Fatalf("error = %q, want attempt summary", err.Error())
	}
}
