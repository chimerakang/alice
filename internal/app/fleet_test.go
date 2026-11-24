package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/chimerakang/alice-core/fleet"
)

// newFleetTestRepo creates a git repository with one commit.
func newFleetTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-q", "-m", "init")
	return dir
}

// newFleetTestService starts a fleet on a fresh alice database. The
// scheduler is capped so that nothing it is given ever starts an agent.
func newFleetTestService(t *testing.T, cfg FleetConfig) (*FleetService, *SQLiteStorage) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "alice.db")
	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStorage: %v", err)
	}
	prev := globalStorage
	globalStorage = storage
	svc, err := NewFleetService(cfg, fleetDatabase(), dbPath, "sonnet")
	if err != nil {
		t.Fatalf("NewFleetService: %v", err)
	}
	// No agent binary exists in tests; make sure none is looked for.
	svc.Runner.Binary = filepath.Join(t.TempDir(), "no-such-claude")
	ctx, cancel := context.WithCancel(context.Background())
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		svc.Stop()
		globalStorage = prev
		storage.Close()
	})
	return svc, storage
}

func TestFleetConfigTrustMode(t *testing.T) {
	cases := []struct {
		in      string
		want    fleet.TrustMode
		wantErr bool
	}{
		{"", fleet.TrustLocalOnly, false},
		{"local-only", fleet.TrustLocalOnly, false},
		{"direct-pr", fleet.TrustDirectPR, false},
		{"no-mistakes", fleet.TrustNoMistakes, false},
		{"yolo", "", true},
	}
	for _, c := range cases {
		got, err := FleetConfig{DefaultTrustMode: c.in}.trustMode()
		if (err != nil) != c.wantErr || (!c.wantErr && got != c.want) {
			t.Errorf("trustMode(%q) = %q, %v; want %q, err=%v", c.in, got, err, c.want, c.wantErr)
		}
	}
}

func TestNewFleetServiceNeedsPersistence(t *testing.T) {
	if _, err := NewFleetService(FleetConfig{Enabled: true}, nil, "./data/alice.db", ""); err == nil {
		t.Fatal("expected an error without a database")
	}
}

func TestNewFleetServiceRejectsUnknownTrustMode(t *testing.T) {
	storage, err := NewSQLiteStorage(filepath.Join(t.TempDir(), "alice.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	if _, err := NewFleetService(FleetConfig{DefaultTrustMode: "yolo"}, storage.db, storage.path, ""); err == nil {
		t.Fatal("expected an error for an unknown trust mode")
	}
}

func TestFleetServiceDefaultsPathsBesideDatabase(t *testing.T) {
	svc, storage := newFleetTestService(t, FleetConfig{Enabled: true})
	dataDir := filepath.Dir(storage.path)
	if want := filepath.Join(dataDir, "worktrees"); svc.Config.WorktreeRoot != want {
		t.Errorf("WorktreeRoot = %q, want %q", svc.Config.WorktreeRoot, want)
	}
	if want := filepath.Join(dataDir, "fleet-reports"); svc.Config.ReportRoot != want {
		t.Errorf("ReportRoot = %q, want %q", svc.Config.ReportRoot, want)
	}
	if svc.TrustMode != fleet.TrustLocalOnly {
		t.Errorf("TrustMode = %q, want local-only", svc.TrustMode)
	}
}

// The fleet shares alice's database handle, and its lifecycle events reach
// the runtime event log.
func TestFleetServiceForwardsEventsToRuntimeEvents(t *testing.T) {
	svc, storage := newFleetTestService(t, FleetConfig{Enabled: true})
	ctx := context.Background()
	me := fleet.Actor{ID: "42", Via: "test"}

	project, err := svc.Commander.RegisterProject(ctx, me, fleet.Project{RepoPath: newFleetTestRepo(t)})
	if err != nil {
		t.Fatalf("RegisterProject: %v", err)
	}
	// Created directly in the store so the scheduler is not kicked; the
	// cancel below lands before its next poll.
	task, err := svc.Store.CreateTask(ctx, fleet.NewTask{ProjectID: project.ID, Goal: "do a thing"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if _, err := svc.Commander.Cancel(ctx, me, task.ID, "changed my mind"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		events, err := storage.GetRuntimeEvents(50, 0)
		if err != nil {
			t.Fatalf("GetRuntimeEvents: %v", err)
		}
		for _, ev := range events {
			if ev.Type == "fleet.status" && ev.TaskID == task.ID && ev.Payload["to"] == "cancelled" {
				if ev.Payload["project_id"] != project.ID {
					t.Fatalf("project_id = %v, want %s", ev.Payload["project_id"], project.ID)
				}
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no fleet.status runtime event for task %s; got %+v", task.ID, events)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
