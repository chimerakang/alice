package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// fleetMilestoneConfig holds the rules a milestone batch reads issues by.
// Every list left empty takes its default.
type fleetMilestoneConfig struct {
	// DependencyMarkers start a body line that lists what an issue depends
	// on, e.g. "依賴:#58、#65".
	DependencyMarkers []string `json:"dependency_markers"`
	// BlockedLabels hold an issue back until the label is removed.
	BlockedLabels []string `json:"blocked_labels"`
	// ExcludeLabels keep an issue out of the batch. Epics (fleet.epic_labels)
	// are always out.
	ExcludeLabels []string `json:"exclude_labels"`
	// ReconcileSeconds is how often active batches are re-read from GitHub.
	ReconcileSeconds int `json:"reconcile_seconds"`
}

var (
	defaultMilestoneDependencyMarkers = []string{"依賴", "相依", "Depends on", "Blocked by"}
	defaultMilestoneBlockedLabels     = []string{"blocked"}
	defaultMilestoneExcludeLabels     = []string{"duplicate", "invalid", "wontfix"}
)

const (
	defaultMilestoneReconcile = 2 * time.Minute
	milestoneGHOutputLimit    = 8 << 20
)

func (c fleetMilestoneConfig) withDefaults() fleetMilestoneConfig {
	if len(c.DependencyMarkers) == 0 {
		c.DependencyMarkers = defaultMilestoneDependencyMarkers
	}
	if len(c.BlockedLabels) == 0 {
		c.BlockedLabels = defaultMilestoneBlockedLabels
	}
	if len(c.ExcludeLabels) == 0 {
		c.ExcludeLabels = defaultMilestoneExcludeLabels
	}
	if c.ReconcileSeconds <= 0 {
		c.ReconcileSeconds = int(defaultMilestoneReconcile / time.Second)
	}
	return c
}

func hasAnyLabel(labels, wanted []string) bool {
	return slices.ContainsFunc(labels, func(l string) bool {
		return slices.ContainsFunc(wanted, func(w string) bool { return strings.EqualFold(l, w) })
	})
}

// issueRefRe matches a reference to an issue of the same repository.
// "owner/repo#4" names another repository and is not matched.
var issueRefRe = regexp.MustCompile(`(?:^|[^\w/-])#(\d+)\b`)

// parseDependencies returns the issues a body says it depends on, in the
// order given. Only lines that start with one of the markers count.
func parseDependencies(body string, markers []string) []int {
	quoted := make([]string, 0, len(markers))
	for _, m := range markers {
		if m = strings.TrimSpace(m); m != "" {
			quoted = append(quoted, regexp.QuoteMeta(m))
		}
	}
	if len(quoted) == 0 {
		return nil
	}
	line := regexp.MustCompile(`(?mi)^[\s>*_-]*(?:\[[ x]\]\s*)?(?:` + strings.Join(quoted, "|") + `)[\s*_]*[:：]?(.*#\d+.*)$`)
	var deps []int
	for _, m := range line.FindAllStringSubmatch(body, -1) {
		for _, ref := range issueRefRe.FindAllStringSubmatch(m[1], -1) {
			if n, err := strconv.Atoi(ref[1]); err == nil && n > 0 && !slices.Contains(deps, n) {
				deps = append(deps, n)
			}
		}
	}
	return deps
}

// milestoneIssue is an issue as a batch needs to see it.
type milestoneIssue struct {
	Number int
	Title  string
	Body   string
	// State is "open" or "closed"; StateReason says why it was closed.
	State       string
	StateReason string
	Labels      []string
	// Milestone is zero for an issue without one.
	Milestone      int
	MilestoneTitle string
}

func (i milestoneIssue) closed() bool { return strings.EqualFold(i.State, "closed") }

// completed reports whether the issue was closed as done, as opposed to not
// planned or a duplicate.
func (i milestoneIssue) completed() bool {
	return i.closed() && (i.StateReason == "" || strings.EqualFold(i.StateReason, "completed"))
}

// fleetMilestoneHost reads milestones. It never writes.
type fleetMilestoneHost interface {
	Issue(ctx context.Context, dir string, number int) (milestoneIssue, error)
	MilestoneIssues(ctx context.Context, dir string, milestone int) ([]milestoneIssue, error)
}

type ghMilestoneHost struct{}

type ghRESTIssue struct {
	Number      int    `json:"number"`
	Title       string `json:"title"`
	Body        string `json:"body"`
	State       string `json:"state"`
	StateReason string `json:"state_reason"`
	Labels      []struct {
		Name string `json:"name"`
	} `json:"labels"`
	Milestone *struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
	} `json:"milestone"`
	// PullRequest is set when the entry is a pull request, which the issues
	// endpoint also returns.
	PullRequest *struct{} `json:"pull_request"`
}

func (r ghRESTIssue) issue() milestoneIssue {
	out := milestoneIssue{Number: r.Number, Title: r.Title, Body: r.Body, State: r.State, StateReason: r.StateReason}
	for _, l := range r.Labels {
		out.Labels = append(out.Labels, l.Name)
	}
	if r.Milestone != nil {
		out.Milestone, out.MilestoneTitle = r.Milestone.Number, r.Milestone.Title
	}
	return out
}

func ghMilestoneRead(ctx context.Context, dir, op string, args ...string) ([]byte, error) {
	out, err := runProcessOutput(ctx, ProcessOptions{Dir: dir, Timeout: 60 * time.Second, OutputLimit: milestoneGHOutputLimit}, "gh", args...)
	if err != nil {
		return nil, ghWrapError(out, err, op)
	}
	return out, nil
}

func (ghMilestoneHost) Issue(ctx context.Context, dir string, number int) (milestoneIssue, error) {
	out, err := ghMilestoneRead(ctx, dir, "read issue", "api", fmt.Sprintf("repos/:owner/:repo/issues/%d", number))
	if err != nil {
		return milestoneIssue{}, err
	}
	var raw ghRESTIssue
	if err := json.Unmarshal(out, &raw); err != nil {
		return milestoneIssue{}, fmt.Errorf("parse issue #%d: %w", number, err)
	}
	if raw.PullRequest != nil {
		return milestoneIssue{}, fmt.Errorf("#%d is a pull request, not an issue", number)
	}
	return raw.issue(), nil
}

func (ghMilestoneHost) MilestoneIssues(ctx context.Context, dir string, milestone int) ([]milestoneIssue, error) {
	out, err := ghMilestoneRead(ctx, dir, "list milestone issues", "api", "--paginate",
		fmt.Sprintf("repos/:owner/:repo/issues?milestone=%d&state=all&per_page=100", milestone))
	if err != nil {
		return nil, err
	}
	return parseMilestoneIssuePages(out)
}

// parseMilestoneIssuePages reads gh's paginated output: one JSON array, or
// several one after another.
func parseMilestoneIssuePages(out []byte) ([]milestoneIssue, error) {
	dec := json.NewDecoder(bytes.NewReader(out))
	var issues []milestoneIssue
	for {
		var page []ghRESTIssue
		err := dec.Decode(&page)
		if errors.Is(err, io.EOF) {
			return issues, nil
		}
		if err != nil {
			return nil, fmt.Errorf("parse milestone issues: %w", err)
		}
		for _, raw := range page {
			if raw.PullRequest == nil {
				issues = append(issues, raw.issue())
			}
		}
	}
}
