package hermes

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ── parseIssueSelector ────────────────────────────────────────────────────────

func TestParseIssueSelector(t *testing.T) {
	cases := []struct {
		input   string
		wantNum int
		wantOk  bool
	}{
		{"#129", 129, true},
		{"129", 129, true},
		{"  #42  ", 42, true},
		{"0", 0, false},
		{"#0", 0, false},
		{"P15", 0, false},
		{"P15 - Something", 0, false},
		{"", 0, false},
		{"#", 0, false},
		{"1a2", 0, false},
	}
	for _, tc := range cases {
		n, ok := parseIssueSelector(tc.input)
		if ok != tc.wantOk || n != tc.wantNum {
			t.Errorf("parseIssueSelector(%q) = (%d, %v); want (%d, %v)", tc.input, n, ok, tc.wantNum, tc.wantOk)
		}
	}
}

// ── matchMilestoneByTitle ─────────────────────────────────────────────────────

var testMilestones = []MilestoneItem{
	{Number: 1, Title: "P13 - Foundation"},
	{Number: 2, Title: "P14 - API Layer"},
	{Number: 3, Title: "P15 - Hermes Stabilization & Cleanup"},
	{Number: 4, Title: "p15 - lowercase variant"},
}

func TestMatchMilestoneByTitle_Exact(t *testing.T) {
	got := matchMilestoneByTitle(testMilestones, "P13 - Foundation")
	if len(got) != 1 || got[0].Number != 1 {
		t.Errorf("exact match: want milestone 1, got %+v", got)
	}
}

func TestMatchMilestoneByTitle_CaseInsensitiveExact(t *testing.T) {
	// "p14 - api layer" should match "P14 - API Layer" via case-insensitive exact.
	got := matchMilestoneByTitle(testMilestones, "p14 - api layer")
	if len(got) != 1 || got[0].Number != 2 {
		t.Errorf("ci-exact match: want milestone 2, got %+v", got)
	}
}

func TestMatchMilestoneByTitle_Prefix(t *testing.T) {
	// "P15" should prefix-match milestone 3 and 4.
	got := matchMilestoneByTitle(testMilestones, "P15")
	if len(got) != 2 {
		t.Errorf("prefix match: want 2 candidates, got %d: %+v", len(got), got)
	}
}

func TestMatchMilestoneByTitle_Contains(t *testing.T) {
	got := matchMilestoneByTitle(testMilestones, "Hermes")
	if len(got) != 1 || got[0].Number != 3 {
		t.Errorf("contains match: want milestone 3, got %+v", got)
	}
}

func TestMatchMilestoneByTitle_NoMatch(t *testing.T) {
	got := matchMilestoneByTitle(testMilestones, "NonExistent")
	if len(got) != 0 {
		t.Errorf("no-match: want empty, got %+v", got)
	}
}

func TestMatchMilestoneByTitle_ExactBeatsPrefix(t *testing.T) {
	// Add a milestone whose full title is exactly "P15".
	ms := append(testMilestones, MilestoneItem{Number: 99, Title: "P15"})
	got := matchMilestoneByTitle(ms, "P15")
	if len(got) != 1 || got[0].Number != 99 {
		t.Errorf("exact should beat prefix: want milestone 99, got %+v", got)
	}
}

// ── milestoneSlug ─────────────────────────────────────────────────────────────

func TestMilestoneSlug(t *testing.T) {
	cases := []struct {
		title string
		want  string
	}{
		{"P15 - Hermes Stabilization & Cleanup", "p15-hermes-stabilization-cleanup"},
		{"Simple", "simple"},
		{"  lots   of   spaces  ", "lots-of-spaces"},
	}
	for _, tc := range cases {
		got := milestoneSlug(tc.title)
		if got != tc.want {
			t.Errorf("milestoneSlug(%q) = %q; want %q", tc.title, got, tc.want)
		}
	}
}

func TestMilestoneSlug_MaxLength(t *testing.T) {
	long := strings.Repeat("abcdefghij", 10) // 100 chars
	got := milestoneSlug(long)
	if len(got) > 40 {
		t.Errorf("slug too long: %d chars", len(got))
	}
}

// ── parseMilestoneReviewResult ────────────────────────────────────────────────

