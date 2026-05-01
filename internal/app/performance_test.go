package app

import (
	"testing"
	"time"
)

func TestReversePerformanceMetricsRestoresChronologicalOrder(t *testing.T) {
	now := time.Now()
	metrics := []PerformanceMetrics{
		{Timestamp: now.Add(2 * time.Minute), TokensUsed: 3},
		{Timestamp: now.Add(time.Minute), TokensUsed: 2},
		{Timestamp: now, TokensUsed: 1},
	}

	reversePerformanceMetrics(metrics)

	if metrics[0].TokensUsed != 1 || metrics[1].TokensUsed != 2 || metrics[2].TokensUsed != 3 {
		t.Fatalf("metrics order = %v, want chronological oldest to newest", []int{
			metrics[0].TokensUsed,
			metrics[1].TokensUsed,
			metrics[2].TokensUsed,
		})
	}
}

func TestGetRecentMetricsReturnsNewestAfterChronologicalLoad(t *testing.T) {
	now := time.Now()
	pm := NewPerformanceMonitor()
	pm.metrics = []PerformanceMetrics{
		{Timestamp: now, TokensUsed: 1},
		{Timestamp: now.Add(time.Minute), TokensUsed: 2},
		{Timestamp: now.Add(2 * time.Minute), TokensUsed: 3},
	}

	recent := pm.GetRecentMetrics(2)

	if len(recent) != 2 {
		t.Fatalf("recent len = %d, want 2", len(recent))
	}
	if recent[0].TokensUsed != 2 || recent[1].TokensUsed != 3 {
		t.Fatalf("recent tokens = %v, want [2 3]", []int{recent[0].TokensUsed, recent[1].TokensUsed})
	}
}
