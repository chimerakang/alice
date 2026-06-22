package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var testPNG = []byte{
	0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
	0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4,
	0x89, 0x00, 0x00, 0x00, 0x0A, 0x49, 0x44, 0x41,
	0x54, 0x78, 0x9C, 0x63, 0x60, 0x00, 0x00, 0x00,
	0x02, 0x00, 0x01, 0xE5, 0x27, 0xD4, 0xA2, 0x00,
	0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE,
	0x42, 0x60, 0x82,
}

func newTestScreenshotManager(t *testing.T) *ScreenshotManager {
	t.Helper()
	sm := NewScreenshotManager()
	sm.tempDir = t.TempDir()
	return sm
}

func TestScreenshotManagerValidateURL(t *testing.T) {
	sm := NewScreenshotManager()

	valid := []string{
		"https://example.com",
		"http://localhost:3939",
	}
	for _, raw := range valid {
		if err := sm.validateURL(raw); err != nil {
			t.Fatalf("validateURL(%q) unexpected error: %v", raw, err)
		}
	}

	invalid := []string{
		"",
		"example.com",
		"ftp://example.com",
	}
	for _, raw := range invalid {
		if err := sm.validateURL(raw); err == nil {
			t.Fatalf("validateURL(%q) expected error", raw)
		}
	}
}

func TestCaptureScreenshotUsesPlaywrightCLI(t *testing.T) {
	sm := newTestScreenshotManager(t)

	var gotName string
	var gotArgs []string
	sm.commandRunner = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		gotName = name
		gotArgs = append([]string(nil), args...)
		if len(args) == 0 {
			return nil, fmt.Errorf("missing args")
		}
		outputPath := args[len(args)-1]
		if err := os.WriteFile(outputPath, testPNG, 0o644); err != nil {
			return nil, err
		}
		return []byte("Navigating to https://example.com"), nil
	}

	path, err := sm.CaptureScreenshot(context.Background(), "https://example.com")
	if err != nil {
		t.Fatalf("CaptureScreenshot failed: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })

	if gotName != "npx" {
		t.Fatalf("command name = %q, want npx", gotName)
	}
	joined := strings.Join(gotArgs, " ")
	for _, want := range []string{
		"--yes",
		"playwright",
		"screenshot",
		"--browser=chromium",
		"--full-page",
		"--ignore-https-errors",
		"--viewport-size",
		"1280,720",
		"--wait-for-timeout",
		"1000",
		"https://example.com",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("command args missing %q: %s", want, joined)
		}
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat screenshot: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("screenshot file is empty")
	}
}

func TestCaptureScreenshotRejectsBusyConcurrency(t *testing.T) {
	sm := newTestScreenshotManager(t)
	sm.maxConcurrent = 1

	started := make(chan struct{})
	release := make(chan struct{})
	sm.commandRunner = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if len(args) == 0 {
			return nil, fmt.Errorf("missing args")
		}
		outputPath := args[len(args)-1]
		if err := os.WriteFile(outputPath, testPNG, 0o644); err != nil {
			return nil, err
		}
		select {
		case <-started:
		default:
			close(started)
		}
		<-release
		return nil, nil
	}

	firstDone := make(chan error, 1)
	go func() {
		_, err := sm.CaptureScreenshot(context.Background(), "https://example.com")
		firstDone <- err
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first capture did not start")
	}

	_, err := sm.CaptureScreenshot(context.Background(), "https://example.org")
	if err == nil || !strings.Contains(err.Error(), "忙碌") {
		t.Fatalf("expected busy error, got %v", err)
	}

	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first capture failed: %v", err)
	}
}

func TestCaptureScreenshotTimeoutCleansUpAndReturnsFast(t *testing.T) {
	sm := newTestScreenshotManager(t)
	sm.timeoutSec = 1

	var calls int
	sm.commandRunner = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		calls++
		<-ctx.Done()
		return nil, ctx.Err()
	}

	start := time.Now()
	_, err := sm.CaptureScreenshot(context.Background(), "https://example.com")
	if err == nil || !strings.Contains(err.Error(), "逾時") {
		t.Fatalf("expected timeout error, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("commandRunner called %d times, want 1", calls)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("timeout took too long: %s", elapsed)
	}

	entries, err := os.ReadDir(sm.tempDir)
	if err != nil {
		t.Fatalf("read temp dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected temp dir to be empty after timeout, found %d entries", len(entries))
	}
}

func TestCaptureScreenshotDoesNotLeakCommandOutput(t *testing.T) {
	sm := newTestScreenshotManager(t)
	sm.maxRetries = 0

	leak := "/private/tmp/alice-secret/playwright.log"
	sm.commandRunner = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte("debug output: " + leak), fmt.Errorf("playwright failed: %s", leak)
	}

	_, err := sm.CaptureScreenshot(context.Background(), "https://example.com")
	if err == nil {
		t.Fatal("expected capture error")
	}
	if strings.Contains(err.Error(), leak) {
		t.Fatalf("error leaked command output: %v", err)
	}
	if strings.Contains(err.Error(), sm.tempDir) {
		t.Fatalf("error leaked temp dir: %v", err)
	}
}

func TestCaptureScreenshotBytesFetchesMetadataAndCleansUp(t *testing.T) {
	sm := newTestScreenshotManager(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html>
<html>
<head>
  <title>Preview Title</title>
  <meta name="description" content="Preview description">
</head>
<body>ok</body>
</html>`)
	}))
	defer srv.Close()

	sm.commandRunner = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		outputPath := args[len(args)-1]
		if err := os.WriteFile(outputPath, testPNG, 0o644); err != nil {
			return nil, err
		}
		return nil, nil
	}

	data, metadata, err := sm.CaptureScreenshotBytes(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("CaptureScreenshotBytes failed: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected screenshot bytes")
	}
	if metadata == nil || metadata.Title != "Preview Title" {
		t.Fatalf("metadata title = %+v, want Preview Title", metadata)
	}
	if metadata.Description != "Preview description" {
		t.Fatalf("metadata description = %+v, want Preview description", metadata)
	}

	entries, err := os.ReadDir(sm.tempDir)
	if err != nil {
		t.Fatalf("read temp dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected temp dir to be cleaned, found %d entries", len(entries))
	}
}

func TestCleanupOldScreenshots(t *testing.T) {
	sm := newTestScreenshotManager(t)

	paths := []string{
		filepath.Join(sm.tempDir, "a.png"),
		filepath.Join(sm.tempDir, "b.png"),
		filepath.Join(sm.tempDir, "c.png"),
	}
	for i, path := range paths {
		if err := os.WriteFile(path, testPNG, 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		ts := time.Now().Add(time.Duration(-i) * time.Minute)
		if err := os.Chtimes(path, ts, ts); err != nil {
			t.Fatalf("chtimes %s: %v", path, err)
		}
	}

	if err := sm.CleanupOldScreenshots(1); err != nil {
		t.Fatalf("CleanupOldScreenshots failed: %v", err)
	}

	entries, err := os.ReadDir(sm.tempDir)
	if err != nil {
		t.Fatalf("read temp dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 screenshot remaining, found %d", len(entries))
	}
}
