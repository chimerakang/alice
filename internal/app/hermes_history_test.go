package app

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

// hermesFixture writes the tables as the removed task system left them.
func hermesFixture(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, stmt := range []string{
		`CREATE TABLE hermes_task_states (id TEXT PRIMARY KEY, chat_id INTEGER NOT NULL, thread_id INTEGER NOT NULL DEFAULT 0,
			planner_session TEXT NOT NULL DEFAULT '', project_dir TEXT NOT NULL DEFAULT '', goal TEXT NOT NULL,
			current_idx INTEGER NOT NULL DEFAULT 0, accumulated TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT 'planning',
			interrupted_by INTEGER, interrupt_policy TEXT NOT NULL DEFAULT 'queue', token_budget TEXT NOT NULL DEFAULT '{}',
			plan_json TEXT NOT NULL DEFAULT '[]', github_issue_number INTEGER NOT NULL DEFAULT 0,
			model_usages TEXT NOT NULL DEFAULT '[]', phase_usages TEXT NOT NULL DEFAULT '[]',
			created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
		`CREATE TABLE hermes_snapshots (id TEXT PRIMARY KEY, task_id TEXT NOT NULL, chat_id INTEGER NOT NULL DEFAULT 0,
			thread_id INTEGER NOT NULL DEFAULT 0, step INTEGER NOT NULL, state_json TEXT NOT NULL,
			next_step TEXT NOT NULL DEFAULT '', source_node TEXT NOT NULL DEFAULT '', writes_json TEXT NOT NULL DEFAULT '{}',
			metadata_json TEXT NOT NULL DEFAULT '{}', parent_snapshot_id TEXT NOT NULL DEFAULT '',
			channel_versions_json TEXT NOT NULL DEFAULT '{}', state_status TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL)`,
		`INSERT INTO hermes_task_states (id, chat_id, goal, status, token_budget, github_issue_number, created_at, updated_at)
			VALUES ('t1', 7, 'old goal', 'done', '{"used_tokens":42}', 12, '2026-01-01T00:00:00Z', '2026-01-02T00:00:00Z')`,
		`INSERT INTO hermes_snapshots (id, task_id, step, state_json, next_step, metadata_json, created_at)
			VALUES ('s1', 't1', 1, '{"status":"executing","plan":[]}', 'executor', '{}', '2026-01-01T00:00:00Z')`,
		`INSERT INTO hermes_snapshots (id, task_id, step, state_json, next_step, metadata_json, created_at)
			VALUES ('s2', 't1', 2, '{"status":"done","project_dir":"/p","accumulated":"result","plan":[{"id":"a","description":"step","status":"done"}]}',
			'end', '{"reason":"finished"}', '2026-01-02T00:00:00Z')`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
}

func openHermesDB(t *testing.T) (*sql.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "history.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db, path
}

func TestExportHermesHistory(t *testing.T) {
	db, path := openHermesDB(t)
	hermesFixture(t, db)

	var out bytes.Buffer
	if err := exportHermesHistory([]string{path}, &out); err != nil {
		t.Fatal(err)
	}
	var got []hermesTaskSummary
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("tasks = %d, want 1", len(got))
	}
	task := got[0]
	if task.Status != "done" || task.Summary != "result" || task.ProjectDir != "/p" ||
		task.UsedTokens != 42 || task.GithubIssueNumber != 12 || len(task.SubTasks) != 1 {
		t.Fatalf("unexpected export: %+v", task)
	}
}

func TestHermesHistoryReads(t *testing.T) {
	db, _ := openHermesDB(t)
	history := hermesHistory{db: db}

	// A database that never ran the old system has empty history, and the
	// export says so instead of writing an empty file.
	if refs, total, err := history.list(hermesHistoryFilter{}); err != nil || total != 0 || len(refs) != 0 {
		t.Fatalf("fresh database: refs=%v total=%d err=%v", refs, total, err)
	}
	if err := writeHermesExport(db, &bytes.Buffer{}); !errors.Is(err, errNoHermesTables) {
		t.Fatalf("export without tables: %v", err)
	}

	hermesFixture(t, db)
	refs, total, err := history.list(hermesHistoryFilter{})
	if err != nil || total != 1 || len(refs) != 1 {
		t.Fatalf("list: refs=%v total=%d err=%v", refs, total, err)
	}
	if refs[0].NextStep != "end" || refs[0].PlanLength != 0 || refs[0].ProjectDir != "/p" {
		t.Fatalf("list must read the latest snapshot: %+v", refs[0])
	}
	snaps, err := history.snapshots("t1")
	if err != nil || len(snaps) != 2 || snaps[1].Reason != "finished" {
		t.Fatalf("snapshots: %+v %v", snaps, err)
	}
	task, err := history.task("t1", snaps[1].RawState)
	if err != nil || task["accumulated"] == "" || task["id"] != "t1" {
		t.Fatalf("task: %v %v", task, err)
	}
	if missing, err := history.task("nope", ""); err != nil || missing != nil {
		t.Fatalf("unknown task: %v %v", missing, err)
	}
}
