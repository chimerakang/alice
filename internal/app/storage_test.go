package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// openTestStorage creates a SQLiteStorage backed by a temp-dir file, with
// SetMaxOpenConns(1) already applied by NewSQLiteStorage.  The db is closed
// automatically via t.Cleanup.
func openTestStorage(t *testing.T) *SQLiteStorage {
	t.Helper()
	dir := t.TempDir()
	s, err := NewSQLiteStorage(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStorage: %v", err)
	}
	t.Cleanup(func() { s.db.Close() })
	return s
}

// TestGetToolExecutionStats_NoDeadlock verifies that GetToolExecutionStats
// completes without deadlocking under SetMaxOpenConns(1).  The function opens
// a primary query, closes it, and conditionally opens a fallback query —
// all sequential with no nested open cursors.
func TestGetToolExecutionStats_NoDeadlock(t *testing.T) {
	s := openTestStorage(t)

	// Insert one row into performance_metrics so the primary path is exercised.
	now := time.Now().Format("2006-01-02 15:04:05")
	_, err := s.db.Exec(`INSERT INTO performance_metrics
		(chat_id, timestamp, tool_execution_type, tool_execution_time_ms)
		VALUES (1, ?, 'Bash', 42)`, now)
	if err != nil {
		t.Fatalf("insert performance_metrics: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := s.GetToolExecutionStats()
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("GetToolExecutionStats: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("GetToolExecutionStats deadlocked under SetMaxOpenConns(1)")
	}
}

// TestGetToolExecutionStats_FallbackNoDeadlock exercises the fallback path
// where performance_metrics has no rows and tool_executions is queried instead.
func TestGetToolExecutionStats_FallbackNoDeadlock(t *testing.T) {
	s := openTestStorage(t)

	// Insert into tool_executions only, leaving performance_metrics empty.
	now := time.Now().Format("2006-01-02 15:04:05")
	_, err := s.db.Exec(`INSERT INTO tool_executions
		(chat_id, tool_name, duration_ms, status, timestamp)
		VALUES (1, 'Read', 10, 'success', ?)`, now)
	if err != nil {
		t.Fatalf("insert tool_executions: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := s.GetToolExecutionStats()
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("GetToolExecutionStats fallback: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("GetToolExecutionStats fallback deadlocked under SetMaxOpenConns(1)")
	}
}

// TestGetCostSavings_NoDeadlock verifies that GetCostSavings (which uses
// defer rows.Close with no nested queries) completes without deadlock.
func TestGetCostSavings_NoDeadlock(t *testing.T) {
	s := openTestStorage(t)

	// Insert one decision_log row with cost data.
	now := time.Now().Format(time.RFC3339)
	_, err := s.db.Exec(`INSERT INTO decision_logs
		(chat_id, session_id, timestamp, model, tokens_input, tokens_output, cost_usd, routing_reason)
		VALUES (1, 'sess-1', ?, 'claude-3-5-haiku', 1000, 500, 0.001, 'cost_saving')`, now)
	if err != nil {
		t.Fatalf("insert decision_logs: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := s.GetCostSavings(24)
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("GetCostSavings: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("GetCostSavings deadlocked under SetMaxOpenConns(1)")
	}
}

// TestGetCostSavings_EmptyDB ensures GetCostSavings handles an empty DB
// correctly (no rows, no crash) under SetMaxOpenConns(1).
func TestGetCostSavings_EmptyDB(t *testing.T) {
	s := openTestStorage(t)

	done := make(chan error, 1)
	go func() {
		report, err := s.GetCostSavings(24)
		if err == nil && report.TotalRequests != 0 {
			done <- nil
			return
		}
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("GetCostSavings empty: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("GetCostSavings empty deadlocked under SetMaxOpenConns(1)")
	}
}

// TestGetPerformanceAnalytics_NoDeadlock verifies that GetPerformanceAnalytics
// (which opens three sequential rows cursors: base stats via QueryRow, then
// ErrorsByType, then ToolUsageStats) completes without deadlocking under
// SetMaxOpenConns(1).  Each cursor is fully closed before the next is opened.
func TestGetPerformanceAnalytics_NoDeadlock(t *testing.T) {
	s := openTestStorage(t)

	now := time.Now().Format("2006-01-02 15:04:05")
	_, err := s.db.Exec(`INSERT INTO performance_metrics
		(chat_id, timestamp, tool_execution_type, tool_execution_time_ms,
		 api_call_success, api_call_latency_ms, tokens_used, estimated_cost,
		 memory_usage, error_type)
		VALUES (1, ?, 'Bash', 42, 1, 120, 500, 0.001, 1024, 'timeout')`, now)
	if err != nil {
		t.Fatalf("insert performance_metrics: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := s.GetPerformanceAnalytics(24)
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("GetPerformanceAnalytics: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("GetPerformanceAnalytics deadlocked under SetMaxOpenConns(1)")
	}
}

// TestGetPerformanceAnalytics_EmptyDB ensures GetPerformanceAnalytics handles
// an empty DB (no rows) without deadlock or crash.
func TestGetPerformanceAnalytics_EmptyDB(t *testing.T) {
	s := openTestStorage(t)

	done := make(chan error, 1)
	go func() {
		_, err := s.GetPerformanceAnalytics(24)
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("GetPerformanceAnalytics empty: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("GetPerformanceAnalytics empty deadlocked under SetMaxOpenConns(1)")
	}
}

// TestGetCostSavingsByProject_NoDeadlock verifies that GetCostSavingsByProject
// (single rows cursor with defer rows.Close) completes without deadlocking under
// SetMaxOpenConns(1).
func TestGetCostSavingsByProject_NoDeadlock(t *testing.T) {
	s := openTestStorage(t)

	now := time.Now().Format(time.RFC3339)
	_, err := s.db.Exec(`INSERT INTO decision_logs
		(chat_id, session_id, timestamp, model, tokens_input, tokens_output, cost_usd,
		 routing_reason, project_path)
		VALUES (1, 'sess-2', ?, 'claude-3-5-haiku', 1000, 500, 0.001, 'cost_saving', '/projects/test')`, now)
	if err != nil {
		t.Fatalf("insert decision_logs: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := s.GetCostSavingsByProject("/projects/test", 24)
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("GetCostSavingsByProject: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("GetCostSavingsByProject deadlocked under SetMaxOpenConns(1)")
	}
}

// TestGetCostSavingsByProject_EmptyDB ensures GetCostSavingsByProject handles
// an empty DB without deadlock.
func TestGetCostSavingsByProject_EmptyDB(t *testing.T) {
	s := openTestStorage(t)

	done := make(chan error, 1)
	go func() {
		_, err := s.GetCostSavingsByProject("/projects/nonexistent", 24)
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("GetCostSavingsByProject empty: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("GetCostSavingsByProject empty deadlocked under SetMaxOpenConns(1)")
	}
}

// TestGetDecisionLogs_NoDeadlock verifies that GetDecisionLogs (multi-row
// cursor with defer rows.Close) completes without deadlocking under
// SetMaxOpenConns(1). This covers the dashboard decision-log view path.
func TestGetDecisionLogs_NoDeadlock(t *testing.T) {
	s := openTestStorage(t)

	now := time.Now().Format(time.RFC3339)
	for i := 0; i < 3; i++ {
		_, err := s.db.Exec(`INSERT INTO decision_logs
			(chat_id, thread_id, session_id, project_path, timestamp, model,
			 user_prompt, agent_response, tool_calls_json, context_json, outcome_json,
			 duration_ms, tokens_input, tokens_output, cost_usd, routing_reason)
			VALUES (?, 0, ?, '/repo', ?, 'claude-3-5-haiku',
			        'hi', 'ok', '[]', '{}', '{}',
			        10, 1000, 500, 0.001, 'default')`,
			i+1, "sess-dl-"+string(rune('a'+i)), now)
		if err != nil {
			t.Fatalf("insert decision_logs[%d]: %v", i, err)
		}
	}

	done := make(chan error, 1)
	go func() {
		_, err := s.GetDecisionLogs(10, 0)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("GetDecisionLogs error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("GetDecisionLogs deadlocked under SetMaxOpenConns(1)")
	}
}

// TestGetToolExecutions_NoDeadlock verifies that GetToolExecutions (multi-row
// cursor with defer rows.Close) completes without deadlocking under
// SetMaxOpenConns(1). This covers the dashboard tool-execution view path.
func TestGetToolExecutions_NoDeadlock(t *testing.T) {
	s := openTestStorage(t)

	now := time.Now().Format("2006-01-02 15:04:05")
	for i := 0; i < 3; i++ {
		_, err := s.db.Exec(`INSERT INTO tool_executions
			(chat_id, thread_id, tool_name, input_json, duration_ms, status, error, timestamp)
			VALUES (?, 0, 'Read', '{}', 10, 'success', '', ?)`, i+1, now)
		if err != nil {
			t.Fatalf("insert tool_executions[%d]: %v", i, err)
		}
	}

	done := make(chan error, 1)
	go func() {
		_, err := s.GetToolExecutions(10, 0)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("GetToolExecutions error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("GetToolExecutions deadlocked under SetMaxOpenConns(1)")
	}
}

// TestDashboardSequentialQueries_NoDeadlock simulates a dashboard WebSocket poll
// that calls GetToolExecutionStats, GetDecisionLogs, and GetPerformanceMetrics
// sequentially on a single-connection DB. Verifies no deadlock between calls.
func TestDashboardSequentialQueries_NoDeadlock(t *testing.T) {
	s := openTestStorage(t)

	done := make(chan error, 1)
	go func() {
		if _, err := s.GetToolExecutionStats(); err != nil {
			done <- err
			return
		}
		if _, err := s.GetDecisionLogs(10, 0); err != nil {
			done <- err
			return
		}
		if _, err := s.GetPerformanceMetrics(10, 0); err != nil {
			done <- err
			return
		}
		done <- nil
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("sequential dashboard queries error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("sequential dashboard queries deadlocked under SetMaxOpenConns(1)")
	}
}

func init() {
	// Ensure the sqlite driver is registered (imported via storage.go in same package).
	_ = os.DevNull
}
