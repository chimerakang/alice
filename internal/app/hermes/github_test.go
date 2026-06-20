package hermes

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeExecutableScript(t *testing.T, dir, name, body string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write script %s: %v", name, err)
	}
	return path
}

// ── ghOutputWithRetry ─────────────────────────────────────────────────────────

// fakeGHCounting installs a fake gh that records its call count in a file and
// delegates behavior to the given script body (which can read $count).
func fakeGHCounting(t *testing.T, body string) string {
	t.Helper()
	tmp := t.TempDir()
	countFile := filepath.Join(tmp, "count")
	script := `#!/bin/sh
count_file="$FAKE_GH_COUNT"
count=$(cat "$count_file" 2>/dev/null || echo 0)
count=$((count + 1))
printf '%s' "$count" >"$count_file"
` + body
	writeExecutableScript(t, tmp, "gh", script)
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_GH_COUNT", countFile)
	return countFile
}

func ghCallCount(t *testing.T, countFile string) string {
	t.Helper()
	b, err := os.ReadFile(countFile)
	if err != nil {
		t.Fatalf("read count file: %v", err)
	}
	return string(b)
}

func TestFetchIssueRetriesTransientFailure(t *testing.T) {
	countFile := fakeGHCounting(t, `
if [ "$count" -lt 2 ]; then
  echo "HTTP 502: Bad Gateway (https://api.github.com/graphql)" >&2
  exit 1
fi
printf '{"title":"ok after retry","state":"OPEN","body":"","labels":[],"comments":[]}\n'
`)

	oldBackoff := ghRetryBackoff
	ghRetryBackoff = 10 * time.Millisecond
	defer func() { ghRetryBackoff = oldBackoff }()

	issue, err := FetchIssue(context.Background(), "", 7)
	if err != nil {
		t.Fatalf("FetchIssue should succeed after one retry: %v", err)
	}
	if issue.Title != "ok after retry" {
		t.Fatalf("unexpected issue: %+v", issue)
	}
	if got := ghCallCount(t, countFile); got != "2" {
		t.Fatalf("want 2 gh calls, got %s", got)
	}
}

func TestFetchIssueNotFoundSkipsRetryAndEmbedsStderr(t *testing.T) {
	countFile := fakeGHCounting(t, `
echo "GraphQL: Could not resolve to an Issue or PullRequest with the number of 999. (repository.issue)" >&2
exit 1
`)

	oldBackoff := ghRetryBackoff
	ghRetryBackoff = 10 * time.Millisecond
	defer func() { ghRetryBackoff = oldBackoff }()

	_, err := FetchIssue(context.Background(), "", 999)
	if err == nil {
		t.Fatal("FetchIssue should fail")
	}
	if !strings.Contains(err.Error(), "Could not resolve to an Issue") {
		t.Fatalf("error should embed gh stderr, got: %v", err)
	}
	if got := ghCallCount(t, countFile); got != "1" {
		t.Fatalf("not-found should not retry; want 1 gh call, got %s", got)
	}
}

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

func TestExtractChecklist_StableIDsAndSections(t *testing.T) {
	body := "intro\n" +
		"## Acceptance Criteria\n" +
		"- [ ] Build passes\n" +
		"- [ ] Tests added\n" +
		"\n" +
		"## Notes\n" +
		"- [ ] Optional follow-up\n"
	items := ExtractChecklist(body)
	if len(items) != 3 {
		t.Fatalf("want 3 items, got %d (%+v)", len(items), items)
	}
	if items[0].ID != "item-2" || items[0].Section != "Acceptance Criteria" {
		t.Errorf("item 0 id/section: got %q/%q", items[0].ID, items[0].Section)
	}
	if items[1].ID != "item-3" || items[1].Section != "Acceptance Criteria" {
		t.Errorf("item 1 id/section: got %q/%q", items[1].ID, items[1].Section)
	}
	if items[2].ID != "item-6" || items[2].Section != "Notes" {
		t.Errorf("item 2 id/section: got %q/%q", items[2].ID, items[2].Section)
	}
}

func TestExtractChecklist_IgnoresHermesPlanSection(t *testing.T) {
	body := "## Acceptance\n" +
		"- [ ] Original item\n\n" +
		"## Hermes 執行計劃\n\n" +
		"- [ ] Internal executor step\n" +
		"- [x] Internal finished step\n\n" +
		"## Notes\n" +
		"- [ ] Follow-up note\n"
	items := ExtractChecklist(body)
	if len(items) != 2 {
		t.Fatalf("want 2 non-Hermes checklist items, got %d (%+v)", len(items), items)
	}
	if items[0].Text != "Original item" || items[1].Text != "Follow-up note" {
		t.Fatalf("unexpected checklist items: %+v", items)
	}
}

