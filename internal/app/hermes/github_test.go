package hermes

import (
	"strings"
	"testing"
)

// ── ExtractChecklist ──────────────────────────────────────────────────────────

func TestExtractChecklist_Basic(t *testing.T) {
	body := "Some intro\n- [ ] Task A\n- [x] Task B\n- [ ] Task C\n"
	items := ExtractChecklist(body)
	if len(items) != 3 {
		t.Fatalf("want 3 items, got %d", len(items))
	}
	if items[0].Text != "Task A" || items[0].Checked {
		t.Errorf("item 0: got %+v", items[0])
	}
	if items[1].Text != "Task B" || !items[1].Checked {
		t.Errorf("item 1: got %+v", items[1])
	}
	if items[2].Text != "Task C" || items[2].Checked {
		t.Errorf("item 2: got %+v", items[2])
	}
}

func TestExtractChecklist_LineNumbers(t *testing.T) {
	body := "intro\n- [ ] First\n\n- [x] Second\n"
	items := ExtractChecklist(body)
	if len(items) != 2 {
		t.Fatalf("want 2, got %d", len(items))
	}
	// "intro" is line 0, "- [ ] First" is line 1
	if items[0].LineNumber != 1 {
		t.Errorf("First item LineNumber: want 1, got %d", items[0].LineNumber)
	}
	// blank line is line 2, "- [x] Second" is line 3
	if items[1].LineNumber != 3 {
		t.Errorf("Second item LineNumber: want 3, got %d", items[1].LineNumber)
	}
}

func TestExtractChecklist_Empty(t *testing.T) {
	items := ExtractChecklist("No checklist here.\n")
	if len(items) != 0 {
		t.Errorf("want 0 items, got %d", len(items))
	}
}

func TestExtractChecklist_UppercaseX(t *testing.T) {
	body := "- [X] Done uppercase\n"
	items := ExtractChecklist(body)
	if len(items) != 1 || !items[0].Checked {
		t.Errorf("uppercase X should be treated as checked: %+v", items)
	}
}

// ── UpdateChecklistInBody ─────────────────────────────────────────────────────

func TestUpdateChecklistInBody_ExactMatch(t *testing.T) {
	body := "## Tasks\n- [ ] Implement auth\n- [ ] Write tests\n- [x] Deploy\n"
	updated := UpdateChecklistInBody(body, []string{"Implement auth"})
	if !strings.Contains(updated, "- [x] Implement auth") {
		t.Errorf("expected auth to be checked, got:\n%s", updated)
	}
	if !strings.Contains(updated, "- [ ] Write tests") {
		t.Errorf("write tests should remain unchecked, got:\n%s", updated)
	}
}

func TestUpdateChecklistInBody_CaseInsensitive(t *testing.T) {
	body := "- [ ] Implement Auth\n"
	updated := UpdateChecklistInBody(body, []string{"implement auth"})
	if !strings.Contains(updated, "- [x] Implement Auth") {
		t.Errorf("case-insensitive match failed:\n%s", updated)
	}
}

func TestUpdateChecklistInBody_PrefixMatch(t *testing.T) {
	body := "- [ ] Implement the authentication system\n"
	// desc is shorter but shares the first 16 chars prefix
	updated := UpdateChecklistInBody(body, []string{"Implement the au"})
	if !strings.Contains(updated, "- [x] Implement the authentication system") {
		t.Errorf("prefix match failed:\n%s", updated)
	}
}

func TestUpdateChecklistInBody_AlreadyChecked(t *testing.T) {
	body := "- [x] Already done\n"
	updated := UpdateChecklistInBody(body, []string{"Already done"})
	if updated != body {
		t.Errorf("already-checked item should not change: got %q", updated)
	}
}

func TestUpdateChecklistInBody_NoMatch(t *testing.T) {
	body := "- [ ] Task A\n"
	updated := UpdateChecklistInBody(body, []string{"Task B"})
	if updated != body {
		t.Errorf("no-match should not change body: got %q", updated)
	}
}

