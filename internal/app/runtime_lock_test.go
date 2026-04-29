package app

import (
	"strings"
	"testing"
)

func TestAcquireRuntimeLockExcludesSecondProcess(t *testing.T) {
	lockPath := t.TempDir() + "/alice.lock"
	lock, err := AcquireRuntimeLock(lockPath)
	if err != nil {
		t.Fatalf("AcquireRuntimeLock first: %v", err)
	}
	defer lock.Close()

	second, err := AcquireRuntimeLock(lockPath)
	if err == nil {
		second.Close()
		t.Fatal("AcquireRuntimeLock second: expected lock error")
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Fatalf("AcquireRuntimeLock second error = %v, want already running", err)
	}
}

func TestAcquireRuntimeLockReleases(t *testing.T) {
	lockPath := t.TempDir() + "/alice.lock"
	lock, err := AcquireRuntimeLock(lockPath)
	if err != nil {
		t.Fatalf("AcquireRuntimeLock first: %v", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	lock, err = AcquireRuntimeLock(lockPath)
	if err != nil {
		t.Fatalf("AcquireRuntimeLock after close: %v", err)
	}
	lock.Close()
}
