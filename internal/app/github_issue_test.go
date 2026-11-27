package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ── parseIssueSelector ────────────────────────────────────────────────────────

// ── matchMilestoneByTitle ─────────────────────────────────────────────────────

// ── milestoneSlug ─────────────────────────────────────────────────────────────

// ── parseMilestoneReviewResult ────────────────────────────────────────────────

// ── RunMilestoneReview ────────────────────────────────────────────────────────

// ── FormatMilestoneReview ─────────────────────────────────────────────────────

// ── LoadMilestoneReviewContext (integration-style with stubbed gh) ─────────────

// ── FetchMilestones (integration-style) ──────────────────────────────────────

// ── verdictEmoji ──────────────────────────────────────────────────────────────

// ── Unit tests: selector parsing ─────────────────────────────────────────────
// Named TestUnit_* so `go test -run Unit` selects them.

// ── Unit tests: matchMilestoneByTitle edge cases ──────────────────────────────

// ── Unit tests: ResolveGitHubMilestone (gh stubbed via PATH) ─────────────────

// ── Integration-style tests: table-driven fixtures of canned gh JSON ─────────
// Named TestInteg_* to allow targeted selection: go test -run Integ

func writeExecutableScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write script %s: %v", name, err)
	}
	return path
}

func TestFetchIssueForDispatch_ParsesAndRetriesOnce(t *testing.T) {
	tmp := t.TempDir()
	counter := filepath.Join(tmp, "calls")
	script := `#!/bin/sh
set -eu
n=$(cat "` + counter + `" 2>/dev/null || echo 0)
n=$((n+1))
echo "$n" > "` + counter + `"
if [ "$*" != "issue view 42 --json title,body,state,labels,comments" ]; then
  printf 'unexpected: %s\n' "$*" >&2
  exit 1
fi
if [ "$n" -eq 1 ]; then
  printf 'connection reset\n' >&2
  exit 1
fi
printf '{"title":"T","body":"intro\\n- [ ] first\\n- [x] second","state":"OPEN","labels":[{"name":"bug"},{"name":"p1"}],"comments":[{"author":{"login":"alice"},"body":"hi","createdAt":"2026-01-02T03:04:05Z"}]}\n'
`
	writeExecutableScript(t, tmp, "gh", script)
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	prev := ghRetryBackoff
	ghRetryBackoff = time.Millisecond
	defer func() { ghRetryBackoff = prev }()

	issue, err := FetchIssueForDispatch(context.Background(), tmp, 42)
	if err != nil {
		t.Fatalf("FetchIssueForDispatch: %v", err)
	}
	if issue.Number != 42 || issue.Title != "T" || issue.State != "OPEN" {
		t.Errorf("unexpected issue header: %+v", issue)
	}
	if len(issue.Labels) != 2 || issue.Labels[0] != "bug" || issue.Labels[1] != "p1" {
		t.Errorf("labels = %v", issue.Labels)
	}
	if len(issue.Checklist) != 2 || issue.Checklist[0].Checked || !issue.Checklist[1].Checked {
		t.Errorf("checklist = %+v", issue.Checklist)
	}
	if len(issue.Comments) != 1 || issue.Comments[0].Author != "alice" || issue.Comments[0].Body != "hi" {
		t.Errorf("comments = %+v", issue.Comments)
	}
	if got, _ := os.ReadFile(counter); strings.TrimSpace(string(got)) != "2" {
		t.Errorf("gh calls = %q, want 2 (one retry)", got)
	}
}

func TestFetchIssueForDispatch_GivesUpAfterOneRetry(t *testing.T) {
	tmp := t.TempDir()
	counter := filepath.Join(tmp, "calls")
	script := `#!/bin/sh
n=$(cat "` + counter + `" 2>/dev/null || echo 0)
echo $((n+1)) > "` + counter + `"
printf 'boom\n' >&2
exit 1
`
	writeExecutableScript(t, tmp, "gh", script)
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	prev := ghRetryBackoff
	ghRetryBackoff = time.Millisecond
	defer func() { ghRetryBackoff = prev }()

	if _, err := FetchIssueForDispatch(context.Background(), tmp, 7); err == nil {
		t.Fatal("expected error")
	}
	if got, _ := os.ReadFile(counter); strings.TrimSpace(string(got)) != "2" {
		t.Errorf("gh calls = %q, want 2", got)
	}
}

func TestGhTimeoutDispatchKeepsPR225Value(t *testing.T) {
	if ghTimeoutDispatch != 25*time.Second {
		t.Errorf("ghTimeoutDispatch = %v, want 25s", ghTimeoutDispatch)
	}
}
