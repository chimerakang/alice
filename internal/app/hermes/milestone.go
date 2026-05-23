package hermes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// MilestoneItem holds GitHub milestone metadata.
type MilestoneItem struct {
	Number       int
	Title        string
	State        string // "open" | "closed"
	Description  string
	DueOn        *time.Time
	OpenIssues   int
	ClosedIssues int
}

// Typed sentinel errors for milestone resolution.

// ErrNoMilestoneOnIssue is returned when an issue exists but has no milestone.
type ErrNoMilestoneOnIssue struct{ IssueNumber int }

func (e *ErrNoMilestoneOnIssue) Error() string {
	return fmt.Sprintf("issue #%d has no linked milestone", e.IssueNumber)
}

// ErrIssueNotFound is returned when the gh CLI cannot find the issue.
type ErrIssueNotFound struct{ IssueNumber int }

func (e *ErrIssueNotFound) Error() string {
	return fmt.Sprintf("issue #%d not found", e.IssueNumber)
}

// ErrNoMilestoneMatch is returned when no milestone title matches the query.
type ErrNoMilestoneMatch struct {
	Query     string
	Available []MilestoneItem // open milestones for user guidance
}

func (e *ErrNoMilestoneMatch) Error() string {
	return fmt.Sprintf("no milestone matches %q", e.Query)
}

// ErrAmbiguousMilestone is returned when multiple milestones match the query.
type ErrAmbiguousMilestone struct {
	Query      string
	Candidates []MilestoneItem
}

func (e *ErrAmbiguousMilestone) Error() string {
	return fmt.Sprintf("ambiguous milestone query %q: %d candidates", e.Query, len(e.Candidates))
}

// ghMilestoneJSON is the JSON shape from `gh api .../milestones`.
type ghMilestoneJSON struct {
	Number       int    `json:"number"`
	Title        string `json:"title"`
	State        string `json:"state"`
	Description  string `json:"description"`
	DueOn        string `json:"due_on"` // RFC3339 or ""
	OpenIssues   int    `json:"open_issues"`
	ClosedIssues int    `json:"closed_issues"`
}

