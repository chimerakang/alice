package issueops

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"claude-tg-agent/internal/app/hermes"
)

func writeExecutableScript(t *testing.T, dir, name, body string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write script %s: %v", name, err)
	}
	return path
}

func TestLoadIssueChecklistMapping_FromGhBody(t *testing.T) {
	projectDir := t.TempDir()
	ghDir := t.TempDir()
	issueJSON := `{"title":"IssueOps mapping","body":"## Scope\n- [ ] Update API timeout handling\n- [x] Sync issue checklist mapping with review\n- [ ] Write validation tests\n","state":"open","labels":[{"name":"enhancement"}],"comments":[]}`
	writeExecutableScript(t, ghDir, "gh", "#!/bin/sh\nif [ \"$1\" = \"issue\" ] && [ \"$2\" = \"view\" ] && [ \"$3\" = \"17\" ]; then\n  cat <<'JSON'\n"+issueJSON+"\nJSON\n  exit 0\nfi\nprintf 'unexpected args: %s\\n' \"$*\" >&2\nexit 1\n")
	t.Setenv("PATH", ghDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	svc := New()
	got, err := svc.LoadIssueChecklistMapping(context.Background(), projectDir, 17, []hermes.SubTask{
		{ID: "s1", Description: "Update API timeout handling"},
		{ID: "s2", Description: "Sync issue checklist mapping with review"},
		{ID: "s3", Description: "Write validation tests"},
	})
	if err != nil {
		t.Fatalf("LoadIssueChecklistMapping() error = %v", err)
	}
	if got.State != hermes.IssueStateChecklistSynced {
		t.Fatalf("State = %q, want %q", got.State, hermes.IssueStateChecklistSynced)
	}
	if got.NeedsHumanConfirmation {
		t.Fatal("expected high-confidence mapping to avoid human confirmation")
	}
	if len(got.Mappings) != 3 {
		t.Fatalf("Mappings len = %d, want 3", len(got.Mappings))
	}
	if got.Mappings[0].Confidence != ChecklistMappingConfidenceHigh || got.Mappings[1].Confidence != ChecklistMappingConfidenceHigh || got.Mappings[2].Confidence != ChecklistMappingConfidenceHigh {
		t.Fatalf("unexpected mapping confidence: %+v", got.Mappings)
	}
	if len(got.UnmappedChecklistItems) != 0 || len(got.UnmappedSubTasks) != 0 {
		t.Fatalf("unexpected unmapped items: %+v", got)
	}
}

func TestBuildChecklistMapping_LowConfidenceNeedsHumanConfirmation(t *testing.T) {
	issue := &hermes.IssueContext{
		Number: 101,
		Title:  "IssueOps service",
		Body:   "body",
		State:  "open",
		Labels: []string{"enhancement", "ready"},
		Checklist: []hermes.ChecklistItem{
			{Text: "Sync checklist mapping with issue evidence", Checked: false, LineNumber: 1},
			{Text: "Prepare close readiness summary", Checked: false, LineNumber: 2},
		},
	}

	got := New().BuildChecklistMapping(issue, []hermes.SubTask{
		{ID: "s1", Description: "sync checklist mapping evidence"},
	})
	if got.State != hermes.IssueStateBlocked {
		t.Fatalf("State = %q, want %q", got.State, hermes.IssueStateBlocked)
	}
	if !got.NeedsHumanConfirmation {
		t.Fatal("expected low-confidence mapping to require human confirmation")
	}
	if len(got.Mappings) != 1 {
		t.Fatalf("Mappings len = %d, want 1", len(got.Mappings))
	}
	if got.Mappings[0].Confidence != ChecklistMappingConfidenceLow {
		t.Fatalf("Confidence = %q, want %q", got.Mappings[0].Confidence, ChecklistMappingConfidenceLow)
	}
}

func TestBuildChecklistMapping_UnmappedItemsMarkUnsynced(t *testing.T) {
	issue := &hermes.IssueContext{
		Number: 100,
		Title:  "IssueOps service",
		Body:   "body",
		State:  "open",
		Checklist: []hermes.ChecklistItem{
			{Text: "Update API timeout handling", Checked: false, LineNumber: 1},
			{Text: "Write validation tests", Checked: false, LineNumber: 2},
		},
	}

	got := New().BuildChecklistMapping(issue, []hermes.SubTask{
		{ID: "s1", Description: "Update API timeout handling"},
	})
	if got.State != hermes.IssueStateChecklistUnsynced {
		t.Fatalf("State = %q, want %q", got.State, hermes.IssueStateChecklistUnsynced)
	}
	if got.NeedsHumanConfirmation {
		t.Fatal("unsynced mapping should not need human confirmation by itself")
	}
	if len(got.UnmappedChecklistItems) != 1 {
		t.Fatalf("UnmappedChecklistItems len = %d, want 1", len(got.UnmappedChecklistItems))
	}
}

func TestRecordEvidence_IncludesMappingAndSources(t *testing.T) {
	projectDir := t.TempDir()
	ghDir := t.TempDir()
	commentPath := filepath.Join(projectDir, "comment.txt")
	writeExecutableScript(t, ghDir, "gh", "#!/bin/sh\nif [ \"$1\" = \"issue\" ] && [ \"$2\" = \"comment\" ] && [ \"$3\" = \"17\" ] && [ \"$4\" = \"--body\" ]; then\n  printf '%s' \"$5\" > \""+commentPath+"\"\n  exit 0\nfi\nprintf 'unexpected args: %s\\n' \"$*\" >&2\nexit 1\n")
	t.Setenv("PATH", ghDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	svc := New()
	err := svc.RecordEvidence(context.Background(), RecordEvidenceRequest{
		ProjectDir:  projectDir,
		IssueNumber: 17,
		Comment:     true,
		SubTask:     hermes.SubTask{ID: "s1", Description: "Update API timeout handling"},
		Result:      "passed",
		Tokens:      42,
		Completed:   1,
		ChecklistMapping: &ChecklistMapping{
			SubTaskID:           "s1",
			SubTaskDescription:  "Update API timeout handling",
			ChecklistText:       "Update API timeout handling",
			ChecklistLineNumber: 0,
			Confidence:          ChecklistMappingConfidenceHigh,
			Score:               100,
		},
		Review: &ReviewEvidence{
			Verdict:   "allow",
			Score:     95,
			Summary:   "review passed",
			Reference: "review:abc123",
		},
		Validation: &ValidationEvidence{
			Command:   "go test ./internal/app/issueops",
			Passed:    true,
			ExitCode:  0,
			Output:    "ok",
			Reference: "cmd:go test ./internal/app/issueops",
		},
	})
	if err != nil {
		t.Fatalf("RecordEvidence() error = %v", err)
	}

	body, err := os.ReadFile(commentPath)
	if err != nil {
		t.Fatalf("read comment body: %v", err)
	}
	text := string(body)
	checks := []string{
		"Evidence mapping summary",
		"Checklist item: Update API timeout handling",
		"Hermes sub-task: Update API timeout handling",
		"Sub-task ID: s1",
		"Confidence: high (100)",
		"subtask: Update API timeout handling",
		"review: allow score=95",
		"validation: go test ./internal/app/issueops",
	}
	for _, want := range checks {
		if !strings.Contains(text, want) {
			t.Fatalf("comment body missing %q:\n%s", want, text)
		}
	}
}

func TestEvidenceBackedChecklistSyncUsesMappingPreview(t *testing.T) {
	projectDir := t.TempDir()
	ghDir := t.TempDir()
	commentPath := filepath.Join(projectDir, "comment.txt")
	issueJSON := `{"title":"IssueOps evidence sync","body":"Intro\n- [ ] Update API timeout handling\n- [ ] Write validation tests\n","state":"open","labels":[{"name":"enhancement"}],"comments":[]}`
	writeExecutableScript(t, ghDir, "gh", "#!/bin/sh\nset -eu\ncase \"$*\" in\n  \"issue view 33 --json title,body,state,labels,comments\")\n    cat <<'JSON'\n"+issueJSON+"\nJSON\n    exit 0\n    ;;\n  issue\\ comment\\ 33\\ --body\\ *)\n    printf '%s' \"$5\" > \""+commentPath+"\"\n    exit 0\n    ;;\n  issue\\ edit\\ *)\n    printf 'unexpected write: %s\\n' \"$*\" >&2\n    exit 1\n    ;;\nesac\nprintf 'unexpected args: %s\\n' \"$*\" >&2\nexit 1\n")
	t.Setenv("PATH", ghDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	svc := New()
	subTasks := []hermes.SubTask{
		{ID: "s1", Description: "Update API timeout handling", Status: hermes.SubTaskDone},
		{ID: "s2", Description: "Write validation tests", Status: hermes.SubTaskPending},
	}
	mapping, err := svc.LoadIssueChecklistMapping(context.Background(), projectDir, 33, subTasks)
	if err != nil {
		t.Fatalf("LoadIssueChecklistMapping() error = %v", err)
	}
	if mapping.State != hermes.IssueStateChecklistSynced {
		t.Fatalf("mapping state = %q, want %q", mapping.State, hermes.IssueStateChecklistSynced)
	}
	if mapping.NeedsHumanConfirmation {
		t.Fatal("high-confidence mapping should not require human confirmation")
	}

	err = svc.RecordEvidence(context.Background(), RecordEvidenceRequest{
		ProjectDir:       projectDir,
		IssueNumber:      33,
		Comment:          true,
		SubTask:          subTasks[0],
		Result:           "passed",
		Tokens:           24,
		Completed:        1,
		ChecklistMapping: &mapping.Mappings[0],
		Review: &ReviewEvidence{
			Verdict:   "allow",
			Score:     92,
			Summary:   "review passed",
			Reference: "review:issue-33",
		},
		Validation: &ValidationEvidence{
			Command:   "go test ./internal/app/issueops",
			Passed:    true,
			ExitCode:  0,
			Output:    "ok",
			Reference: "cmd:go test ./internal/app/issueops",
		},
	})
	if err != nil {
		t.Fatalf("RecordEvidence() error = %v", err)
	}

	body, err := os.ReadFile(commentPath)
	if err != nil {
		t.Fatalf("read comment body: %v", err)
	}
	text := string(body)
	for _, want := range []string{"Evidence mapping summary", "Evidence sources", "review: allow score=92", "validation: go test ./internal/app/issueops"} {
		if !strings.Contains(text, want) {
			t.Fatalf("evidence comment missing %q:\n%s", want, text)
		}
	}

	syncResult, err := svc.SyncChecklist(context.Background(), SyncChecklistRequest{
		ProjectDir:       projectDir,
		IssueNumber:      33,
		SubTasks:         subTasks,
		DryRun:           true,
		ChecklistMapping: &mapping,
	})
	if err != nil {
		t.Fatalf("SyncChecklist() error = %v", err)
	}
	if syncResult.State != hermes.IssueStateChecklistSynced {
		t.Fatalf("sync state = %q, want %q", syncResult.State, hermes.IssueStateChecklistSynced)
	}
	if !syncResult.WouldWrite || syncResult.Wrote {
		t.Fatalf("dry-run sync state = %+v", syncResult)
	}
	if syncResult.Guard.NeedsHumanConfirmation {
		t.Fatalf("dry-run sync should not need human confirmation: %+v", syncResult.Guard)
	}
	if !syncResult.Guard.CanWrite() {
		t.Fatalf("guard unexpectedly blocked dry-run preview: %+v", syncResult.Guard)
	}
	if len(syncResult.Preview.UpdatedChecklistItems) != 1 || syncResult.Preview.UpdatedChecklistItems[0] != "Update API timeout handling" {
		t.Fatalf("unexpected updated checklist items: %+v", syncResult.Preview.UpdatedChecklistItems)
	}
	if !strings.Contains(strings.Join(syncResult.Notes, "\n"), "will update checklist items") {
		t.Fatalf("sync notes missing update summary: %+v", syncResult.Notes)
	}
}

func TestSyncChecklist_DryRunPreviewDoesNotWrite(t *testing.T) {
	projectDir := t.TempDir()
	ghDir := t.TempDir()
	issueJSON := `{"title":"IssueOps dry-run","body":"Intro\n- [ ] Update API timeout handling\n- [ ] Write validation tests\nUser note: keep this block.\n","state":"open","labels":[{"name":"enhancement"}],"comments":[]}`
	writeExecutableScript(t, ghDir, "gh", "#!/bin/sh\nset -eu\ncase \"$*\" in\n  \"issue view 17 --json title,body,state,labels,comments\")\n    cat <<'JSON'\n"+issueJSON+"\nJSON\n    exit 0\n    ;;\n  issue\\ edit\\ *)\n    printf 'unexpected write: %s\\n' \"$*\" >&2\n    exit 1\n    ;;\nesac\nprintf 'unexpected args: %s\\n' \"$*\" >&2\nexit 1\n")
	t.Setenv("PATH", ghDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	svc := New()
	got, err := svc.SyncChecklist(context.Background(), SyncChecklistRequest{
		ProjectDir:  projectDir,
		IssueNumber: 17,
		SubTasks: []hermes.SubTask{
			{ID: "s1", Description: "Update API timeout handling", Status: hermes.SubTaskDone},
			{ID: "s2", Description: "Write validation tests", Status: hermes.SubTaskPending},
		},
		DryRun: true,
	})
	if err != nil {
		t.Fatalf("SyncChecklist() error = %v", err)
	}
	if got.Wrote {
		t.Fatal("dry-run should not write to GitHub")
	}
	if !got.WouldWrite {
		t.Fatal("dry-run should still report that a write would happen")
	}
	if !got.Guard.CanWrite() {
		t.Fatalf("guard unexpectedly blocked preview: %+v", got.Guard)
	}
	if len(got.Preview.UpdatedChecklistItems) != 1 || got.Preview.UpdatedChecklistItems[0] != "Update API timeout handling" {
		t.Fatalf("unexpected updated checklist items: %+v", got.Preview.UpdatedChecklistItems)
	}
	if !strings.Contains(got.Preview.BodyAfter, "User note: keep this block.") {
		t.Fatalf("dry-run preview dropped user content:\n%s", got.Preview.BodyAfter)
	}
	if !strings.Contains(got.Preview.BodyAfter, "- [x] Update API timeout handling") {
		t.Fatalf("dry-run preview missing checked item:\n%s", got.Preview.BodyAfter)
	}
}

func TestSyncChecklist_GuardBlocksHumanConfirmation(t *testing.T) {
	projectDir := t.TempDir()
	ghDir := t.TempDir()
	issueJSON := `{"title":"IssueOps guard","body":"Intro\n- [ ] Sync checklist mapping with review\n","state":"open","labels":[{"name":"enhancement"}],"comments":[]}`
	writeExecutableScript(t, ghDir, "gh", "#!/bin/sh\nset -eu\ncase \"$*\" in\n  \"issue view 18 --json title,body,state,labels,comments\")\n    cat <<'JSON'\n"+issueJSON+"\nJSON\n    exit 0\n    ;;\n  issue\\ edit\\ *)\n    printf 'unexpected write: %s\\n' \"$*\" >&2\n    exit 1\n    ;;\nesac\nprintf 'unexpected args: %s\\n' \"$*\" >&2\nexit 1\n")
	t.Setenv("PATH", ghDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	svc := New()
	got, err := svc.SyncChecklist(context.Background(), SyncChecklistRequest{
		ProjectDir:  projectDir,
		IssueNumber: 18,
		SubTasks: []hermes.SubTask{
			{ID: "s1", Description: "Sync checklist mapping with review", Status: hermes.SubTaskDone},
		},
		ChecklistMapping: &ChecklistMappingResult{NeedsHumanConfirmation: true},
	})
	if err != nil {
		t.Fatalf("SyncChecklist() error = %v", err)
	}
	if got.Wrote {
		t.Fatal("guarded sync should not write to GitHub")
	}
	if got.Guard.CanWrite() {
		t.Fatalf("expected guard to block write: %+v", got.Guard)
	}
	if !strings.Contains(strings.Join(got.Notes, "\n"), "human confirmation") {
		t.Fatalf("expected human confirmation note, got: %+v", got.Notes)
	}
}

func TestSyncChecklist_GhFailureReturnsBlockedRecovery(t *testing.T) {
	projectDir := t.TempDir()
	ghDir := t.TempDir()
	issueJSON := `{"title":"IssueOps blocked","body":"Intro\n- [ ] Update API timeout handling\n","state":"open","labels":[{"name":"enhancement"}],"comments":[]}`
	writeExecutableScript(t, ghDir, "gh", "#!/bin/sh\nset -eu\ncase \"$*\" in\n  \"issue view 19 --json title,body,state,labels,comments\")\n    cat <<'JSON'\n"+issueJSON+"\nJSON\n    exit 0\n    ;;\n  issue\\ edit\\ *)\n    printf 'synthetic sync failure\\n' >&2\n    exit 1\n    ;;\nesac\nprintf 'unexpected args: %s\\n' \"$*\" >&2\nexit 1\n")
	t.Setenv("PATH", ghDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	svc := New()
	got, err := svc.SyncChecklist(context.Background(), SyncChecklistRequest{
		ProjectDir:  projectDir,
		IssueNumber: 19,
		SubTasks: []hermes.SubTask{
			{ID: "s1", Description: "Update API timeout handling", Status: hermes.SubTaskDone},
		},
	})
	if err == nil {
		t.Fatal("SyncChecklist() error = nil, want blocked recovery error")
	}
	if got.State != hermes.IssueStateBlocked {
		t.Fatalf("State = %q, want %q", got.State, hermes.IssueStateBlocked)
	}
	if got.Recovery == nil {
		t.Fatal("expected recovery metadata on blocked sync failure")
	}
	if got.Recovery.RetryAction != "retry-sync-checklist" || !got.Recovery.Retryable {
		t.Fatalf("recovery = %+v", got.Recovery)
	}
	if !strings.Contains(got.Recovery.Error, "synthetic sync failure") {
		t.Fatalf("recovery error = %q", got.Recovery.Error)
	}
}

func TestBuildCloseReadiness(t *testing.T) {
	baseIssue := &hermes.IssueContext{
		Number: 101,
		Title:  "IssueOps service",
		Body:   "body",
		State:  "open",
		Labels: []string{"enhancement", "ready"},
		Checklist: []hermes.ChecklistItem{
			{Text: "done", Checked: true, LineNumber: 1},
			{Text: "todo", Checked: false, LineNumber: 2},
		},
		Comments: []hermes.IssueComment{
			{
				Author: "hermes",
				Body: hermes.CommentDoneWithNote(hermes.TaskState{
					Plan: []hermes.SubTask{
						{Status: hermes.SubTaskDone},
						{Status: hermes.SubTaskDone},
					},
				}, ""),
				CreatedAt: time.Now(),
			},
		},
	}

	tests := []struct {
		name                string
		issue               *hermes.IssueContext
		req                 AssessCloseReadinessRequest
		want                hermes.IssueState
		auto                bool
		wantBlockingLabel   bool
		wantRequiredLabel   bool
		wantChecklistSynced bool
		wantReviewAccepted  bool
		wantValidationPass  bool
	}{
		{
			name:  "happy_path",
			issue: &hermes.IssueContext{Number: 1, State: "open", Labels: []string{"ready"}},
			req: AssessCloseReadinessRequest{
				RequiredCloseLabel: "ready",
				ReviewAccepted:     true,
				ValidationPassed:   true,
			},
			want:                hermes.IssueStateReadyToClose,
			auto:                true,
			wantRequiredLabel:   true,
			wantChecklistSynced: true,
			wantReviewAccepted:  true,
			wantValidationPass:  true,
		},
		{
			name:  "checklist_unsynced",
			issue: baseIssue,
			req: AssessCloseReadinessRequest{
				RequiredCloseLabel: "ready",
				ReviewAccepted:     true,
				ValidationPassed:   true,
			},
			want:                hermes.IssueStateChecklistUnsynced,
			auto:                false,
			wantRequiredLabel:   true,
			wantChecklistSynced: false,
			wantReviewAccepted:  true,
			wantValidationPass:  true,
		},
		{
			name:  "blocked_issue_state",
			issue: &hermes.IssueContext{Number: 2, State: "blocked", Labels: []string{"ready"}},
			req: AssessCloseReadinessRequest{
				RequiredCloseLabel: "ready",
				ReviewAccepted:     true,
				ValidationPassed:   true,
			},
			want:                hermes.IssueStateBlocked,
			auto:                false,
			wantBlockingLabel:   false,
			wantRequiredLabel:   true,
			wantChecklistSynced: true,
			wantReviewAccepted:  true,
			wantValidationPass:  true,
		},
		{
			name:  "blocked_label",
			issue: &hermes.IssueContext{Number: 22, State: "open", Labels: []string{"ready", "blocked"}},
			req: AssessCloseReadinessRequest{
				RequiredCloseLabel: "ready",
				ReviewAccepted:     true,
				ValidationPassed:   true,
			},
			want:                hermes.IssueStateBlocked,
			auto:                false,
			wantBlockingLabel:   true,
			wantRequiredLabel:   true,
			wantChecklistSynced: true,
			wantReviewAccepted:  true,
			wantValidationPass:  true,
		},
		{
			name:  "review_rejected",
			issue: &hermes.IssueContext{Number: 23, State: "open", Labels: []string{"ready"}},
			req: AssessCloseReadinessRequest{
				RequiredCloseLabel: "ready",
				ReviewAccepted:     false,
				ValidationPassed:   true,
			},
			want:                hermes.IssueStateBlocked,
			auto:                false,
			wantRequiredLabel:   true,
			wantChecklistSynced: true,
			wantReviewAccepted:  false,
			wantValidationPass:  true,
		},
		{
			name:  "validation_failed",
			issue: &hermes.IssueContext{Number: 24, State: "open", Labels: []string{"ready"}},
			req: AssessCloseReadinessRequest{
				RequiredCloseLabel: "ready",
				ReviewAccepted:     true,
				ValidationPassed:   false,
			},
			want:                hermes.IssueStateBlocked,
			auto:                false,
			wantRequiredLabel:   true,
			wantChecklistSynced: true,
			wantReviewAccepted:  true,
			wantValidationPass:  false,
		},
		{
			name:  "missing_required_label",
			issue: &hermes.IssueContext{Number: 25, State: "open", Labels: []string{"enhancement"}},
			req: AssessCloseReadinessRequest{
				RequiredCloseLabel: "ready",
				ReviewAccepted:     true,
				ValidationPassed:   true,
			},
			want:                hermes.IssueStateChecklistSynced,
			auto:                false,
			wantRequiredLabel:   false,
			wantChecklistSynced: true,
			wantReviewAccepted:  true,
			wantValidationPass:  true,
		},
		{
			name:  "closed",
			issue: &hermes.IssueContext{Number: 3, State: "closed", Labels: []string{"ready"}},
			req: AssessCloseReadinessRequest{
				RequiredCloseLabel: "ready",
				ReviewAccepted:     true,
				ValidationPassed:   true,
			},
			want:                hermes.IssueStateClosed,
			auto:                false,
			wantRequiredLabel:   true,
			wantChecklistSynced: true,
			wantReviewAccepted:  true,
			wantValidationPass:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildCloseReadiness(tt.issue, tt.req)
			if got.State != tt.want {
				t.Fatalf("State = %q, want %q", got.State, tt.want)
			}
			if got.CanAutoClose != tt.auto {
				t.Fatalf("CanAutoClose = %v, want %v", got.CanAutoClose, tt.auto)
			}
			if got.Guard.HasBlockingLabel != tt.wantBlockingLabel {
				t.Fatalf("Guard.HasBlockingLabel = %v, want %v", got.Guard.HasBlockingLabel, tt.wantBlockingLabel)
			}
			if got.HasRequiredLabel != tt.wantRequiredLabel {
				t.Fatalf("HasRequiredLabel = %v, want %v", got.HasRequiredLabel, tt.wantRequiredLabel)
			}
			if got.Guard.ChecklistSynced != tt.wantChecklistSynced {
				t.Fatalf("Guard.ChecklistSynced = %v, want %v", got.Guard.ChecklistSynced, tt.wantChecklistSynced)
			}
			if got.Guard.ReviewAccepted != tt.wantReviewAccepted {
				t.Fatalf("Guard.ReviewAccepted = %v, want %v", got.Guard.ReviewAccepted, tt.wantReviewAccepted)
			}
			if got.Guard.ValidationPassed != tt.wantValidationPass {
				t.Fatalf("Guard.ValidationPassed = %v, want %v", got.Guard.ValidationPassed, tt.wantValidationPass)
			}
		})
	}
}

func TestCloseIssue_DoesNotMutateWhenGuardBlocks(t *testing.T) {
	projectDir := t.TempDir()
	ghDir := t.TempDir()
	issueJSON := `{"title":"IssueOps close guard","body":"Intro\n- [x] keep open\n","state":"open","labels":[{"name":"enhancement"}],"comments":[]}`
	writeExecutableScript(t, ghDir, "gh", "#!/bin/sh\nset -eu\ncase \"$*\" in\n  \"issue view 21 --json title,body,state,labels,comments\")\n    cat <<'JSON'\n"+issueJSON+"\nJSON\n    exit 0\n    ;;\n  issue\\ close\\ *)\n    printf 'unexpected close attempt: %s\\n' \"$*\" >&2\n    exit 1\n    ;;\nesac\nprintf 'unexpected args: %s\\n' \"$*\" >&2\nexit 1\n")
	t.Setenv("PATH", ghDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	svc := New()
	got, err := svc.CloseIssue(context.Background(), CloseIssueRequest{
		AssessCloseReadinessRequest: AssessCloseReadinessRequest{
			ProjectDir:         projectDir,
			IssueNumber:        21,
			RequiredCloseLabel: "ready",
			ReviewAccepted:     true,
			ValidationPassed:   true,
		},
	})
	if err != nil {
		t.Fatalf("CloseIssue() error = %v, want nil when guard blocks", err)
	}
	if got.CanAutoClose {
		t.Fatal("guard-blocked issue should not be auto-closeable")
	}
	if got.State != hermes.IssueStateChecklistSynced {
		t.Fatalf("State = %q, want %q", got.State, hermes.IssueStateChecklistSynced)
	}
	if got.Closed {
		t.Fatal("guard-blocked issue should not be marked closed")
	}
	if got.HasRequiredLabel {
		t.Fatal("missing required label should be reported")
	}
}

func TestCloseIssue_GhFailureReturnsBlockedRecovery(t *testing.T) {
	projectDir := t.TempDir()
	ghDir := t.TempDir()
	issueJSON := `{"title":"IssueOps close","body":"Intro\n","state":"open","labels":[{"name":"ready"}],"comments":[]}`
	writeExecutableScript(t, ghDir, "gh", "#!/bin/sh\nset -eu\ncase \"$*\" in\n  \"issue view 20 --json title,body,state,labels,comments\")\n    cat <<'JSON'\n"+issueJSON+"\nJSON\n    exit 0\n    ;;\n  issue\\ close\\ *)\n    printf 'synthetic close failure\\n' >&2\n    exit 1\n    ;;\nesac\nprintf 'unexpected args: %s\\n' \"$*\" >&2\nexit 1\n")
	t.Setenv("PATH", ghDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	svc := New()
	got, err := svc.CloseIssue(context.Background(), CloseIssueRequest{
		AssessCloseReadinessRequest: AssessCloseReadinessRequest{
			ProjectDir:         projectDir,
			IssueNumber:        20,
			RequiredCloseLabel: "ready",
			ReviewAccepted:     true,
			ValidationPassed:   true,
		},
	})
	if err == nil {
		t.Fatal("CloseIssue() error = nil, want blocked recovery error")
	}
	if got.State != hermes.IssueStateBlocked {
		t.Fatalf("State = %q, want %q", got.State, hermes.IssueStateBlocked)
	}
	if got.Recovery == nil {
		t.Fatal("expected recovery metadata on blocked close failure")
	}
	if got.Recovery.RetryAction != "retry-close" || !got.Recovery.Retryable {
		t.Fatalf("recovery = %+v", got.Recovery)
	}
	if got.Closed {
		t.Fatal("close failure should not mark issue closed")
	}
}