func TestUpdateChecklistInBody_MultipleCompleted(t *testing.T) {
	body := "- [ ] Step 1\n- [ ] Step 2\n- [ ] Step 3\n"
	updated := UpdateChecklistInBody(body, []string{"Step 1", "Step 3"})
	if !strings.Contains(updated, "- [x] Step 1") {
		t.Errorf("step 1 should be checked")
	}
	if !strings.Contains(updated, "- [ ] Step 2") {
		t.Errorf("step 2 should remain unchecked")
	}
	if !strings.Contains(updated, "- [x] Step 3") {
		t.Errorf("step 3 should be checked")
	}
}

// ── CommentStarted / CommentDone / CommentFailed / CommentBudgetExceeded ─────

func TestCommentStarted(t *testing.T) {
	body := CommentStarted("claude-opus-4-7", "claude-haiku-4-5")
	if !strings.Contains(body, "claude-opus-4-7") {
		t.Errorf("planner model missing: %s", body)
	}
	if !strings.Contains(body, "claude-haiku-4-5") {
		t.Errorf("executor model missing: %s", body)
	}
	if !strings.Contains(body, "🤖") {
		t.Errorf("emoji missing: %s", body)
	}
}

func TestCommentDone_Fields(t *testing.T) {
	state := TaskState{
		Plan: []SubTask{
			{Status: SubTaskDone},
			{Status: SubTaskDone},
			{Status: SubTaskFailed},
		},
		TokenBudget: TokenBudget{UsedTokens: 12345, MaxTotalTokens: 50000},
	}
	body := CommentDone(state)
	if !strings.Contains(body, "2/3") {
		t.Errorf("done count wrong: %s", body)
	}
	if !strings.Contains(body, "12345") {
		t.Errorf("token count missing: %s", body)
	}
}

func TestCommentDone_WithArtifacts(t *testing.T) {
	state := TaskState{
		Plan:      []SubTask{{Status: SubTaskDone}},
		Artifacts: []Artifact{{Path: "internal/foo.go", Hash: "abcdef123"}},
	}
	body := CommentDone(state)
	if !strings.Contains(body, "internal/foo.go") {
		t.Errorf("artifact path missing: %s", body)
	}
}

func TestCommentFailed_Fields(t *testing.T) {
	body := CommentFailed("Parse input", "timeout", 3, 7)
	if !strings.Contains(body, "Parse input") {
		t.Errorf("failed desc missing: %s", body)
	}
	if !strings.Contains(body, "timeout") {
		t.Errorf("reason missing: %s", body)
	}
	if !strings.Contains(body, "3/7") {
		t.Errorf("progress count missing: %s", body)
	}
}

func TestCommentBudgetExceeded_Fields(t *testing.T) {
	body := CommentBudgetExceeded(90000, 100000)
	if !strings.Contains(body, "90000") || !strings.Contains(body, "100000") {
		t.Errorf("token counts missing: %s", body)
	}
	if !strings.Contains(body, "⚠️") {
		t.Errorf("emoji missing: %s", body)
	}
}

// ── BuildGoalFromIssue ────────────────────────────────────────────────────────

func TestBuildGoalFromIssue_WithChecklist(t *testing.T) {
	issue := &IssueContext{
		Number: 42,
		Title:  "Fix login bug",
		Body:   "Some body text\n- [ ] Step A\n- [x] Step B\n",
		Checklist: []ChecklistItem{
			{Text: "Step A", Checked: false},
			{Text: "Step B", Checked: true},
		},
	}
	goal := BuildGoalFromIssue(issue)
	if !strings.Contains(goal, "#42") {
		t.Errorf("issue number missing: %s", goal)
	}
	if !strings.Contains(goal, "Step A") {
		t.Errorf("unchecked item missing: %s", goal)
	}
	if strings.Contains(goal, "Step B") {
		// already checked - should not appear in task list guidance
		// (it appears in the full body but not in the unchecked list)
	}
	if !strings.Contains(goal, "use these as SubTask") {
		t.Errorf("planner hint missing: %s", goal)
	}
}

