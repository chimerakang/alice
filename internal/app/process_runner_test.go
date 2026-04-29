package app

import (
	"context"
	"testing"
	"time"
)

func TestRunProcessOutputEnforcesTimeout(t *testing.T) {
	start := time.Now()
	_, err := runProcessOutput(context.Background(), ProcessOptions{Timeout: 20 * time.Millisecond}, "sh", "-c", "sleep 1")
	if err == nil {
		t.Fatal("runProcessOutput: expected timeout error")
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("runProcessOutput elapsed = %v, want timeout to stop command promptly", elapsed)
	}
}