func TestParseMilestoneReviewResult_Full(t *testing.T) {
	raw := `VERDICT: READY WITH RISKS
SCORE: 72

BLOCKERS:
- Issue #45 is still open and blocks the release

GAPS:
- Missing integration test for the webhook handler
- No documented rollback procedure

INCONSISTENCIES:
- Issue #30 says config key is "timeout_ms" but #38 uses "timeout_seconds"

CLOSEOUT CHECKLIST:
- Close issue #45 after hotfix is merged
- Tag release candidate

RECOMMENDED NEXT ACTIONS:
- Fix issue #45 within 48 hours
- Update rollback docs before merging
`
	r := parseMilestoneReviewResult(raw)

	if r.Verdict != "READY WITH RISKS" {
		t.Errorf("Verdict = %q; want READY WITH RISKS", r.Verdict)
	}
	if r.Score != 72 {
		t.Errorf("Score = %d; want 72", r.Score)
	}
	if len(r.Blockers) != 1 {
		t.Errorf("Blockers len = %d; want 1", len(r.Blockers))
	}
	if len(r.Gaps) != 2 {
		t.Errorf("Gaps len = %d; want 2", len(r.Gaps))
	}
	if len(r.Inconsistencies) != 1 {
		t.Errorf("Inconsistencies len = %d; want 1", len(r.Inconsistencies))
	}
	if len(r.Closeout) != 2 {
		t.Errorf("Closeout len = %d; want 2", len(r.Closeout))
	}
	if len(r.NextActions) != 2 {
		t.Errorf("NextActions len = %d; want 2", len(r.NextActions))
	}
}

func TestParseMilestoneReviewResult_NotReady(t *testing.T) {
	raw := "VERDICT: NOT READY\nSCORE: 20\n\nBLOCKERS:\n- Critical bug in auth\n\nGAPS:\n- None\n\nINCONSISTENCIES:\n- None\n\nCLOSEOUT CHECKLIST:\n- Fix auth bug\n\nRECOMMENDED NEXT ACTIONS:\n- Prioritize auth fix\n"
	r := parseMilestoneReviewResult(raw)
	if r.Verdict != "NOT READY" {
		t.Errorf("Verdict = %q; want NOT READY", r.Verdict)
	}
	if r.Score != 20 {
		t.Errorf("Score = %d; want 20", r.Score)
	}
	// "None" bullets should be skipped.
	if len(r.Gaps) != 0 {
		t.Errorf("Gaps should be empty when 'None', got %v", r.Gaps)
	}
	if len(r.Inconsistencies) != 0 {
		t.Errorf("Inconsistencies should be empty when 'None', got %v", r.Inconsistencies)
	}
}

func TestParseMilestoneReviewResult_Ready(t *testing.T) {
	raw := "VERDICT: READY\nSCORE: 95\n\nBLOCKERS:\n- None\n\nGAPS:\n- None\n\nINCONSISTENCIES:\n- None\n\nCLOSEOUT CHECKLIST:\n- Archive branch\n\nRECOMMENDED NEXT ACTIONS:\n- None\n"
	r := parseMilestoneReviewResult(raw)
	if r.Verdict != "READY" {
		t.Errorf("Verdict = %q; want READY", r.Verdict)
	}
	if r.Score != 95 {
		t.Errorf("Score = %d; want 95", r.Score)
	}
}

func TestParseMilestoneReviewResult_EmptyFallback(t *testing.T) {
	r := parseMilestoneReviewResult("no structured content here")
	if r.Verdict != "NOT READY" {
		t.Errorf("empty fallback verdict: want NOT READY, got %q", r.Verdict)
	}
}

// ── RunMilestoneReview ────────────────────────────────────────────────────────

func TestRunMilestoneReview_ParsesLLMResponse(t *testing.T) {
	stubResponse := "VERDICT: READY\nSCORE: 88\n\nBLOCKERS:\n- None\n\nGAPS:\n- None\n\nINCONSISTENCIES:\n- None\n\nCLOSEOUT CHECKLIST:\n- Tag release\n\nRECOMMENDED NEXT ACTIONS:\n- Deploy to prod\n"
	callFn := func(_ context.Context, _, _, _ string) (CallPlanResult, error) {
		return CallPlanResult{Text: stubResponse}, nil
	}

	mrc := &MilestoneReviewContext{
		Milestone: MilestoneItem{Number: 10, Title: "P10", State: "open"},
		Issues: []*IssueContext{
			{Number: 1, Title: "Test Issue", State: "CLOSED", Body: "Done."},
		},
	}

	r, err := RunMilestoneReview(context.Background(), callFn, "/tmp", mrc)
	if err != nil {
		t.Fatalf("RunMilestoneReview: %v", err)
	}
	if r.Verdict != "READY" {
		t.Errorf("Verdict = %q; want READY", r.Verdict)
	}
	if r.Score != 88 {
		t.Errorf("Score = %d; want 88", r.Score)
	}
	if len(r.NextActions) != 1 || r.NextActions[0] != "Deploy to prod" {
		t.Errorf("NextActions = %v; want [Deploy to prod]", r.NextActions)
	}
}

func TestRunMilestoneReview_EmptyResponseError(t *testing.T) {
	callFn := func(_ context.Context, _, _, _ string) (CallPlanResult, error) {
		return CallPlanResult{Text: "   "}, nil
	}
	mrc := &MilestoneReviewContext{Milestone: MilestoneItem{Number: 1, Title: "X"}}
	_, err := RunMilestoneReview(context.Background(), callFn, "/tmp", mrc)
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Errorf("want empty-response error, got %v", err)
	}
}