// ghIssueWithMilestoneJSON is `gh issue view --json number,title,milestone,...`.
type ghIssueWithMilestoneJSON struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	State  string `json:"state"`
	Body   string `json:"body"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
	Milestone *struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
	} `json:"milestone"`
	Comments []struct {
		Author struct {
			Login string `json:"login"`
		} `json:"author"`
		Body      string    `json:"body"`
		CreatedAt time.Time `json:"createdAt"`
	} `json:"comments"`
}

// FetchMilestones returns all milestones (open + closed) for the repo rooted
// at projectDir by calling `gh api repos/:owner/:repo/milestones?state=all`.
func FetchMilestones(ctx context.Context, projectDir string) ([]MilestoneItem, error) {
	out, err := ghOutput(ctx, projectDir, ghTimeoutLong,
		"api", "--paginate",
		"repos/{owner}/{repo}/milestones?state=all&per_page=100",
	)
	if err != nil {
		return nil, fmt.Errorf("gh api milestones: %w", err)
	}
	var raw []ghMilestoneJSON
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parse milestones JSON: %w", err)
	}
	items := make([]MilestoneItem, 0, len(raw))
	for _, r := range raw {
		m := MilestoneItem{
			Number:       r.Number,
			Title:        r.Title,
			State:        r.State,
			Description:  r.Description,
			OpenIssues:   r.OpenIssues,
			ClosedIssues: r.ClosedIssues,
		}
		if r.DueOn != "" {
			if t, err := time.Parse(time.RFC3339, r.DueOn); err == nil {
				m.DueOn = &t
			}
		}
		items = append(items, m)
	}
	return items, nil
}

// FetchIssueWithMilestone fetches a single issue including its milestone field.
// Returns ErrIssueNotFound if gh reports the issue does not exist.
func FetchIssueWithMilestone(ctx context.Context, projectDir string, issueNum int) (*IssueContext, *MilestoneItem, error) {
	out, err := ghOutput(ctx, projectDir, ghTimeoutShort,
		"issue", "view", fmt.Sprintf("%d", issueNum),
		"--json", "number,title,body,state,labels,comments,milestone",
	)
	if err != nil {
		// Check both err.Error() and ExitError.Stderr so that real gh CLI
		// error messages (written to stderr) trigger the typed sentinel.
		msg := strings.ToLower(err.Error())
		if exitErr, ok := err.(*exec.ExitError); ok && len(exitErr.Stderr) > 0 {
			msg += " " + strings.ToLower(string(exitErr.Stderr))
		}
		if strings.Contains(msg, "not found") || strings.Contains(msg, "no such") || strings.Contains(msg, "could not resolve") {
			return nil, nil, &ErrIssueNotFound{IssueNumber: issueNum}
		}
		return nil, nil, fmt.Errorf("gh issue view %d: %w", issueNum, err)
	}

	var raw ghIssueWithMilestoneJSON
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, nil, fmt.Errorf("parse issue JSON: %w", err)
	}

	labels := make([]string, len(raw.Labels))
	for i, l := range raw.Labels {
		labels[i] = l.Name
	}
	comments := make([]IssueComment, 0, len(raw.Comments))
	for _, c := range raw.Comments {
		comments = append(comments, IssueComment{
			Author:    c.Author.Login,
			Body:      c.Body,
			CreatedAt: c.CreatedAt,
		})
	}
	issue := &IssueContext{
		Number:    raw.Number,
		Title:     raw.Title,
		Body:      raw.Body,
		State:     raw.State,
		Labels:    labels,
		Checklist: ExtractChecklist(raw.Body),
		Comments:  comments,
	}

	if raw.Milestone == nil {
		return issue, nil, nil
	}
	ms := &MilestoneItem{
		Number: raw.Milestone.Number,
		Title:  raw.Milestone.Title,
	}
	return issue, ms, nil
}

// FetchMilestoneIssues returns all issues (open + closed) belonging to the
// given milestone number with full bodies and recent comments.
func FetchMilestoneIssues(ctx context.Context, projectDir string, milestoneNumber int) ([]*IssueContext, error) {
	// Step 1: list issue numbers in milestone
	out, err := ghOutput(ctx, projectDir, ghTimeoutLong,
		"issue", "list",
		"--milestone", fmt.Sprintf("%d", milestoneNumber),
		"--state", "all",
		"--limit", "200",
		"--json", "number",
	)
	if err != nil {
		return nil, fmt.Errorf("gh issue list milestone=%d: %w", milestoneNumber, err)
	}
	var nums []struct {
		Number int `json:"number"`
	}
	if err := json.Unmarshal(out, &nums); err != nil {
		return nil, fmt.Errorf("parse issue list JSON: %w", err)
	}

	// Step 2: fetch each issue with full body (parallel would be nicer but
	// sequential is simpler and safe for typical milestone sizes).
	issues := make([]*IssueContext, 0, len(nums))
	for _, n := range nums {
		issue, _, err := FetchIssueWithMilestone(ctx, projectDir, n.Number)
		if err != nil {
			var notFound *ErrIssueNotFound
			if errors.As(err, &notFound) {
				continue // skip deleted issues
			}
			return nil, fmt.Errorf("fetch issue #%d: %w", n.Number, err)
		}
		issues = append(issues, issue)
	}
	return issues, nil
}

// matchMilestoneByTitle applies the four-level matching order specified in
// issue #177: exact → case-insensitive exact → prefix → contains.
// Returns the matched items for each level; callers apply precedence.
func matchMilestoneByTitle(milestones []MilestoneItem, query string) []MilestoneItem {
	queryLower := strings.ToLower(strings.TrimSpace(query))

	// Level 1: exact
	for _, m := range milestones {
		if m.Title == query {
			return []MilestoneItem{m}
		}
	}
	// Level 2: case-insensitive exact
	var ciMatches []MilestoneItem
	for _, m := range milestones {
		if strings.ToLower(m.Title) == queryLower {
			ciMatches = append(ciMatches, m)
		}
	}
	if len(ciMatches) > 0 {
		return ciMatches
	}
	// Level 3: prefix
	var prefixMatches []MilestoneItem
	for _, m := range milestones {
		if strings.HasPrefix(strings.ToLower(m.Title), queryLower) {
			prefixMatches = append(prefixMatches, m)
		}
	}
	if len(prefixMatches) > 0 {
		return prefixMatches
	}
	// Level 4: contains
	var containsMatches []MilestoneItem
	for _, m := range milestones {
		if strings.Contains(strings.ToLower(m.Title), queryLower) {
			containsMatches = append(containsMatches, m)
		}
	}
	return containsMatches
}

// openMilestones filters to open milestones for user guidance in error messages.
func openMilestones(milestones []MilestoneItem) []MilestoneItem {
	var out []MilestoneItem
	for _, m := range milestones {
		if m.State == "open" {
			out = append(out, m)
		}
	}
	return out
}

// ResolveGitHubMilestone resolves a /mr selector to a MilestoneItem using only
// GitHub as the source of truth. Three selector forms are supported:
//
//   - "#N" or "N" (numeric): fetch issue N, return its milestone
//   - text query (title string): fuzzy-match against GitHub milestone titles
//   - "current": not yet implemented; returns an error
//
// Returns one of the typed error values (ErrNoMilestoneOnIssue, ErrIssueNotFound,
// ErrNoMilestoneMatch, ErrAmbiguousMilestone) for actionable user feedback.
func ResolveGitHubMilestone(ctx context.Context, projectDir, selector string) (*MilestoneItem, error) {
	selector = strings.TrimSpace(selector)

	// "current" is a placeholder for future context-aware resolution.
	if strings.ToLower(selector) == "current" {
		return nil, fmt.Errorf("'current' selector is not yet implemented; use /mr #<issue> or /mr <milestone-title>")
	}

	// Issue selector: "#N" or bare numeric string.
	issueNum, isIssue := parseIssueSelector(selector)
	if isIssue {
		_, ms, err := FetchIssueWithMilestone(ctx, projectDir, issueNum)
		if err != nil {
			return nil, err
		}
		if ms == nil {
			return nil, &ErrNoMilestoneOnIssue{IssueNumber: issueNum}
		}
		// ms only has Number+Title from the issue view; fetch full metadata.
		full, err := fetchMilestoneByNumber(ctx, projectDir, ms.Number)
		if err != nil {
			// Fall back to the partial data; not a hard failure.
			return ms, nil
		}
		return full, nil
	}

	// Title query selector.
	allMilestones, err := FetchMilestones(ctx, projectDir)
	if err != nil {
		return nil, fmt.Errorf("fetch milestones: %w", err)
	}

	matches := matchMilestoneByTitle(allMilestones, selector)
	switch len(matches) {
	case 0:
		return nil, &ErrNoMilestoneMatch{
			Query:     selector,
			Available: openMilestones(allMilestones),
		}
	case 1:
		return &matches[0], nil
	default:
		return nil, &ErrAmbiguousMilestone{
			Query:      selector,
			Candidates: matches,
		}
	}
}

// parseIssueSelector detects "#N" or bare numeric selectors and returns the
// issue number. Returns (0, false) for non-numeric selectors.
func parseIssueSelector(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "#") {
		s = s[1:]
	}
	// Must be purely numeric.
	if s == "" {
		return 0, false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
	}
	if n <= 0 {
		return 0, false
	}
	return n, true
}

// fetchMilestoneByNumber retrieves a single milestone by number via the REST API.
func fetchMilestoneByNumber(ctx context.Context, projectDir string, number int) (*MilestoneItem, error) {
	out, err := ghOutput(ctx, projectDir, ghTimeoutShort,
		"api", fmt.Sprintf("repos/{owner}/{repo}/milestones/%d", number),
	)
	if err != nil {
		return nil, fmt.Errorf("gh api milestone %d: %w", number, err)
	}
	var raw ghMilestoneJSON
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parse milestone JSON: %w", err)
	}
	m := &MilestoneItem{
		Number:       raw.Number,
		Title:        raw.Title,
		State:        raw.State,
		Description:  raw.Description,
		OpenIssues:   raw.OpenIssues,
		ClosedIssues: raw.ClosedIssues,
	}
	if raw.DueOn != "" {
		if t, err := time.Parse(time.RFC3339, raw.DueOn); err == nil {
			m.DueOn = &t
		}
	}
	return m, nil
}

// ── Milestone review pipeline ─────────────────────────────────────────────────

// MilestoneReviewContext holds all GitHub data collected for a milestone review.
type MilestoneReviewContext struct {
	Milestone MilestoneItem
	Issues    []*IssueContext
}

// MilestoneReviewResult is the structured output from the LLM reviewer.
type MilestoneReviewResult struct {
	Verdict         string // "NOT READY" | "READY WITH RISKS" | "READY"
	Score           int    // 0–100
	Blockers        []string
	Gaps            []string
	Inconsistencies []string
	Closeout        []string
	NextActions     []string
	RawText         string
}

const (
	milestoneIssueTruncBody    = 3000 // max chars per issue body
	milestoneIssueTruncComment = 600  // max chars per comment body
	milestoneMaxComments       = 8    // max comments per issue included in prompt
)

// LoadMilestoneReviewContext fetches full milestone metadata and all issues
// (open + closed) with bodies and recent comments bounded for prompt size.
func LoadMilestoneReviewContext(ctx context.Context, projectDir string, ms *MilestoneItem) (*MilestoneReviewContext, error) {
	// Ensure we have full metadata if caller only has partial (Number+Title).
	if ms.State == "" && ms.Number > 0 {
		full, err := fetchMilestoneByNumber(ctx, projectDir, ms.Number)
		if err == nil {
			ms = full
		}
	}

	issues, err := FetchMilestoneIssues(ctx, projectDir, ms.Number)
	if err != nil {
		return nil, fmt.Errorf("load milestone issues: %w", err)
	}

	// Truncate bodies and limit comments for prompt budget.
	for _, iss := range issues {
		if len(iss.Body) > milestoneIssueTruncBody {
			iss.Body = iss.Body[:milestoneIssueTruncBody] + "\n...[truncated]"
		}
		if len(iss.Comments) > milestoneMaxComments {
			iss.Comments = iss.Comments[len(iss.Comments)-milestoneMaxComments:]
		}
		for i, c := range iss.Comments {
			if len(c.Body) > milestoneIssueTruncComment {
				iss.Comments[i].Body = c.Body[:milestoneIssueTruncComment] + "...[truncated]"
			}
		}
	}

	return &MilestoneReviewContext{Milestone: *ms, Issues: issues}, nil
}

// buildMilestoneReviewPrompt assembles the LLM prompt for milestone review.
func buildMilestoneReviewPrompt(mrc *MilestoneReviewContext) string {
	ms := mrc.Milestone
	var sb strings.Builder

	sb.WriteString("You are a senior engineering lead performing a GitHub milestone closeout review.\n")
	sb.WriteString("Your job: read all issues in the milestone and produce a structured verdict.\n\n")
	sb.WriteString("## Milestone\n\n")
	fmt.Fprintf(&sb, "- Number: %d\n- Title: %s\n- State: %s\n", ms.Number, ms.Title, ms.State)
	if ms.Description != "" {
		fmt.Fprintf(&sb, "- Description: %s\n", ms.Description)
	}
	if ms.DueOn != nil {
		fmt.Fprintf(&sb, "- Due: %s\n", ms.DueOn.Format("2006-01-02"))
	}
	fmt.Fprintf(&sb, "- Open issues: %d  |  Closed issues: %d\n\n", ms.OpenIssues, ms.ClosedIssues)

	sb.WriteString("## Issues\n\n")
	for _, iss := range mrc.Issues {
		fmt.Fprintf(&sb, "### #%d [%s] %s\n", iss.Number, strings.ToUpper(iss.State), iss.Title)
		if len(iss.Labels) > 0 {
			fmt.Fprintf(&sb, "Labels: %s\n", strings.Join(iss.Labels, ", "))
		}
		checkedCount := 0
		for _, item := range iss.Checklist {
			if item.Checked {
				checkedCount++
			}
		}
		if len(iss.Checklist) > 0 {
			fmt.Fprintf(&sb, "Checklist: %d/%d checked\n", checkedCount, len(iss.Checklist))
		}
		if iss.Body != "" {
			sb.WriteString("Body:\n")
			sb.WriteString(iss.Body)
			sb.WriteString("\n")
		}
		if len(iss.Comments) > 0 {
			sb.WriteString("Recent comments:\n")
			for _, c := range iss.Comments {
				fmt.Fprintf(&sb, "  [%s] %s: %s\n", c.CreatedAt.Format("2006-01-02"), c.Author, c.Body)
			}
		}
		sb.WriteString("\n")
	}

	sb.WriteString(`## Required output format

