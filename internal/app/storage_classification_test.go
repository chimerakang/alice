package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// newTestSQLiteStorage creates a temporary SQLiteStorage backed by an on-disk
// file (SQLite's in-memory mode conflicts when multiple connections are opened).
func newTestSQLiteStorage(t *testing.T) *SQLiteStorage {
	t.Helper()
	dir := t.TempDir()
	s, err := NewSQLiteStorage(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStorage: %v", err)
	}
	t.Cleanup(func() {
		s.Close()
		os.RemoveAll(dir)
	})
	return s
}

func TestGetToolExecutionStatsByTimeRangeDoesNotDeadlockWithSingleSQLiteConn(t *testing.T) {
	s := newTestSQLiteStorage(t)
	if err := s.InsertToolExecution(ToolExecution{
		Timestamp: time.Now(),
		ToolName:  "Read",
		Status:    "success",
		Duration:  150 * time.Millisecond,
	}); err != nil {
		t.Fatalf("InsertToolExecution: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := s.GetToolExecutionStatsByTimeRange(time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("GetToolExecutionStatsByTimeRange: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("GetToolExecutionStatsByTimeRange deadlocked with single SQLite connection")
	}
}

func TestGetPerformanceAnalyticsDoesNotDeadlockWithSingleSQLiteConn(t *testing.T) {
	s := newTestSQLiteStorage(t)

	done := make(chan error, 1)
	go func() {
		_, err := s.GetPerformanceAnalytics(24)
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("GetPerformanceAnalytics: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("GetPerformanceAnalytics deadlocked with single SQLite connection")
	}
}

func TestTopicModelPreferenceRoundTrip(t *testing.T) {
	s := newTestSQLiteStorage(t)

	if got, err := s.GetTopicModelPreference(42, 7); err != nil || got != "" {
		t.Fatalf("empty preference: got %q err=%v", got, err)
	}

	if err := s.SaveTopicSetting(42, 7, "/repo"); err != nil {
		t.Fatalf("SaveTopicSetting: %v", err)
	}
	if err := s.SaveTopicModelPreference(42, 7, "/repo", "gpt-5.5"); err != nil {
		t.Fatalf("SaveTopicModelPreference: %v", err)
	}

	got, err := s.GetTopicModelPreference(42, 7)
	if err != nil {
		t.Fatalf("GetTopicModelPreference: %v", err)
	}
	if got != "gpt-5.5" {
		t.Fatalf("model preference = %q, want gpt-5.5", got)
	}

	projectDir, err := s.GetTopicSetting(42, 7)
	if err != nil {
		t.Fatalf("GetTopicSetting: %v", err)
	}
	if projectDir != "/repo" {
		t.Fatalf("project dir = %q, want /repo", projectDir)
	}
}

func TestListPromptClassifications_Empty(t *testing.T) {
	s := newTestSQLiteStorage(t)
	recs, err := s.ListPromptClassifications(10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(recs) != 0 {
		t.Fatalf("expected 0 records, got %d", len(recs))
	}
}

func TestListPromptClassifications_RoundTrip(t *testing.T) {
	s := newTestSQLiteStorage(t)

	base := time.Now().UTC().Truncate(time.Second)
	records := []PromptClassificationRecord{
		{SessionID: "s1", Timestamp: base.Add(-2 * time.Second), Source: "vscode", CWD: "/proj", PromptSnippet: "refactor auth", PromptLength: 15, Classification: "complex", MatchedRule: "refactor"},
		{SessionID: "s2", Timestamp: base.Add(-1 * time.Second), Source: "terminal", CWD: "/proj", PromptSnippet: "git status", PromptLength: 10, Classification: "trivial", MatchedRule: ""},
		{SessionID: "s3", Timestamp: base, Source: "vscode", CWD: "/proj", PromptSnippet: "implement #108", PromptLength: 14, Classification: "complex", MatchedRule: "implement+issue"},
	}
	for _, r := range records {
		if err := s.RecordPromptClassification(r); err != nil {
			t.Fatalf("RecordPromptClassification: %v", err)
		}
	}

	recs, err := s.ListPromptClassifications(10)
	if err != nil {
		t.Fatalf("ListPromptClassifications: %v", err)
	}
	if len(recs) != 3 {
		t.Fatalf("expected 3 records, got %d", len(recs))
	}
	// Newest first
	if recs[0].SessionID != "s3" {
		t.Errorf("expected s3 first, got %s", recs[0].SessionID)
	}
	if recs[2].SessionID != "s1" {
		t.Errorf("expected s1 last, got %s", recs[2].SessionID)
	}
}

func TestListPromptClassifications_LimitRespected(t *testing.T) {
	s := newTestSQLiteStorage(t)

	base := time.Now().UTC()
	for i := 0; i < 5; i++ {
		r := PromptClassificationRecord{
			SessionID:      "s" + string(rune('0'+i)),
			Timestamp:      base.Add(time.Duration(i) * time.Second),
			Source:         "vscode",
			PromptSnippet:  "prompt",
			PromptLength:   6,
			Classification: "moderate",
		}
		if err := s.RecordPromptClassification(r); err != nil {
			t.Fatalf("RecordPromptClassification: %v", err)
		}
	}

	recs, err := s.ListPromptClassifications(3)
	if err != nil {
		t.Fatalf("ListPromptClassifications: %v", err)
	}
	if len(recs) != 3 {
		t.Fatalf("expected limit=3, got %d records", len(recs))
	}
}

func TestListPromptClassifications_FieldsPreserved(t *testing.T) {
	s := newTestSQLiteStorage(t)

	ts := time.Now().UTC().Truncate(time.Second)
	rec := PromptClassificationRecord{
		SessionID:      "sess-abc",
		Timestamp:      ts,
		Source:         "terminal",
		CWD:            "/home/user/project",
		PromptSnippet:  "implement feature X",
		PromptLength:   19,
		Classification: "complex",
		MatchedRule:    "implement",
	}
	if err := s.RecordPromptClassification(rec); err != nil {
		t.Fatalf("RecordPromptClassification: %v", err)
	}

	recs, err := s.ListPromptClassifications(1)
	if err != nil {
		t.Fatalf("ListPromptClassifications: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("expected 1 record, got %d", len(recs))
	}
	got := recs[0]
	if got.SessionID != rec.SessionID {
		t.Errorf("SessionID: want %q, got %q", rec.SessionID, got.SessionID)
	}
	if got.Source != rec.Source {
		t.Errorf("Source: want %q, got %q", rec.Source, got.Source)
	}
	if got.CWD != rec.CWD {
		t.Errorf("CWD: want %q, got %q", rec.CWD, got.CWD)
	}
	if got.PromptSnippet != rec.PromptSnippet {
		t.Errorf("PromptSnippet: want %q, got %q", rec.PromptSnippet, got.PromptSnippet)
	}
	if got.PromptLength != rec.PromptLength {
		t.Errorf("PromptLength: want %d, got %d", rec.PromptLength, got.PromptLength)
	}
	if got.Classification != rec.Classification {
		t.Errorf("Classification: want %q, got %q", rec.Classification, got.Classification)
	}
	if got.MatchedRule != rec.MatchedRule {
		t.Errorf("MatchedRule: want %q, got %q", rec.MatchedRule, got.MatchedRule)
	}
}
