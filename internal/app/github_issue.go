package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/chimerakang/alice-core/ghissue"
)

// ghIssueWithMilestoneJSON is `gh issue view --json number,title,milestone,...`.
type ghIssueWithMilestoneJSON struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	State  string `json:"state"`
	Body   string `json:"body"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
	Comments []struct {
		Author struct {
			Login string `json:"login"`
		} `json:"author"`
		Body      string    `json:"body"`
		CreatedAt time.Time `json:"createdAt"`
	} `json:"comments"`
}

// issueContext converts the parsed gh JSON into a ghissue.IssueContext with the
// given issue number (callers that did not request the "number" field pass the
// number they asked gh for).
func (raw ghIssueWithMilestoneJSON) issueContext(number int) *ghissue.IssueContext {
	labels := make([]string, len(raw.Labels))
	for i, l := range raw.Labels {
		labels[i] = l.Name
	}
	comments := make([]ghissue.IssueComment, 0, len(raw.Comments))
	for _, c := range raw.Comments {
		comments = append(comments, ghissue.IssueComment{
			Author:    c.Author.Login,
			Body:      c.Body,
			CreatedAt: c.CreatedAt,
		})
	}
	return &ghissue.IssueContext{
		Number:    number,
		Title:     raw.Title,
		Body:      raw.Body,
		State:     raw.State,
		Labels:    labels,
		Checklist: ghissue.ExtractChecklist(raw.Body),
		Comments:  comments,
	}
}

// ghTimeoutDispatch is the per-attempt limit for the issue read behind a
// Telegram Fleet dispatch. It carries over the 25s value PR #225 introduced
// (FetchIssueForDispatch) because the default ghTimeoutShort (10s) was too
// tight for dispatch reads.
const ghTimeoutDispatch = 25 * time.Second

// FetchIssueForDispatch is FetchIssue with the longer dispatch time limit. It
// reads `gh issue view N --json title,body,state,labels,comments` and retries
// once on transient failures via ghOutputWithRetry. The returned Number is the
// requested one, matching alice-core's ghissue.FetchIssue.
func FetchIssueForDispatch(ctx context.Context, projectDir string, number int) (*ghissue.IssueContext, error) {
	out, err := ghOutputWithRetry(ctx, projectDir, ghTimeoutDispatch, "issue", "view",
		fmt.Sprintf("%d", number),
		"--json", "title,body,state,labels,comments",
	)
	if err != nil {
		return nil, fmt.Errorf("gh issue view %d: %w", number, err)
	}
	var raw ghIssueWithMilestoneJSON
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parse issue JSON: %w", err)
	}
	return raw.issueContext(number), nil
}

// gh timeouts cap every `gh` invocation so a hung CLI cannot block the caller.
const (
	ghTimeoutShort = 10 * time.Second // single-issue reads
	ghTimeoutLong  = 90 * time.Second // list/search across issues
)

// ghRetryBackoff is the pause before the single retry in ghOutputWithRetry.
// Variable so tests can shorten it.
var ghRetryBackoff = 2 * time.Second

// ghOutput runs a gh subcommand rooted at projectDir and embeds a trimmed
// stderr snippet in the returned error.
func ghOutput(ctx context.Context, projectDir string, timeout time.Duration, args ...string) ([]byte, error) {
	if timeout <= 0 {
		timeout = ghTimeoutShort
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Dir = projectDir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			if len(msg) > 300 {
				msg = msg[:300] + "…"
			}
			return out, fmt.Errorf("%w: %s", err, msg)
		}
		return out, err
	}
	return out, nil
}

// isGHNotFoundErr reports whether a gh error indicates a missing issue or repository.
func isGHNotFoundErr(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not found") ||
		strings.Contains(msg, "no such") ||
		strings.Contains(msg, "could not resolve")
}

// ghOutputWithRetry retries once on transient failures. Read-only commands only.
func ghOutputWithRetry(ctx context.Context, projectDir string, timeout time.Duration, args ...string) ([]byte, error) {
	out, err := ghOutput(ctx, projectDir, timeout, args...)
	if err == nil || isGHNotFoundErr(err) || ctx.Err() != nil {
		return out, err
	}
	select {
	case <-ctx.Done():
		return out, err
	case <-time.After(ghRetryBackoff):
	}
	return ghOutput(ctx, projectDir, timeout, args...)
}