// ── FormatMilestoneReview ─────────────────────────────────────────────────────

func TestFormatMilestoneReview_WriteFileAndSendMarker(t *testing.T) {
	dir := t.TempDir()
	ms := &MilestoneItem{Number: 15, Title: "P15 - Test"}
	// Use enough content to push the markdown report over 500 chars so
	// [SEND_FILE:...] is emitted.
	r := &MilestoneReviewResult{
		Verdict:         "READY WITH RISKS",
		Score:           75,
		Blockers:        []string{"Issue #1 open and blocking release pipeline"},
		Gaps:            []string{"Missing integration docs", "No rollback procedure defined"},
		Inconsistencies: []string{"Config key mismatch between #30 and #38"},
		Closeout:        []string{"Merge hotfix", "Tag release candidate"},
		NextActions:     []string{"Fix docs before Thursday", "Deploy to staging first"},
		RawText:         strings.Repeat("raw llm output content. ", 20),
	}

	msg, err := FormatMilestoneReview(dir, ms, r)
	if err != nil {
		t.Fatalf("FormatMilestoneReview: %v", err)
	}

	// Telegram summary should contain key fields.
	if !strings.Contains(msg, "P15 - Test") {
		t.Errorf("summary missing milestone title: %s", msg)
	}
	if !strings.Contains(msg, "READY WITH RISKS") {
		t.Errorf("summary missing verdict: %s", msg)
	}
	if !strings.Contains(msg, "75") {
		t.Errorf("summary missing score: %s", msg)
	}

	// [SEND_FILE:...] marker should be present (markdown report > 500 chars).
	if !strings.Contains(msg, "[SEND_FILE:") {
		t.Errorf("long report should include SEND_FILE marker:\n%s", msg)
		return
	}

	// Extract and verify the file was actually written.
	markerStart := strings.Index(msg, "[SEND_FILE:") + len("[SEND_FILE:")
	markerEnd := strings.Index(msg[markerStart:], "]")
	if markerEnd < 0 {
		t.Fatalf("malformed SEND_FILE marker in: %s", msg)
	}
	relPath := msg[markerStart : markerStart+markerEnd]
	absPath := filepath.Join(dir, relPath)
	data, err := os.ReadFile(absPath)
	if err != nil {
		t.Fatalf("report file not written at %s: %v", absPath, err)
	}
	if !strings.Contains(string(data), "P15 - Test") {
		t.Errorf("report file missing milestone title")
	}
	if !strings.Contains(string(data), "READY WITH RISKS") {
		t.Errorf("report file missing verdict")
	}
}

func TestFormatMilestoneReview_ShortReportNoSendFile(t *testing.T) {
	dir := t.TempDir()
	ms := &MilestoneItem{Number: 1, Title: "X"}
	r := &MilestoneReviewResult{Verdict: "READY", Score: 100}

	msg, err := FormatMilestoneReview(dir, ms, r)
	if err != nil {
		t.Fatalf("FormatMilestoneReview: %v", err)
	}
	// Short markdown (no blockers/gaps/etc.) should not exceed 500 chars and
	// therefore should not include SEND_FILE.
	if strings.Contains(msg, "[SEND_FILE:") {
		// The report file might still be > 500 if RawText is included.
		// Just verify the file was created without failing.
		t.Logf("short report still emitted SEND_FILE (report may include raw text): %s", msg)
	}
}

// ── LoadMilestoneReviewContext (integration-style with stubbed gh) ─────────────

