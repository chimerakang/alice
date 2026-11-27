package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/chimerakang/alice-core/ghissue"
	"github.com/chimerakang/alice-core/process"
)

// ghFleetIssueHost is the FleetIssueHost backed by the gh and git CLIs.
type ghFleetIssueHost struct{}

const (
	ghFleetIssueTimeout = 60 * time.Second
	// ghFleetTruncated is what process appends to output it cut short.
	ghFleetTruncated = "[process output truncated]"
)

func ghFleetRun(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	out, err := process.RunCombinedOutput(ctx, process.Options{
		Dir: dir, Timeout: ghFleetIssueTimeout, OutputLimit: 256 * 1024, LogPrefix: "fleet.issuesync",
		// Comment bodies are long; keep them out of the process log.
		LogLabel: name + " " + strings.Join(args[:min(len(args), 3)], " "),
	}, name, args...)
	if err != nil {
		return out, fmt.Errorf("%s %s: %w: %s", name, args[0], err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

func (ghFleetIssueHost) Fetch(ctx context.Context, dir string, number int) (*ghissue.IssueContext, error) {
	return ghissue.FetchIssue(ctx, dir, number)
}

func (ghFleetIssueHost) CloseIssue(ctx context.Context, dir string, number int) error {
	return ghissue.CloseIssue(ctx, dir, number)
}

type ghIssueComment struct {
	ID   int64  `json:"id"`
	Body string `json:"body"`
}

func (ghFleetIssueHost) UpsertComment(ctx context.Context, dir string, number int, marker, body string) error {
	out, err := ghFleetRun(ctx, dir, "gh", "api", "--paginate",
		fmt.Sprintf("repos/{owner}/{repo}/issues/%d/comments", number),
		// Only this task's comment: a long thread would overrun the output
		// limit, and a comment missed that way would be posted again.
		"--jq", ".[] | select(.body | contains("+strconv.Quote(marker)+")) | {id: .id, body: .body}")
	if err != nil {
		return err
	}
	if strings.Contains(string(out), ghFleetTruncated) {
		return fmt.Errorf("list issue #%d comments: output truncated", number)
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var c ghIssueComment
		if json.Unmarshal([]byte(line), &c) != nil || !strings.Contains(c.Body, marker) {
			continue
		}
		if strings.TrimSpace(c.Body) == strings.TrimSpace(body) {
			return nil
		}
		_, err := ghFleetRun(ctx, dir, "gh", "api", "-X", "PATCH",
			"repos/{owner}/{repo}/issues/comments/"+strconv.FormatInt(c.ID, 10), "-f", "body="+body)
		return err
	}
	return ghissue.PostComment(ctx, dir, number, body)
}

func (ghFleetIssueHost) PullRequest(ctx context.Context, dir string, number int) (FleetPullRequest, error) {
	out, err := ghFleetRun(ctx, dir, "gh", "pr", "view", strconv.Itoa(number),
		"--json", "state,title,url,baseRefName,mergeCommit,files")
	if err != nil {
		return FleetPullRequest{}, err
	}
	var raw struct {
		State       string `json:"state"`
		Title       string `json:"title"`
		URL         string `json:"url"`
		BaseRefName string `json:"baseRefName"`
		MergeCommit struct {
			OID string `json:"oid"`
		} `json:"mergeCommit"`
		Files []struct {
			Path string `json:"path"`
		} `json:"files"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return FleetPullRequest{}, fmt.Errorf("parse pr view: %w", err)
	}
	pr := FleetPullRequest{State: raw.State, Title: raw.Title, URL: raw.URL, BaseBranch: raw.BaseRefName, MergeCommit: raw.MergeCommit.OID}
	for _, f := range raw.Files {
		pr.Files = append(pr.Files, f.Path)
	}
	return pr, nil
}

func (ghFleetIssueHost) LocalHead(ctx context.Context, dir, branch string) (string, string, error) {
	out, err := ghFleetRun(ctx, dir, "git", "log", "-1", "--format=%H%n%s", "refs/heads/"+branch)
	if err != nil {
		return "", "", err
	}
	sha, subject, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	return sha, subject, nil
}
