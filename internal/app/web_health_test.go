package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHandleHealthIncludesStorageAndJobs(t *testing.T) {
	s := newTestSQLiteStorage(t)
	oldStorage := globalStorage
	oldTracker := globalJobTracker
	globalStorage = s
	globalJobTracker = NewBackgroundJobTracker(5)
	t.Cleanup(func() {
		globalStorage = oldStorage
		globalJobTracker = oldTracker
	})

	if err := s.UpsertUnifiedTask(UnifiedTask{
		ID:        "task-health-active",
		Goal:      "health",
		Engine:    "test",
		Backend:   "test",
		Status:    "executing",
		StartedAt: time.Now(),
	}); err != nil {
		t.Fatalf("UpsertUnifiedTask: %v", err)
	}
	if err := s.UpsertUnifiedTask(UnifiedTask{
		ID:        "task-health-validating",
		Goal:      "health validating",
		Engine:    "test",
		Backend:   "test",
		Status:    "validating",
		StartedAt: time.Now(),
	}); err != nil {
		t.Fatalf("UpsertUnifiedTask(validating): %v", err)
	}
	done := globalJobTracker.Start("test.job")
	defer done(nil)

	wi := &WebInterface{}
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	w := httptest.NewRecorder()
	wi.handleHealth(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	storage, ok := body["storage"].(map[string]any)
	if !ok {
		t.Fatalf("missing storage health: %+v", body)
	}
	if storage["healthy"] != true || storage["backend"] != "sqlite" {
		t.Fatalf("storage health = %+v", storage)
	}
	if got := storage["active_tasks"]; got != float64(2) {
		t.Fatalf("active_tasks = %v, want 2", got)
	}
	jobs, ok := body["jobs"].(map[string]any)
	if !ok {
		t.Fatalf("missing jobs summary: %+v", body)
	}
	if got := jobs["active_count"]; got != float64(1) {
		t.Fatalf("active_count = %v, want 1", got)
	}
}