func TestBuildGoalFromIssue_NoChecklist(t *testing.T) {
	issue := &IssueContext{
		Number: 10,
		Title:  "Refactor",
		Body:   "Just a description, no checkboxes.",
	}
	goal := BuildGoalFromIssue(issue)
	if strings.Contains(goal, "use these as SubTask") {
		t.Errorf("should not contain checklist hint when no unchecked items: %s", goal)
	}
	if !strings.Contains(goal, "Refactor") {
		t.Errorf("title missing: %s", goal)
	}
}

// ── HasLabel ──────────────────────────────────────────────────────────────────

func TestHasLabel(t *testing.T) {
	issue := &IssueContext{Labels: []string{"hermes-auto-close", "complexity:medium"}}
	if !HasLabel(issue, "hermes-auto-close") {
		t.Error("should find hermes-auto-close")
	}
	if !HasLabel(issue, "Hermes-Auto-Close") {
		t.Error("label check should be case-insensitive")
	}
	if HasLabel(issue, "hermes-failed") {
		t.Error("should not find hermes-failed")
	}
}

// ── CommentSubTaskProgress ────────────────────────────────────────────────────

func TestCommentSubTaskProgress_BasicFields(t *testing.T) {
	subTask := SubTask{
		Description: "Implement feature X",
		Result:      "Feature X implemented",
		Status:      SubTaskDone,
	}
	body := CommentSubTaskProgress(0, 5, subTask, "Success: feature added", 1500, 1)
	if !strings.Contains(body, "✅") {
		t.Errorf("emoji missing: %s", body)
	}
	if !strings.Contains(body, "子任務進度") {
		t.Errorf("title missing: %s", body)
	}
	if !strings.Contains(body, "1/5") {
		t.Errorf("progress count (1-based indexing) missing: %s", body)
	}
	if !strings.Contains(body, "Implement feature X") {
		t.Errorf("subtask description missing: %s", body)
	}
	if !strings.Contains(body, "Success: feature added") {
		t.Errorf("result text missing: %s", body)
	}
	if !strings.Contains(body, "1500") {
		t.Errorf("executor tokens missing: %s", body)
	}
	if !strings.Contains(body, "1/5") {
		t.Errorf("completion progress missing: %s", body)
	}
}

func TestCommentSubTaskProgress_MiddleIndex(t *testing.T) {
	subTask := SubTask{
		Description: "Step 2",
		Status:      SubTaskDone,
	}
	body := CommentSubTaskProgress(2, 4, subTask, "Completed", 2000, 3)
	if !strings.Contains(body, "3/4") {
		t.Errorf("middle index (idx=2 should show 3/4) missing: %s", body)
	}
	if !strings.Contains(body, "3/4") {
		t.Errorf("completed count (3/4) missing: %s", body)
	}
}

func TestCommentSubTaskProgress_LastSubTask(t *testing.T) {
	subTask := SubTask{
		Description: "Final step",
		Status:      SubTaskDone,
	}
	body := CommentSubTaskProgress(4, 5, subTask, "All done", 1000, 5)
	if !strings.Contains(body, "5/5") {
		t.Errorf("last step progress missing: %s", body)
	}
}

func TestCommentSubTaskProgress_LongDescription(t *testing.T) {
	longDesc := "Fix the critical authentication flow that handles multi-factor verification across all provider types"
	subTask := SubTask{
		Description: longDesc,
		Status:      SubTaskDone,
	}
	body := CommentSubTaskProgress(0, 3, subTask, "Fixed", 5000, 1)
	if !strings.Contains(body, longDesc) {
		t.Errorf("long description should be fully included: %s", body)
	}
}