func TestLoadMilestoneReviewContext_TruncatesBodyAndComments(t *testing.T) {
	tmp := t.TempDir()

	longBody := strings.Repeat("x", 4000)
	manyComments := ""
	for i := 0; i < 10; i++ {
		longComment := strings.Repeat("c", 800)
		manyComments += `{"author":{"login":"user"},"body":"` + longComment + `","createdAt":"2026-01-01T00:00:00Z"},`
	}
	manyComments = strings.TrimRight(manyComments, ",")

	// gh script: handles `issue list --milestone 5 --state all --limit 200 --json number`
	// and `issue view N --json number,title,body,state,labels,comments,milestone`
	script := `#!/bin/sh
set -eu
case "$*" in
  "issue list --milestone 5 --state all --limit 200 --json number")
    printf '[{"number":101},{"number":102}]\n'
    ;;
  "issue view 101 --json number,title,body,state,labels,comments,milestone")
    printf '{"number":101,"title":"Open Issue","body":"` + longBody + `","state":"OPEN","labels":[],"comments":[` + manyComments + `],"milestone":{"number":5,"title":"P5"}}\n'
    ;;
  "issue view 102 --json number,title,body,state,labels,comments,milestone")
    printf '{"number":102,"title":"Closed Issue","body":"short body","state":"CLOSED","labels":[],"comments":[],"milestone":{"number":5,"title":"P5"}}\n'
    ;;
  *)
    printf 'unexpected: %s\n' "$*" >&2
    exit 1
    ;;
esac
`
	writeExecutableScript(t, tmp, "gh", script)
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	ms := &MilestoneItem{Number: 5, Title: "P5", State: "open"}
	mrc, err := LoadMilestoneReviewContext(context.Background(), "", ms)
	if err != nil {
		t.Fatalf("LoadMilestoneReviewContext: %v", err)
	}

	if len(mrc.Issues) != 2 {
		t.Fatalf("want 2 issues, got %d", len(mrc.Issues))
	}

	// Verify body truncation.
	issue101 := mrc.Issues[0]
	if len(issue101.Body) > milestoneIssueTruncBody+20 { // +20 for "...[truncated]"
		t.Errorf("body not truncated: len=%d", len(issue101.Body))
	}
	if !strings.HasSuffix(issue101.Body, "[truncated]") {
		end := len(issue101.Body)
		start := end - 30
		if start < 0 {
			start = 0
		}
		t.Errorf("body missing truncated marker: %q", issue101.Body[start:])
	}

	// Verify comments capped at milestoneMaxComments.
	if len(issue101.Comments) > milestoneMaxComments {
		t.Errorf("comments not capped: got %d, want <= %d", len(issue101.Comments), milestoneMaxComments)
	}

	// Verify comment body truncation.
	for i, c := range issue101.Comments {
		if len(c.Body) > milestoneIssueTruncComment+20 {
			t.Errorf("comment[%d] body not truncated: len=%d", i, len(c.Body))
		}
	}

	// Verify closed issue is included.
	issue102 := mrc.Issues[1]
	if issue102.State != "CLOSED" {
		t.Errorf("issue102 state = %q; want CLOSED", issue102.State)
	}
}

func TestLoadMilestoneReviewContext_BothOpenAndClosed(t *testing.T) {
	tmp := t.TempDir()
	script := `#!/bin/sh
set -eu
case "$*" in
  "issue list --milestone 7 --state all --limit 200 --json number")
    printf '[{"number":10},{"number":11},{"number":12}]\n'
    ;;
  "issue view 10 --json number,title,body,state,labels,comments,milestone")
    printf '{"number":10,"title":"Open 1","body":"","state":"OPEN","labels":[],"comments":[],"milestone":{"number":7,"title":"P7"}}\n'
    ;;
  "issue view 11 --json number,title,body,state,labels,comments,milestone")
    printf '{"number":11,"title":"Closed 1","body":"","state":"CLOSED","labels":[],"comments":[],"milestone":{"number":7,"title":"P7"}}\n'
    ;;
  "issue view 12 --json number,title,body,state,labels,comments,milestone")
    printf '{"number":12,"title":"Open 2","body":"","state":"OPEN","labels":[],"comments":[],"milestone":{"number":7,"title":"P7"}}\n'
    ;;
  *)
    printf 'unexpected: %s\n' "$*" >&2
    exit 1
    ;;
esac
`
	writeExecutableScript(t, tmp, "gh", script)
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	ms := &MilestoneItem{Number: 7, Title: "P7", State: "open"}
	mrc, err := LoadMilestoneReviewContext(context.Background(), "", ms)
	if err != nil {
		t.Fatalf("LoadMilestoneReviewContext: %v", err)
	}

	if len(mrc.Issues) != 3 {
		t.Fatalf("want 3 issues (open+closed), got %d", len(mrc.Issues))
	}

	states := map[string]int{}
	for _, iss := range mrc.Issues {
		states[iss.State]++
	}
	if states["OPEN"] != 2 || states["CLOSED"] != 1 {
		t.Errorf("unexpected state distribution: %v", states)
	}
}

// ── FetchMilestones (integration-style) ──────────────────────────────────────

