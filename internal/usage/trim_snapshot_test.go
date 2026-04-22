package usage

import (
	"testing"
	"time"
)

func TestTrimSnapshot_RemovesOldDays(t *testing.T) {
	now := time.Now()
	snap := StatisticsSnapshot{
		TotalRequests: 100,
		SuccessCount:  90,
		FailureCount:  10,
		TotalTokens:   5000,
		RequestsByDay: map[string]int64{
			now.Format("2006-01-02"):                    80,
			now.AddDate(0, 0, -29).Format("2006-01-02"): 10,
			now.AddDate(0, 0, -31).Format("2006-01-02"): 5,
			now.AddDate(0, 0, -60).Format("2006-01-02"): 5,
		},
		TokensByDay: map[string]int64{
			now.Format("2006-01-02"):                    4000,
			now.AddDate(0, 0, -29).Format("2006-01-02"): 500,
			now.AddDate(0, 0, -31).Format("2006-01-02"): 300,
			now.AddDate(0, 0, -60).Format("2006-01-02"): 200,
		},
		RequestsByHour: map[string]int64{"12": 50, "13": 50},
		TokensByHour:   map[string]int64{"12": 2500, "13": 2500},
		APIs:           make(map[string]APISnapshot),
	}

	result := TrimSnapshot(snap, 30)

	// Should keep today and 29-day-old, remove 31 and 60 day old
	if len(result.RequestsByDay) != 2 {
		t.Errorf("expected 2 days retained, got %d", len(result.RequestsByDay))
	}
	if _, ok := result.RequestsByDay[now.AddDate(0, 0, -31).Format("2006-01-02")]; ok {
		t.Error("31-day-old entry should have been removed")
	}
	if len(result.TokensByDay) != 2 {
		t.Errorf("expected 2 token days retained, got %d", len(result.TokensByDay))
	}

	// Hourly maps should be cleared (can't accurately trim)
	if len(result.RequestsByHour) != 0 {
		t.Errorf("expected hourly map to be cleared, got %d entries", len(result.RequestsByHour))
	}
}

func TestTrimSnapshot_FiltersDetails(t *testing.T) {
	now := time.Now()
	snap := StatisticsSnapshot{
		APIs: map[string]APISnapshot{
			"api-1": {
				TotalRequests: 3,
				TotalTokens:   300,
				Models: map[string]ModelSnapshot{
					"model-a": {
						TotalRequests: 3,
						TotalTokens:   300,
						Details: []RequestDetail{
							{Timestamp: now, Tokens: TokenStats{TotalTokens: 100}, Failed: false},
							{Timestamp: now.AddDate(0, 0, -15), Tokens: TokenStats{TotalTokens: 100}, Failed: false},
							{Timestamp: now.AddDate(0, 0, -45), Tokens: TokenStats{TotalTokens: 100}, Failed: true},
						},
					},
				},
			},
		},
		RequestsByDay:  map[string]int64{},
		TokensByDay:    map[string]int64{},
		RequestsByHour: map[string]int64{},
		TokensByHour:   map[string]int64{},
	}

	result := TrimSnapshot(snap, 30)

	api := result.APIs["api-1"]
	model := api.Models["model-a"]

	if len(model.Details) != 2 {
		t.Fatalf("expected 2 details retained, got %d", len(model.Details))
	}
	if model.TotalRequests != 2 {
		t.Errorf("expected 2 total requests, got %d", model.TotalRequests)
	}
	if model.TotalTokens != 200 {
		t.Errorf("expected 200 total tokens, got %d", model.TotalTokens)
	}

	// Global counters recalculated
	if result.TotalRequests != 2 {
		t.Errorf("expected global total 2, got %d", result.TotalRequests)
	}
	if result.SuccessCount != 2 {
		t.Errorf("expected 2 success, got %d", result.SuccessCount)
	}
	if result.FailureCount != 0 {
		t.Errorf("expected 0 failure (old one trimmed), got %d", result.FailureCount)
	}
}

func TestTrimSnapshot_DoesNotMutateInput(t *testing.T) {
	now := time.Now()
	snap := StatisticsSnapshot{
		RequestsByDay: map[string]int64{
			now.Format("2006-01-02"):                    10,
			now.AddDate(0, 0, -60).Format("2006-01-02"): 5,
		},
		TokensByDay:    map[string]int64{},
		RequestsByHour: map[string]int64{"12": 50},
		TokensByHour:   map[string]int64{},
		APIs:           map[string]APISnapshot{},
	}

	_ = TrimSnapshot(snap, 30)

	// Original should still have both days
	if len(snap.RequestsByDay) != 2 {
		t.Errorf("original snapshot was mutated: expected 2 days, got %d", len(snap.RequestsByDay))
	}
	if len(snap.RequestsByHour) != 1 {
		t.Errorf("original hourly map was mutated: expected 1, got %d", len(snap.RequestsByHour))
	}
}

func TestTrimSnapshot_Empty(t *testing.T) {
	snap := StatisticsSnapshot{
		RequestsByDay:  map[string]int64{},
		TokensByDay:    map[string]int64{},
		RequestsByHour: map[string]int64{},
		TokensByHour:   map[string]int64{},
		APIs:           map[string]APISnapshot{},
	}

	result := TrimSnapshot(snap, 30)

	if result.TotalRequests != 0 {
		t.Errorf("expected 0 total requests, got %d", result.TotalRequests)
	}
	if len(result.APIs) != 0 {
		t.Errorf("expected 0 APIs, got %d", len(result.APIs))
	}
}
