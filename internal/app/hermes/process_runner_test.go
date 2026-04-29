package hermes

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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

func TestRunProcessCombinedOutputAppliesOutputLimit(t *testing.T) {
	output, err := runProcessCombinedOutput(context.Background(), ProcessOptions{OutputLimit: 5}, "sh", "-c", "printf 123456789")
	if err != nil {
		t.Fatalf("runProcessCombinedOutput: %v", err)
	}
	got := string(output)
	if !strings.HasPrefix(got, "12345") || !strings.Contains(got, "process output truncated") {
		t.Fatalf("output = %q, want limited output with truncation marker", got)
	}
}

func TestRunProcessTimeoutKillsChildProcessGroup(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "child-survived")
	_, err := runProcessOutput(context.Background(), ProcessOptions{Timeout: 20 * time.Millisecond}, "sh", "-c", "sleep 0.2; echo survived > "+marker)
	if err == nil {
		t.Fatal("runProcessOutput: expected timeout error")
	}

	time.Sleep(300 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Fatalf("child process survived cancellation and wrote %s", marker)
	} else if !os.IsNotExist(err) {
		t.Fatalf("checking marker: %v", err)
	}
}