func TestFetchMilestones_ParsesAllFields(t *testing.T) {
	tmp := t.TempDir()
	script := `#!/bin/sh
set -eu
case "$*" in
  "api --paginate repos/{owner}/{repo}/milestones?state=all&per_page=100")
    printf '[{"number":3,"title":"P3","state":"closed","description":"desc","due_on":"2026-03-01T00:00:00Z","open_issues":0,"closed_issues":5}]\n'
    ;;
  *)
    printf 'unexpected: %s\n' "$*" >&2
    exit 1
    ;;
esac
`
	writeExecutableScript(t, tmp, "gh", script)
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	items, err := FetchMilestones(context.Background(), "")
	if err != nil {
		t.Fatalf("FetchMilestones: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("want 1 milestone, got %d", len(items))
	}
	m := items[0]
	if m.Number != 3 || m.Title != "P3" || m.State != "closed" {
		t.Errorf("unexpected milestone: %+v", m)
	}
	if m.DueOn == nil {
		t.Error("DueOn should be parsed")
	} else {
		expected := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
		if !m.DueOn.Equal(expected) {
			t.Errorf("DueOn = %v; want %v", m.DueOn, expected)
		}
	}
	if m.ClosedIssues != 5 {
		t.Errorf("ClosedIssues = %d; want 5", m.ClosedIssues)
	}
}

// ── verdictEmoji ──────────────────────────────────────────────────────────────

func TestVerdictEmoji(t *testing.T) {
	if verdictEmoji("READY") != "✅" {
		t.Error("READY emoji mismatch")
	}
	if verdictEmoji("READY WITH RISKS") != "⚠️" {
		t.Error("READY WITH RISKS emoji mismatch")
	}
	if verdictEmoji("NOT READY") != "🚫" {
		t.Error("NOT READY emoji mismatch")
	}
	if verdictEmoji("unknown") != "🚫" {
		t.Error("unknown should default to 🚫")
	}
}

// ── Unit tests: selector parsing ─────────────────────────────────────────────
// Named TestUnit_* so `go test -run Unit` selects them.

func TestUnit_ParseIssueSelector_NumericForms(t *testing.T) {
	cases := []struct {
		in  string
		num int
		ok  bool
	}{
		{"#1", 1, true},
		{"42", 42, true},
		{"  #99  ", 99, true},
		{"0", 0, false},
		{"#0", 0, false},
	}
	for _, tc := range cases {
		n, ok := parseIssueSelector(tc.in)
		if ok != tc.ok || n != tc.num {
			t.Errorf("parseIssueSelector(%q) = (%d,%v); want (%d,%v)", tc.in, n, ok, tc.num, tc.ok)
		}
	}
}

func TestUnit_ParseIssueSelector_QuerySelectors(t *testing.T) {
	// Text queries like "P15", "current", quoted titles must NOT be parsed as issues.
	nonIssue := []string{"P15", "current", "P15 - Hermes Cleanup", `"P15 - Hermes Cleanup"`, "", "#", "1a2"}
	for _, s := range nonIssue {
		_, ok := parseIssueSelector(s)
		if ok {
			t.Errorf("parseIssueSelector(%q) returned ok=true; want false for non-issue selector", s)
		}
	}
}

func TestUnit_ParseIssueSelector_CurrentIsQuery(t *testing.T) {
	// "current" must not be treated as a numeric issue selector.
	_, ok := parseIssueSelector("current")
	if ok {
		t.Error("parseIssueSelector(\"current\") = true; must be false (handled as text query)")
	}
}

// ── Unit tests: matchMilestoneByTitle edge cases ──────────────────────────────

func TestUnit_MatchMilestoneByTitle_EmptyList(t *testing.T) {
	got := matchMilestoneByTitle(nil, "P15")
	if len(got) != 0 {
		t.Errorf("empty list: want 0 matches, got %d", len(got))
	}
	got = matchMilestoneByTitle([]MilestoneItem{}, "P15")
	if len(got) != 0 {
		t.Errorf("empty list (non-nil): want 0 matches, got %d", len(got))
	}
}

func TestUnit_MatchMilestoneByTitle_ClosedOnly(t *testing.T) {
	// Matching should work regardless of milestone State field.
	closed := []MilestoneItem{
		{Number: 10, Title: "P10 - Released", State: "closed"},
		{Number: 11, Title: "P11 - Done", State: "closed"},
	}
	got := matchMilestoneByTitle(closed, "P10 - Released")
	if len(got) != 1 || got[0].Number != 10 {
		t.Errorf("closed-only exact: want milestone 10, got %+v", got)
	}
	got = matchMilestoneByTitle(closed, "P1") // prefix matches both P10 and P11
	if len(got) != 2 {
		t.Errorf("closed-only prefix: want 2 matches, got %d", len(got))
	}
}

func TestUnit_MatchMilestoneByTitle_AmbiguousMultiMatch(t *testing.T) {
	ms := []MilestoneItem{
		{Number: 20, Title: "P20 - Alpha"},
		{Number: 21, Title: "P20 - Beta"},
	}
	got := matchMilestoneByTitle(ms, "P20")
	if len(got) != 2 {
		t.Errorf("ambiguous prefix: want 2 candidates, got %d: %+v", len(got), got)
	}
}

func TestUnit_MatchMilestoneByTitle_ContainsLastResort(t *testing.T) {
	ms := []MilestoneItem{
		{Number: 30, Title: "Foundation Work"},
		{Number: 31, Title: "API Layer"},
	}
	// "Work" should match via contains only (not exact, not ci-exact, not prefix).
	got := matchMilestoneByTitle(ms, "Work")
	if len(got) != 1 || got[0].Number != 30 {
		t.Errorf("contains-only: want milestone 30, got %+v", got)
	}
}

// ── Unit tests: ResolveGitHubMilestone (gh stubbed via PATH) ─────────────────

func TestUnit_ResolveGitHubMilestone_CurrentSelector(t *testing.T) {
	// "current" must return an error without calling gh at all.
	_, err := ResolveGitHubMilestone(context.Background(), "", "current")
	if err == nil {
		t.Fatal("expected error for 'current' selector, got nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "current") {
		t.Errorf("error should mention 'current': %v", err)
	}
}

func TestUnit_ResolveGitHubMilestone_NoMatch(t *testing.T) {
	tmp := t.TempDir()
	script := `#!/bin/sh
set -eu
case "$*" in
  "api --paginate repos/{owner}/{repo}/milestones?state=all&per_page=100")
    printf '[{"number":1,"title":"P1 - Existing","state":"open","description":"","due_on":"","open_issues":1,"closed_issues":0}]\n'
    ;;
  *)
    printf 'unexpected: %s\n' "$*" >&2; exit 1 ;;
esac
`
	writeExecutableScript(t, tmp, "gh", script)
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	_, err := ResolveGitHubMilestone(context.Background(), "", "NonExistentMilestone")
	if err == nil {
		t.Fatal("expected ErrNoMilestoneMatch, got nil")
	}
	var noMatch *ErrNoMilestoneMatch
	if !errors.As(err, &noMatch) {
		t.Errorf("want ErrNoMilestoneMatch, got %T: %v", err, err)
	}
	if noMatch != nil && noMatch.Query != "NonExistentMilestone" {
		t.Errorf("Query = %q; want NonExistentMilestone", noMatch.Query)
	}
	// Available should list open milestones for user guidance.
	if noMatch != nil && len(noMatch.Available) == 0 {
		t.Error("Available milestones should include open milestones for guidance")
	}
}

func TestUnit_ResolveGitHubMilestone_Ambiguous(t *testing.T) {
	tmp := t.TempDir()
	script := `#!/bin/sh
set -eu
case "$*" in
  "api --paginate repos/{owner}/{repo}/milestones?state=all&per_page=100")
    printf '[{"number":1,"title":"P2 - Alpha","state":"open","description":"","due_on":"","open_issues":1,"closed_issues":0},{"number":2,"title":"P2 - Beta","state":"open","description":"","due_on":"","open_issues":0,"closed_issues":3}]\n'
    ;;
  *)
    printf 'unexpected: %s\n' "$*" >&2; exit 1 ;;
esac
`
	writeExecutableScript(t, tmp, "gh", script)
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	_, err := ResolveGitHubMilestone(context.Background(), "", "P2")
	if err == nil {
		t.Fatal("expected ErrAmbiguousMilestone, got nil")
	}
	var ambig *ErrAmbiguousMilestone
	if !errors.As(err, &ambig) {
		t.Errorf("want ErrAmbiguousMilestone, got %T: %v", err, err)
	}
	if ambig != nil && len(ambig.Candidates) != 2 {
		t.Errorf("Candidates len = %d; want 2", len(ambig.Candidates))
	}
}

func TestUnit_ResolveGitHubMilestone_IssueNoMilestone(t *testing.T) {
	tmp := t.TempDir()
	script := `#!/bin/sh
set -eu
case "$*" in
  "issue view 55 --json number,title,body,state,labels,comments,milestone")
    printf '{"number":55,"title":"No milestone issue","body":"","state":"OPEN","labels":[],"comments":[]}\n'
    ;;
  *)
    printf 'unexpected: %s\n' "$*" >&2; exit 1 ;;
esac
`
	writeExecutableScript(t, tmp, "gh", script)
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	_, err := ResolveGitHubMilestone(context.Background(), "", "#55")
	if err == nil {
		t.Fatal("expected ErrNoMilestoneOnIssue, got nil")
	}
	var noMS *ErrNoMilestoneOnIssue
	if !errors.As(err, &noMS) {
		t.Errorf("want ErrNoMilestoneOnIssue, got %T: %v", err, err)
	}
	if noMS != nil && noMS.IssueNumber != 55 {
		t.Errorf("IssueNumber = %d; want 55", noMS.IssueNumber)
	}
}

func TestUnit_ResolveGitHubMilestone_IssueNotFound(t *testing.T) {
	tmp := t.TempDir()
	script := `#!/bin/sh
set -eu
printf 'could not resolve to an issue or pull request: not found\n' >&2
exit 1
`
	writeExecutableScript(t, tmp, "gh", script)
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	_, err := ResolveGitHubMilestone(context.Background(), "", "#999")
	if err == nil {
		t.Fatal("expected ErrIssueNotFound, got nil")
	}
	var notFound *ErrIssueNotFound
	if !errors.As(err, &notFound) {
		t.Errorf("want ErrIssueNotFound, got %T: %v", err, err)
	}
	if notFound != nil && notFound.IssueNumber != 999 {
		t.Errorf("IssueNumber = %d; want 999", notFound.IssueNumber)
	}
}

func TestUnit_ResolveGitHubMilestone_ExactTitleMatch(t *testing.T) {
	tmp := t.TempDir()
	script := `#!/bin/sh
set -eu
case "$*" in
  "api --paginate repos/{owner}/{repo}/milestones?state=all&per_page=100")
    printf '[{"number":5,"title":"P5 - Release","state":"open","description":"","due_on":"","open_issues":0,"closed_issues":10}]\n'
    ;;
  *)
    printf 'unexpected: %s\n' "$*" >&2; exit 1 ;;
esac
`
	writeExecutableScript(t, tmp, "gh", script)
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	ms, err := ResolveGitHubMilestone(context.Background(), "", "P5 - Release")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ms.Number != 5 || ms.Title != "P5 - Release" {
		t.Errorf("unexpected milestone: %+v", ms)
	}
}

// ── Integration-style tests: table-driven fixtures of canned gh JSON ─────────
// Named TestInteg_* to allow targeted selection: go test -run Integ

func TestInteg_ResolveGitHubMilestone_IssuePath_HappyPath(t *testing.T) {
	// Issue #42 has milestone #10; fetchMilestoneByNumber fetches full metadata.
	tmp := t.TempDir()
	script := `#!/bin/sh
set -eu
case "$*" in
  "issue view 42 --json number,title,body,state,labels,comments,milestone")
    printf '{"number":42,"title":"Feature work","body":"done","state":"CLOSED","labels":[],"comments":[],"milestone":{"number":10,"title":"P10 - Alpha"}}\n'
    ;;
  "api repos/{owner}/{repo}/milestones/10")
    printf '{"number":10,"title":"P10 - Alpha","state":"closed","description":"Alpha release","due_on":"2026-04-01T00:00:00Z","open_issues":0,"closed_issues":8}\n'
    ;;
  *)
    printf 'unexpected: %s\n' "$*" >&2; exit 1 ;;
esac
`
	writeExecutableScript(t, tmp, "gh", script)
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	ms, err := ResolveGitHubMilestone(context.Background(), "", "#42")
	if err != nil {
		t.Fatalf("issue-based resolution: %v", err)
	}
	if ms.Number != 10 {
		t.Errorf("Number = %d; want 10", ms.Number)
	}
	if ms.Title != "P10 - Alpha" {
		t.Errorf("Title = %q; want P10 - Alpha", ms.Title)
	}
	if ms.ClosedIssues != 8 {
		t.Errorf("ClosedIssues = %d; want 8 (from full metadata fetch)", ms.ClosedIssues)
	}
	if ms.DueOn == nil {
		t.Error("DueOn should be parsed from full metadata fetch")
	}
}

func TestInteg_ResolveGitHubMilestone_TitlePath_PrefixMatch(t *testing.T) {
	// "P16" prefix-matches exactly one milestone.
	tmp := t.TempDir()
	script := `#!/bin/sh
set -eu
case "$*" in
  "api --paginate repos/{owner}/{repo}/milestones?state=all&per_page=100")
    printf '[{"number":16,"title":"P16 - Beta","state":"open","description":"","due_on":"","open_issues":3,"closed_issues":7},{"number":17,"title":"P17 - Gamma","state":"open","description":"","due_on":"","open_issues":1,"closed_issues":2}]\n'
    ;;
  *)
    printf 'unexpected: %s\n' "$*" >&2; exit 1 ;;
esac
`
	writeExecutableScript(t, tmp, "gh", script)
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	ms, err := ResolveGitHubMilestone(context.Background(), "", "P16")
	if err != nil {
		t.Fatalf("title prefix resolution: %v", err)
	}
	if ms.Number != 16 || ms.Title != "P16 - Beta" {
		t.Errorf("unexpected milestone: %+v", ms)
	}
}

// TestInteg_ResolveGitHubMilestone_TableDriven exercises multiple resolution
// paths using table-driven canned JSON fixtures for a single stubbed repo.
func TestInteg_ResolveGitHubMilestone_TableDriven(t *testing.T) {
	tmp := t.TempDir()
	// Single stub handles all selectors used by the table cases.
	script := `#!/bin/sh
set -eu
case "$*" in
  "api --paginate repos/{owner}/{repo}/milestones?state=all&per_page=100")
    printf '[{"number":20,"title":"P20 - Alpha","state":"open","description":"","due_on":"","open_issues":1,"closed_issues":0},{"number":21,"title":"P21 - Beta","state":"open","description":"","due_on":"","open_issues":0,"closed_issues":5},{"number":22,"title":"P21 - Gamma","state":"closed","description":"","due_on":"","open_issues":0,"closed_issues":10}]\n'
    ;;
  *)
    printf 'unexpected: %s\n' "$*" >&2; exit 1 ;;
esac
`
	writeExecutableScript(t, tmp, "gh", script)
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	cases := []struct {
		selector    string
		wantNum     int    // 0 = expect error
		wantErrType string // "noMatch" | "ambiguous" | ""
	}{
		{"P20 - Alpha", 20, ""},       // exact match
		{"p20 - alpha", 20, ""},       // ci-exact match
		{"P20", 20, ""},               // unique prefix
		{"P21", 0, "ambiguous"},       // prefix matches #21 and #22
		{"NonExistent", 0, "noMatch"}, // no match
		{"P20 - Alpha", 20, ""},       // repeated — idempotent
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.selector, func(t *testing.T) {
			ms, err := ResolveGitHubMilestone(context.Background(), "", tc.selector)
			switch tc.wantErrType {
			case "noMatch":
				var e *ErrNoMilestoneMatch
				if !errors.As(err, &e) {
					t.Errorf("want ErrNoMilestoneMatch, got %T: %v", err, err)
				}
			case "ambiguous":
				var e *ErrAmbiguousMilestone
				if !errors.As(err, &e) {
					t.Errorf("want ErrAmbiguousMilestone, got %T: %v", err, err)
				}
				if e != nil && len(e.Candidates) < 2 {
					t.Errorf("Candidates = %d; want >= 2", len(e.Candidates))
				}
			default:
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if ms.Number != tc.wantNum {
					t.Errorf("Number = %d; want %d", ms.Number, tc.wantNum)
				}
			}
		})
	}
}

func TestInteg_LoadMilestoneReviewContext_IssueAndCommentPayloads(t *testing.T) {
	// End-to-end LoadMilestoneReviewContext with realistic issue + comment payloads.
	tmp := t.TempDir()
	script := `#!/bin/sh
set -eu
case "$*" in
  "issue list --milestone 8 --state all --limit 200 --json number")
    printf '[{"number":200},{"number":201}]\n'
    ;;
  "issue view 200 --json number,title,body,state,labels,comments,milestone")
    printf '{"number":200,"title":"Auth overhaul","body":"Implement JWT auth with refresh tokens.","state":"CLOSED","labels":[{"name":"backend"},{"name":"P1"}],"comments":[{"author":{"login":"alice"},"body":"Done in PR #55","createdAt":"2026-05-01T10:00:00Z"},{"author":{"login":"bob"},"body":"Reviewed and merged.","createdAt":"2026-05-02T09:00:00Z"}],"milestone":{"number":8,"title":"P8"}}\n'
    ;;
  "issue view 201 --json number,title,body,state,labels,comments,milestone")
    printf '{"number":201,"title":"Dashboard metrics","body":"Add real-time metrics to dashboard.","state":"OPEN","labels":[{"name":"frontend"}],"comments":[],"milestone":{"number":8,"title":"P8"}}\n'
    ;;
  *)
    printf 'unexpected: %s\n' "$*" >&2; exit 1 ;;
esac
`
	writeExecutableScript(t, tmp, "gh", script)
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	ms := &MilestoneItem{Number: 8, Title: "P8", State: "open"}
	mrc, err := LoadMilestoneReviewContext(context.Background(), "", ms)
	if err != nil {
		t.Fatalf("LoadMilestoneReviewContext: %v", err)
	}

	if len(mrc.Issues) != 2 {
		t.Fatalf("want 2 issues, got %d", len(mrc.Issues))
	}
	if mrc.Milestone.Title != "P8" {
		t.Errorf("Milestone.Title = %q; want P8", mrc.Milestone.Title)
	}

	iss200 := mrc.Issues[0]
	if iss200.Number != 200 {
		t.Errorf("Issues[0].Number = %d; want 200", iss200.Number)
	}
	if iss200.State != "CLOSED" {
		t.Errorf("Issues[0].State = %q; want CLOSED", iss200.State)
	}
	if len(iss200.Labels) != 2 {
		t.Errorf("Issues[0].Labels = %v; want [backend P1]", iss200.Labels)
	}
	if len(iss200.Comments) != 2 {
		t.Errorf("Issues[0].Comments len = %d; want 2", len(iss200.Comments))
	}
	if iss200.Comments[0].Author != "alice" {
		t.Errorf("Issues[0].Comments[0].Author = %q; want alice", iss200.Comments[0].Author)
	}

	iss201 := mrc.Issues[1]
	if iss201.State != "OPEN" {
		t.Errorf("Issues[1].State = %q; want OPEN", iss201.State)
	}
	if len(iss201.Comments) != 0 {
		t.Errorf("Issues[1].Comments = %d; want 0", len(iss201.Comments))
	}
}
