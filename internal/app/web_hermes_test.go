package app

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleHermesSnapshotsIncludesTaskUsageDetails(t *testing.T) {
	prev := globalStorage
	storage := newTestSQLiteStorage(t)
	globalStorage = storage
	t.Cleanup(func() { globalStorage = prev })

	db := storage.GetDB().(*sql.DB)
	hermesFixture(t, db)
	if _, err := db.Exec(`UPDATE hermes_task_states SET
		model_usages = '[{"model":"gpt-5.5","cache_read_input_tokens":20}]',
		phase_usages = '[{"phase":"executor"}]' WHERE id = 't1'`); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	(&WebInterface{}).handleHermesSnapshots(w, httptest.NewRequest(http.MethodGet, "/api/hermes/snapshots?task_id=t1", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		Total       int              `json:"total"`
		Accumulated string           `json:"accumulated"`
		LatestPlan  []map[string]any `json:"latest_plan"`
		Task        struct {
			ID          string           `json:"id"`
			ModelUsages []map[string]any `json:"model_usages"`
			PhaseUsages []map[string]any `json:"phase_usages"`
		} `json:"task"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Total != 2 || resp.Accumulated != "result" || len(resp.LatestPlan) != 1 || resp.Task.ID != "t1" {
		t.Fatalf("unexpected response: %s", w.Body.String())
	}
	if len(resp.Task.ModelUsages) != 1 || len(resp.Task.PhaseUsages) != 1 {
		t.Fatalf("usage details missing from task payload: %s", w.Body.String())
	}
}

// A database that never ran the old system still serves empty history pages.
func TestHermesHistoryEndpointsWithoutTables(t *testing.T) {
	prev := globalStorage
	globalStorage = newTestSQLiteStorage(t)
	t.Cleanup(func() { globalStorage = prev })

	wi := &WebInterface{}
	for path, handler := range map[string]http.HandlerFunc{
		"/api/hermes/tasks":                 wi.handleHermesTasks,
		"/api/hermes/stats":                 wi.handleHermesStats,
		"/api/hermes/snapshots?task_id=any": wi.handleHermesSnapshots,
	} {
		w := httptest.NewRecorder()
		handler(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusOK {
			t.Errorf("%s: status = %d body=%s", path, w.Code, w.Body.String())
		}
	}
}
