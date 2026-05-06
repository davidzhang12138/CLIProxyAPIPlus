package auth

import (
	"context"
	"testing"
	"time"
)

func TestExportCooldownStates_IncludesMetrics(t *testing.T) {
	mgr := NewManager(nil, nil, nil)
	auth := &Auth{
		ID:       "auth-metrics",
		Provider: "antigravity",
		Attributes: map[string]string{
			"runtime_only": "true",
		},
		Metadata: map[string]any{"type": "antigravity"},
	}
	if _, err := mgr.Register(WithSkipPersist(context.Background()), auth); err != nil {
		t.Fatalf("Register: %v", err)
	}

	mgr.MarkResult(context.Background(), Result{AuthID: "auth-metrics", Provider: "antigravity", Model: "m", Success: true})
	mgr.MarkResult(context.Background(), Result{AuthID: "auth-metrics", Provider: "antigravity", Model: "m", Success: true})
	mgr.MarkResult(context.Background(), Result{AuthID: "auth-metrics", Provider: "antigravity", Model: "m", Success: false})

	snapshots := mgr.ExportCooldownStates()
	if len(snapshots) != 1 {
		t.Fatalf("ExportCooldownStates() returned %d snapshots, want 1", len(snapshots))
	}
	snap := snapshots[0]
	if snap.AuthID != "auth-metrics" {
		t.Fatalf("AuthID = %q, want %q", snap.AuthID, "auth-metrics")
	}
	if snap.Success != 2 {
		t.Fatalf("Success = %d, want 2", snap.Success)
	}
	if snap.Failed != 1 {
		t.Fatalf("Failed = %d, want 1", snap.Failed)
	}
	if len(snap.RecentBuckets) == 0 {
		t.Fatal("RecentBuckets is empty, want non-empty")
	}

	var bucketSuccess, bucketFailed int64
	for _, b := range snap.RecentBuckets {
		bucketSuccess += b.Success
		bucketFailed += b.Failed
	}
	if bucketSuccess != 2 || bucketFailed != 1 {
		t.Fatalf("RecentBuckets totals = success=%d failed=%d, want 2/1", bucketSuccess, bucketFailed)
	}
}

func TestExportCooldownStates_SkipsZeroCountAuth(t *testing.T) {
	mgr := NewManager(nil, nil, nil)
	auth := &Auth{
		ID:       "auth-zero",
		Provider: "antigravity",
		Attributes: map[string]string{
			"runtime_only": "true",
		},
		Metadata: map[string]any{"type": "antigravity"},
	}
	if _, err := mgr.Register(WithSkipPersist(context.Background()), auth); err != nil {
		t.Fatalf("Register: %v", err)
	}

	snapshots := mgr.ExportCooldownStates()
	if len(snapshots) != 0 {
		t.Fatalf("ExportCooldownStates() returned %d snapshots for zero-count auth, want 0", len(snapshots))
	}
}

func TestRestoreCooldownStates_RestoresMetrics(t *testing.T) {
	mgr := NewManager(nil, nil, nil)
	auth := &Auth{
		ID:       "auth-restore",
		Provider: "antigravity",
		Attributes: map[string]string{
			"runtime_only": "true",
		},
		Metadata: map[string]any{"type": "antigravity"},
	}
	if _, err := mgr.Register(WithSkipPersist(context.Background()), auth); err != nil {
		t.Fatalf("Register: %v", err)
	}

	now := time.Now()
	bucketID := recentRequestBucketID(now)

	restored := mgr.RestoreCooldownStates([]AuthCooldownSnapshot{{
		AuthID:  "auth-restore",
		Success: 42,
		Failed:  7,
		RecentBuckets: []PersistedRecentBucket{
			{BucketID: bucketID, Success: 30, Failed: 5},
			{BucketID: bucketID - 1, Success: 12, Failed: 2},
		},
	}})
	if restored != 1 {
		t.Fatalf("RestoreCooldownStates() = %d, want 1", restored)
	}

	got, ok := mgr.GetByID("auth-restore")
	if !ok || got == nil {
		t.Fatal("GetByID returned nil")
	}
	if got.Success != 42 {
		t.Fatalf("Success = %d, want 42", got.Success)
	}
	if got.Failed != 7 {
		t.Fatalf("Failed = %d, want 7", got.Failed)
	}

	snapshot := got.RecentRequestsSnapshot(now)
	var bucketSuccess, bucketFailed int64
	for _, b := range snapshot {
		bucketSuccess += b.Success
		bucketFailed += b.Failed
	}
	if bucketSuccess != 42 || bucketFailed != 7 {
		t.Fatalf("RecentRequests totals = success=%d failed=%d, want 42/7", bucketSuccess, bucketFailed)
	}
}

func TestExportRestoreRoundTrip(t *testing.T) {
	mgr := NewManager(nil, nil, nil)
	auth := &Auth{
		ID:       "auth-roundtrip",
		Provider: "antigravity",
		Attributes: map[string]string{
			"runtime_only": "true",
		},
		Metadata: map[string]any{"type": "antigravity"},
	}
	if _, err := mgr.Register(WithSkipPersist(context.Background()), auth); err != nil {
		t.Fatalf("Register: %v", err)
	}

	for i := 0; i < 10; i++ {
		mgr.MarkResult(context.Background(), Result{AuthID: "auth-roundtrip", Provider: "antigravity", Model: "m", Success: true})
	}
	for i := 0; i < 3; i++ {
		mgr.MarkResult(context.Background(), Result{AuthID: "auth-roundtrip", Provider: "antigravity", Model: "m", Success: false})
	}

	snapshots := mgr.ExportCooldownStates()

	mgr2 := NewManager(nil, nil, nil)
	auth2 := &Auth{
		ID:       "auth-roundtrip",
		Provider: "antigravity",
		Attributes: map[string]string{
			"runtime_only": "true",
		},
		Metadata: map[string]any{"type": "antigravity"},
	}
	if _, err := mgr2.Register(WithSkipPersist(context.Background()), auth2); err != nil {
		t.Fatalf("Register: %v", err)
	}

	restored := mgr2.RestoreCooldownStates(snapshots)
	if restored != 1 {
		t.Fatalf("RestoreCooldownStates() = %d, want 1", restored)
	}

	got, ok := mgr2.GetByID("auth-roundtrip")
	if !ok || got == nil {
		t.Fatal("GetByID returned nil")
	}
	if got.Success != 10 || got.Failed != 3 {
		t.Fatalf("totals = success=%d failed=%d, want 10/3", got.Success, got.Failed)
	}

	snapshot := got.RecentRequestsSnapshot(time.Now())
	var s, f int64
	for _, b := range snapshot {
		s += b.Success
		f += b.Failed
	}
	if s != 10 || f != 3 {
		t.Fatalf("RecentRequests totals = success=%d failed=%d, want 10/3", s, f)
	}
}