Reply ONLY with this exact structure (no extra prose before or after):

VERDICT: <NOT READY|READY WITH RISKS|READY>
SCORE: <0-100>

BLOCKERS:
- <item or "None">

GAPS:
- <item or "None">

INCONSISTENCIES:
- <item or "None">

CLOSEOUT CHECKLIST:
- <item>

RECOMMENDED NEXT ACTIONS:
- <item>

Evaluation dimensions:
- Milestone goal clarity vs actual issue set
- Open blockers and whether any closed issue should be revisited
- Cross-issue consistency (API contracts, config names, docs, UI wording, model choices)
- Missing acceptance criteria or validation evidence
- Docs / deployment / config / release-readiness gaps
- Duplicate or stale issues
- Whether the milestone is safe to close
`)
	return sb.String()
}

var (
	mrVerdictRe = regexp.MustCompile(`(?i)^VERDICT:\s*(.+)$`)
	mrScoreRe   = regexp.MustCompile(`(?i)^SCORE:\s*(\d+)`)
	mrSectionRe = regexp.MustCompile(`(?i)^(BLOCKERS|GAPS|INCONSISTENCIES|CLOSEOUT CHECKLIST|RECOMMENDED NEXT ACTIONS):\s*$`)
	mrBulletRe  = regexp.MustCompile(`^[-*]\s+(.+)$`)
)

// parseMilestoneReviewResult extracts structured fields from LLM text output.
func parseMilestoneReviewResult(raw string) *MilestoneReviewResult {
	r := &MilestoneReviewResult{RawText: raw}
	lines := strings.Split(raw, "\n")
	currentSection := ""
	for _, line := range lines {
		line = strings.TrimRight(line, " \t")

		if m := mrVerdictRe.FindStringSubmatch(line); len(m) == 2 {
			v := strings.TrimSpace(strings.ToUpper(m[1]))
			switch {
			case strings.Contains(v, "READY WITH RISKS"):
				r.Verdict = "READY WITH RISKS"
			case strings.Contains(v, "NOT READY"):
				r.Verdict = "NOT READY"
			case strings.Contains(v, "READY"):
				r.Verdict = "READY"
			default:
				r.Verdict = strings.TrimSpace(m[1])
			}
			continue
		}
		if m := mrScoreRe.FindStringSubmatch(line); len(m) == 2 {
			if n, err := strconv.Atoi(m[1]); err == nil {
				r.Score = n
			}
			continue
		}
		if m := mrSectionRe.FindStringSubmatch(line); len(m) == 2 {
			currentSection = strings.ToUpper(m[1])
			continue
		}
		if m := mrBulletRe.FindStringSubmatch(line); len(m) == 2 {
			item := strings.TrimSpace(m[1])
			if strings.EqualFold(item, "none") {
				continue
			}
			switch currentSection {
			case "BLOCKERS":
				r.Blockers = append(r.Blockers, item)
			case "GAPS":
				r.Gaps = append(r.Gaps, item)
			case "INCONSISTENCIES":
				r.Inconsistencies = append(r.Inconsistencies, item)
			case "CLOSEOUT CHECKLIST":
				r.Closeout = append(r.Closeout, item)
			case "RECOMMENDED NEXT ACTIONS":
				r.NextActions = append(r.NextActions, item)
			}
		}
	}
	if r.Verdict == "" {
		r.Verdict = "NOT READY"
	}
	return r
}

// RunMilestoneReview invokes the configured LLM via callFn and returns a
// structured review result. Read-only: never mutates GitHub state.
func RunMilestoneReview(ctx context.Context, callFn CallPlanFunc, projectDir string, mrc *MilestoneReviewContext) (*MilestoneReviewResult, error) {
	prompt := buildMilestoneReviewPrompt(mrc)
	res, err := callFn(ctx, prompt, projectDir, "")
	if err != nil {
		return nil, fmt.Errorf("milestone review LLM call: %w", err)
	}
	text := strings.TrimSpace(res.Text)
	if text == "" {
		return nil, fmt.Errorf("milestone review: empty LLM response")
	}
	return parseMilestoneReviewResult(text), nil
}

// verdictEmoji returns a display emoji for a verdict string.
func verdictEmoji(verdict string) string {
	switch strings.ToUpper(strings.TrimSpace(verdict)) {
	case "READY":
		return "✅"
	case "READY WITH RISKS":
		return "⚠️"
	default:
		return "🚫"
	}
}

// buildMilestoneReviewMarkdown renders the full review as Markdown.
func buildMilestoneReviewMarkdown(ms *MilestoneItem, r *MilestoneReviewResult) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# Milestone Review: %s\n\n", ms.Title)
	fmt.Fprintf(&sb, "**Verdict:** %s %s  \n", verdictEmoji(r.Verdict), r.Verdict)
	fmt.Fprintf(&sb, "**Score:** %d/100\n\n", r.Score)

	writeSection := func(title string, items []string) {
		fmt.Fprintf(&sb, "## %s\n\n", title)
		if len(items) == 0 {
			sb.WriteString("None.\n\n")
			return
		}
		for _, item := range items {
			fmt.Fprintf(&sb, "- %s\n", item)
		}
		sb.WriteString("\n")
	}

	writeSection("Blockers", r.Blockers)
	writeSection("Gaps", r.Gaps)
	writeSection("Cross-issue Inconsistencies", r.Inconsistencies)
	writeSection("Closeout Checklist", r.Closeout)
	writeSection("Recommended Next Actions", r.NextActions)

	if r.RawText != "" {
		sb.WriteString("---\n\n## Raw LLM Response\n\n")
		sb.WriteString(r.RawText)
		sb.WriteString("\n")
	}
	return sb.String()
}

// milestoneSlug returns a filesystem-safe slug for a milestone title.
func milestoneSlug(title string) string {
	slug := strings.ToLower(title)
	slug = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")
	if len(slug) > 40 {
		slug = slug[:40]
	}
	return slug
}

// FormatMilestoneReview formats a compact Telegram summary and writes the full
// Markdown report to temp/milestone-review-<slug>-<ts>.md. When the report is
// long, the Telegram message includes a [SEND_FILE:...] marker so the bot layer
// dispatches the file automatically. Read-only: no GitHub mutations.
func FormatMilestoneReview(projectDir string, ms *MilestoneItem, r *MilestoneReviewResult) (string, error) {
	// Write full markdown report to temp/.
	slug := milestoneSlug(ms.Title)
	ts := time.Now().Format("20060102-150405")
	relPath := fmt.Sprintf("temp/milestone-review-%s-%s.md", slug, ts)
	absPath := filepath.Join(projectDir, relPath)

	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return "", fmt.Errorf("mkdir temp: %w", err)
	}
	md := buildMilestoneReviewMarkdown(ms, r)
	if err := os.WriteFile(absPath, []byte(md), 0o644); err != nil {
		return "", fmt.Errorf("write milestone review: %w", err)
	}

	// Compact Telegram summary.
	var sb strings.Builder
	fmt.Fprintf(&sb, "🧭 *Milestone Review: %s*\n", ms.Title)
	fmt.Fprintf(&sb, "Verdict: %s %s\nScore: %d/100\n", verdictEmoji(r.Verdict), r.Verdict, r.Score)

	if len(r.Blockers) > 0 {
		sb.WriteString("\n*Blockers:*\n")
		for i, b := range r.Blockers {
			fmt.Fprintf(&sb, "%d. %s\n", i+1, b)
		}
	}
	if len(r.Gaps) > 0 {
		sb.WriteString("\n*Gaps:*\n")
		for i, g := range r.Gaps {
			fmt.Fprintf(&sb, "%d. %s\n", i+1, g)
		}
	}
	if len(r.Inconsistencies) > 0 {
		sb.WriteString("\n*Cross-issue inconsistencies:*\n")
		for i, c := range r.Inconsistencies {
			fmt.Fprintf(&sb, "%d. %s\n", i+1, c)
		}
	}
	if len(r.Closeout) > 0 {
		sb.WriteString("\n*Closeout checklist:*\n")
		for _, item := range r.Closeout {
			fmt.Fprintf(&sb, "- %s\n", item)
		}
	}
	if len(r.NextActions) > 0 {
		sb.WriteString("\n*Recommended next actions:*\n")
		for i, a := range r.NextActions {
			fmt.Fprintf(&sb, "%d. %s\n", i+1, a)
		}
	}

	// Attach full report when summary is long or report has content.
	if len(md) > 500 {
		fmt.Fprintf(&sb, "\n[SEND_FILE:%s]", relPath)
	}

	return sb.String(), nil
}