func TestBuildChecklistSyncPreview_DeclaredItemsTicked(t *testing.T) {
	body := "## Acceptance\n" +
		"- [ ] Build passes\n" +
		"- [ ] Tests added\n" +
		"- [ ] Docs updated\n"
	subtasks := []SubTask{
		{ID: "s1", Description: "do build work", Status: SubTaskDone, ChecklistItemIDs: []string{"item-1"}},
		{ID: "s2", Description: "add tests", Status: SubTaskDone, ChecklistItemIDs: []string{"item-2"}},
	}
	preview := BuildChecklistSyncPreview(body, subtasks)
	if !preview.Changed {
		t.Fatal("expected changed=true")
	}
	if !strings.Contains(preview.BodyAfter, "- [x] Build passes") {
		t.Errorf("Build passes should be ticked: %s", preview.BodyAfter)
	}
	if !strings.Contains(preview.BodyAfter, "- [x] Tests added") {
		t.Errorf("Tests added should be ticked: %s", preview.BodyAfter)
	}
	if !strings.Contains(preview.BodyAfter, "- [ ] Docs updated") {
		t.Errorf("Docs updated should remain unchecked (no declaration): %s", preview.BodyAfter)
	}
	if len(preview.UpdatedChecklistItems) != 2 {
		t.Errorf("UpdatedChecklistItems = %v, want 2", preview.UpdatedChecklistItems)
	}
}

func TestBuildChecklistSyncPreview_DeclaredModeIgnoresFuzzyMatch(t *testing.T) {
	// Reproduces #253 / #254 pattern: sub-task description happens to match
	// an item that was NOT declared. In legacy mode that item gets ticked
	// erroneously; in declarative mode it must stay unchecked.
	body := "- [ ] Refactor parser\n- [ ] Update README\n"
	subtasks := []SubTask{
		{ID: "s1", Description: "Refactor parser and update related tests", Status: SubTaskDone, ChecklistItemIDs: []string{"item-0"}},
	}
	preview := BuildChecklistSyncPreview(body, subtasks)
	if !strings.Contains(preview.BodyAfter, "- [x] Refactor parser") {
		t.Errorf("declared item should be ticked: %s", preview.BodyAfter)
	}
	if !strings.Contains(preview.BodyAfter, "- [ ] Update README") {
		t.Errorf("undeclared item must stay unchecked: %s", preview.BodyAfter)
	}
}

func TestBuildChecklistSyncPreview_LegacyFuzzyFallback(t *testing.T) {
	// No sub-task has ChecklistItemIDs declarations → legacy fuzzy mode.
	body := "- [ ] Implement auth\n- [ ] Add tests\n"
	subtasks := []SubTask{
		{ID: "s1", Description: "Implement auth", Status: SubTaskDone},
	}
	preview := BuildChecklistSyncPreview(body, subtasks)
	if !preview.Changed {
		t.Fatal("expected fuzzy fallback to tick the matching item")
	}
	if !strings.Contains(preview.BodyAfter, "- [x] Implement auth") {
		t.Errorf("fuzzy match should tick auth: %s", preview.BodyAfter)
	}
}

func TestBuildChecklistSyncPreview_LegacyFuzzyIgnoresHermesPlanSection(t *testing.T) {
	body := "## Acceptance\n" +
		"- [ ] Ship video support\n\n" +
		"## Hermes 執行計劃\n\n" +
		"- [ ] Ship video support\n"
	preview := BuildChecklistSyncPreview(body, []SubTask{
		{ID: "s1", Description: "Ship video support", Status: SubTaskDone},
	})
	if !strings.Contains(preview.BodyAfter, "- [x] Ship video support") {
		t.Fatalf("acceptance item should be checked:\n%s", preview.BodyAfter)
	}
	if strings.Contains(preview.BodyAfter, "## Hermes 執行計劃\n\n- [x] Ship video support") {
		t.Fatalf("Hermes plan section should not be checked:\n%s", preview.BodyAfter)
	}
}

func TestIsAcceptanceSection(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", true},
		{"Acceptance Criteria", true},
		{"## Definition of Done", true},
		{"驗收條件", true},
		{"完成條件", true},
		{"Notes", false},
		{"Discussion", false},
		{"Background", false},
	}
	for _, c := range cases {
		if got := IsAcceptanceSection(c.in); got != c.want {
			t.Errorf("IsAcceptanceSection(%q) = %v, want %v", c.in, got, c.want)
		}
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

func TestCommentDoneWithNote(t *testing.T) {
	body := CommentDoneWithNote(TaskState{Plan: []SubTask{{Status: SubTaskDone}}}, "Issue was not auto-closed because it is missing a label.")
	if !strings.Contains(body, "**Note:**") || !strings.Contains(body, "missing a label") {
		t.Fatalf("note missing from comment:\n%s", body)
	}
}

func TestReconcileIssueCompletion_Unchecked(t *testing.T) {
	rec := ReconcileIssueCompletion(&IssueContext{
		Number: 153,
		State:  "OPEN",
		Checklist: []ChecklistItem{
			{Text: "done", Checked: true},
			{Text: "remaining", Checked: false},
		},
	})
	if !rec.HasUnchecked() || rec.ChecklistComplete() {
		t.Fatalf("unexpected reconciliation flags: %#v", rec)
	}
	if rec.CheckedCount != 1 || rec.ChecklistTotal != 2 || len(rec.Unchecked) != 1 {
		t.Fatalf("unexpected reconciliation counts: %#v", rec)
	}
	note := rec.CommentNote()
	if !strings.Contains(note, "尚未完成") || !strings.Contains(note, "remaining") {
		t.Fatalf("unchecked note missing detail:\n%s", note)
	}
}

func TestReconcileIssueCompletion_AllChecked(t *testing.T) {
	rec := ReconcileIssueCompletion(&IssueContext{
		Number: 153,
		Checklist: []ChecklistItem{
			{Text: "done", Checked: true},
			{Text: "also done", Checked: true},
		},
	})
	if rec.HasUnchecked() || !rec.ChecklistComplete() {
		t.Fatalf("unexpected reconciliation flags: %#v", rec)
	}
	if note := rec.CommentNote(); !strings.Contains(note, "checklist is complete") || !strings.Contains(note, "2/2") {
		t.Fatalf("complete note missing detail:\n%s", note)
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
		State:  "OPEN",
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
	if !strings.Contains(goal, "Already checked issue items") || !strings.Contains(goal, "Step B") {
		t.Errorf("checked item should appear only as completed context: %s", goal)
	}
	if strings.Contains(goal, "- [x] Step B\nFull issue") || strings.Contains(goal, "Some body text\n- [ ] Step A") {
		t.Errorf("raw checklist lines should be omitted from full issue body: %s", goal)
	}
	if !strings.Contains(goal, "plan ONLY these as SubTask") {
		t.Errorf("planner hint missing: %s", goal)
	}
	if !strings.Contains(goal, "Issue State Snapshot") || !strings.Contains(goal, "1 unchecked, 1 checked") {
		t.Errorf("issue state snapshot missing: %s", goal)
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

func TestBuildIssueStateSnapshotIncludesRecentHermesSignals(t *testing.T) {
	issue := &IssueContext{
		Number: 305,
		Title:  "Result structure expansion",
		State:  "OPEN",
		Labels: []string{"backend"},
		Checklist: []ChecklistItem{
			{Text: "Add prize_pct", Checked: true},
			{Text: "Close issue after verification", Checked: false},
		},
		Comments: []IssueComment{
			{
				Author:    "alice",
				CreatedAt: time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC),
				Body:      "✅ **Hermes 完成** 1/1 SubTasks\n\n**結論**：全部 PASS。\n\n**下一步**：可關閉 issue。",
			},
			{
				Author:    "dev",
				CreatedAt: time.Date(2026, 5, 1, 10, 1, 0, 0, time.UTC),
				Body:      "unrelated note",
			},
		},
	}
	snapshot := BuildIssueStateSnapshot(issue)
	for _, want := range []string{
		"State: OPEN",
		"Labels: backend",
		"1 unchecked, 1 checked",
		"Close issue after verification",
		"Add prize_pct",
		"Recent Hermes/comment signals",
		"Latest Hermes status: complete (1/1 SubTasks",
		"IssueOps state: checklist_unsynced",
		"do not continue implementation work",
		"全部 PASS",
		"可關閉 issue",
	} {
		if !strings.Contains(snapshot, want) {
			t.Fatalf("snapshot missing %q:\n%s", want, snapshot)
		}
	}
}

func TestRecentSuccessfulHermesCompletionDetectsLatestCompleteRun(t *testing.T) {
	issue := &IssueContext{
		Comments: []IssueComment{
			{Body: "✅ **Hermes 完成** 5/5 SubTasks", CreatedAt: time.Date(2026, 5, 1, 1, 0, 0, 0, time.UTC)},
			{Body: "🤖 **Hermes 開始執行**"},
			{Body: "✅ **子任務進度** (1/1)"},
			{Author: "alice", Body: "✅ **Hermes 完成** 1/1 SubTasks", CreatedAt: time.Date(2026, 5, 1, 2, 0, 0, 0, time.UTC)},
		},
	}
	signal, ok := RecentSuccessfulHermesCompletion(issue)
	if !ok {
		t.Fatalf("expected latest complete signal")
	}
	if signal.Done != 1 || signal.Total != 1 || signal.Author != "alice" {
		t.Fatalf("unexpected signal: %+v", signal)
	}
}

func TestRecentSuccessfulHermesCompletionIgnoresFailedLatestRun(t *testing.T) {
	issue := &IssueContext{
		Comments: []IssueComment{
			{Body: "✅ **Hermes 完成** 5/5 SubTasks"},
			{Body: "❌ **Hermes 執行失敗**\n\n- 原因: tests failed"},
		},
	}
	if signal, ok := RecentSuccessfulHermesCompletion(issue); ok {
		t.Fatalf("latest failed run should not count as complete: %+v", signal)
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

func TestFetchIssueAndSyncChecklistFlow(t *testing.T) {
	tmp := t.TempDir()
	script := `#!/bin/sh
set -eu
log="$FAKE_GH_LOG"
printf '%s\n' "$*" >>"$log"
case "$*" in
  "issue view 101 --json title,body,state,labels,comments")
    cat <<'JSON'
{"title":"Hermes #101","state":"OPEN","body":"Intro\n- [ ] Step A\n- [x] Step B\n","labels":[{"name":"hermes-auto-close"},{"name":"complexity:medium"}],"comments":[{"author":{"login":"alice"},"body":"✅ **Hermes 完成** 1/1 SubTasks\n\n**結論**：PASS","createdAt":"2026-05-01T00:00:00Z"}]}
JSON
    exit 0
    ;;
  "issue view 101 --json body")
    cat <<'JSON'
{"body":"Intro\n- [ ] Step A\n- [x] Step B\n"}
JSON
    exit 0
    ;;
  issue\ edit\ 101\ --body\ *)
    exit 0
    ;;
esac
printf '%s\n' "unexpected: $*" >>"$log"
exit 1
`
	writeExecutableScript(t, tmp, "gh", script)
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	logFile := filepath.Join(tmp, "gh.log")
	t.Setenv("FAKE_GH_LOG", logFile)

	ctx := context.Background()
	issue, err := FetchIssue(ctx, "", 101)
	if err != nil {
		t.Fatalf("FetchIssue: %v", err)
	}
	if issue.Title != "Hermes #101" || len(issue.Checklist) != 2 {
		t.Fatalf("unexpected issue parsed: %+v", issue)
	}
	if issue.State != "OPEN" || len(issue.Comments) != 1 || issue.Comments[0].Author != "alice" {
		t.Fatalf("expected state/comment to be parsed: %+v", issue)
	}
	if !HasLabel(issue, "hermes-auto-close") {
		t.Fatalf("expected label to be parsed: %+v", issue.Labels)
	}

	if err := SyncChecklist(ctx, "", 101, []SubTask{{Description: "Step A", Status: SubTaskDone}}); err != nil {
		t.Fatalf("SyncChecklist: %v", err)
	}

	logBytes, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("ReadFile log: %v", err)
	}
	got := string(logBytes)
	if !strings.Contains(got, "issue view 101 --json title,body,state,labels,comments") {
		t.Fatalf("missing issue fetch invocation:\n%s", got)
	}
	if !strings.Contains(got, "issue view 101 --json body") {
		t.Fatalf("missing issue body refetch:\n%s", got)
	}
	if !strings.Contains(got, "issue edit 101 --body") {
		t.Fatalf("expected issue edit body update, got:\n%s", got)
	}
}

func TestSyncChecklist_PreservesManualBodyContent(t *testing.T) {
	tmp := t.TempDir()
	bodyPath := filepath.Join(tmp, "body.txt")
	script := `#!/bin/sh
set -eu
log="$FAKE_GH_LOG"
printf '%s\n' "$*" >>"$log"
case "$*" in
  "issue view 102 --json body")
    cat <<'JSON'
{"body":"Intro\n- [ ] Step A\nUser note: keep this block.\n## Appendix\nManual content stays.\n"}
JSON
    exit 0
    ;;
  issue\ edit\ 102\ --body\ *)
    printf '%s' "$5" >"$BODY_FILE"
    exit 0
    ;;
esac
printf '%s\n' "unexpected: $*" >>"$log"
exit 1
`
	writeExecutableScript(t, tmp, "gh", script)
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	logFile := filepath.Join(tmp, "gh.log")
	t.Setenv("FAKE_GH_LOG", logFile)
	t.Setenv("BODY_FILE", bodyPath)

	if err := SyncChecklist(context.Background(), "", 102, []SubTask{{Description: "Step A", Status: SubTaskDone}}); err != nil {
		t.Fatalf("SyncChecklist: %v", err)
	}

	body, err := os.ReadFile(bodyPath)
	if err != nil {
		t.Fatalf("ReadFile body: %v", err)
	}
	text := string(body)
	if !strings.Contains(text, "User note: keep this block.") {
		t.Fatalf("manual note was lost:\n%s", text)
	}
	if !strings.Contains(text, "Manual content stays.") {
		t.Fatalf("appendix content was lost:\n%s", text)
	}
	if !strings.Contains(text, "- [x] Step A") {
		t.Fatalf("checked checklist item missing:\n%s", text)
	}
}

func TestWritePlanToIssueReplacesExistingPlanSection(t *testing.T) {
	tmp := t.TempDir()
	bodyPath := filepath.Join(tmp, "body.txt")
	script := `#!/bin/sh
set -eu
log="$FAKE_GH_LOG"
printf '%s\n' "$*" >>"$log"
case "$*" in
  "issue view 7 --json body")
    cat <<'JSON'
{"body":"Intro\n## Hermes 執行計劃\n- [ ] Old step\n## Notes\nKeep this.\n"}
JSON
    exit 0
    ;;
  issue\ edit\ 7\ --body\ *)
    printf '%s' "$5" >"$BODY_FILE"
    exit 0
    ;;
esac
printf '%s\n' "unexpected: $*" >>"$log"
exit 1
`
	writeExecutableScript(t, tmp, "gh", script)
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	logFile := filepath.Join(tmp, "gh.log")
	t.Setenv("FAKE_GH_LOG", logFile)
	t.Setenv("BODY_FILE", bodyPath)

	err := WritePlanToIssue(context.Background(), "", 7, "", []SubTask{{Description: "New step", Status: SubTaskPending}})
	if err != nil {
		t.Fatalf("WritePlanToIssue: %v", err)
	}

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("ReadFile log: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "issue view 7 --json body") {
		t.Fatalf("missing issue body fetch:\n%s", got)
	}
	if !strings.Contains(got, "issue edit 7 --body") || !strings.Contains(got, "## Hermes 執行計劃") {
		t.Fatalf("unexpected issue edit payload:\n%s", got)
	}
	body, err := os.ReadFile(bodyPath)
	if err != nil {
		t.Fatalf("ReadFile body: %v", err)
	}
	text := string(body)
	if strings.Contains(text, "- [ ] New step") {
		t.Fatalf("Hermes plan should not create GitHub checklist items:\n%s", text)
	}
	if !strings.Contains(text, "1. New step") {
		t.Fatalf("Hermes plan should include numbered step:\n%s", text)
	}
}

func TestPostCommentEventCompleteUsesGh(t *testing.T) {
	tmp := t.TempDir()
	ghScript := `#!/bin/sh
set -eu
printf '%s\n' "$*" >>"$FAKE_GH_LOG"
exit 0
`
	writeExecutableScript(t, tmp, "gh", ghScript)
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	ghLog := filepath.Join(tmp, "gh.log")
	t.Setenv("FAKE_GH_LOG", ghLog)

	if err := PostCommentEvent(context.Background(), "", 3, "complete", TaskState{Plan: []SubTask{{Status: SubTaskDone}}, TokenBudget: TokenBudget{UsedTokens: 42}}); err != nil {
		t.Fatalf("PostCommentEvent: %v", err)
	}

	logBytes, err := os.ReadFile(ghLog)
	if err != nil {
		t.Fatalf("ReadFile log: %v", err)
	}
	if !strings.Contains(string(logBytes), "issue comment 3 --body") {
		t.Fatalf("unexpected gh invocation:\n%s", string(logBytes))
	}
	if !strings.Contains(string(logBytes), "Hermes 完成") || !strings.Contains(string(logBytes), "42") {
		t.Fatalf("comment body missing expected content:\n%s", string(logBytes))
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
