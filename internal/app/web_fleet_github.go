package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	alicev1 "claude-tg-agent/gen/go/alice/v1"

	"github.com/chimerakang/alice-core/fleet"
	"google.golang.org/protobuf/proto"
)

// Testable vars for GitHub operations during Fleet dispatch, following the same
// pattern as tasksGitHubIssueListFunc.
var (
	fleetGitHubIssueCreateFunc      = createFleetGitHubIssue
	fleetGitHubMilestoneGetOrCreate = getOrCreateFleetGitHubMilestone
	fleetGitHubMilestoneListFunc    = listFleetGitHubMilestones
	fleetGitHubEpicUpdateFunc       = appendToFleetGitHubEpic
	fleetGitHubLabelEnsureFunc      = ensureFleetGitHubLabel
	fleetGitHubLabeledIssueFunc     = createFleetGitHubLabeledIssue
	fleetGitHubStatusFunc           = checkFleetGitHubStatus
	// fleetDispatchFromIssueFunc wraps the Commander.Dispatch call for the
	// "from issue" path so tests can substitute it without needing real gh.
	fleetDispatchFromIssueFunc = defaultFleetDispatchFromIssue
)

type fleetCreatedIssue struct {
	Number  int    `json:"number"`
	HTMLURL string `json:"html_url"`
}

func createFleetGitHubIssue(ctx context.Context, repoPath, title, body string, milestoneNumber int) (fleetCreatedIssue, error) {
	return createFleetGitHubLabeledIssue(ctx, repoPath, title, body, milestoneNumber, nil)
}

// createFleetGitHubLabeledIssue creates an issue carrying the given labels.
func createFleetGitHubLabeledIssue(ctx context.Context, repoPath, title, body string, milestoneNumber int, labels []string) (fleetCreatedIssue, error) {
	args := []string{
		"api", "repos/:owner/:repo/issues", "-X", "POST",
		"-f", "title=" + title,
		"-f", "body=" + body,
	}
	for _, l := range labels {
		args = append(args, "-f", "labels[]="+l)
	}
	if milestoneNumber > 0 {
		args = append(args, "-F", fmt.Sprintf("milestone=%d", milestoneNumber))
	}
	out, err := runProcessCombinedOutput(ctx, ProcessOptions{Dir: repoPath, Timeout: 30 * time.Second}, "gh", args...)
	if err != nil {
		return fleetCreatedIssue{}, ghWrapError(out, err, "create issue")
	}
	var result fleetCreatedIssue
	if err := json.Unmarshal(out, &result); err != nil {
		return fleetCreatedIssue{}, fmt.Errorf("parse issue creation response: %w", err)
	}
	return result, nil
}

type fleetMilestoneJSON struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
}

func listFleetGitHubMilestones(ctx context.Context, repoPath string) ([]fleetMilestoneJSON, error) {
	out, err := runProcessCombinedOutput(ctx, ProcessOptions{Dir: repoPath, Timeout: 30 * time.Second}, "gh",
		"api", "repos/:owner/:repo/milestones?state=open&per_page=100",
	)
	if err != nil {
		return nil, ghWrapError(out, err, "list milestones")
	}
	var list []fleetMilestoneJSON
	if err := json.Unmarshal(out, &list); err != nil {
		return nil, fmt.Errorf("parse milestones: %w", err)
	}
	return list, nil
}

// getOrCreateFleetGitHubMilestone returns the number of the open milestone with
// that title, creating it when there is none, and reports whether it did.
func getOrCreateFleetGitHubMilestone(ctx context.Context, repoPath, title string) (int, bool, error) {
	milestones, err := listFleetGitHubMilestones(ctx, repoPath)
	if err != nil {
		return 0, false, err
	}
	want := strings.ToLower(strings.TrimSpace(title))
	for _, m := range milestones {
		if strings.ToLower(strings.TrimSpace(m.Title)) == want {
			return m.Number, false, nil
		}
	}
	// Milestone not found; create it.
	out, err := runProcessCombinedOutput(ctx, ProcessOptions{Dir: repoPath, Timeout: 30 * time.Second}, "gh",
		"api", "repos/:owner/:repo/milestones", "-X", "POST",
		"-f", "title="+strings.TrimSpace(title),
	)
	if err != nil {
		return 0, false, ghWrapError(out, err, "create milestone")
	}
	var created fleetMilestoneJSON
	if err := json.Unmarshal(out, &created); err != nil {
		return 0, false, fmt.Errorf("parse created milestone: %w", err)
	}
	return created.Number, true, nil
}

type fleetIssueBodyJSON struct {
	Body string `json:"body"`
}

func appendToFleetGitHubEpic(ctx context.Context, repoPath string, epicNumber, issueNumber int, issueTitle string) error {
	out, err := runProcessCombinedOutput(ctx, ProcessOptions{Dir: repoPath, Timeout: 30 * time.Second}, "gh",
		"api", fmt.Sprintf("repos/:owner/:repo/issues/%d", epicNumber),
	)
	if err != nil {
		return ghWrapError(out, err, "fetch epic body")
	}
	var epic fleetIssueBodyJSON
	if err := json.Unmarshal(out, &epic); err != nil {
		return fmt.Errorf("parse epic body: %w", err)
	}
	body := appendEpicChild(epic.Body, fmt.Sprintf("- [ ] #%d %s", issueNumber, issueTitle))
	patchOut, err := runProcessCombinedOutput(ctx, ProcessOptions{Dir: repoPath, Timeout: 30 * time.Second}, "gh",
		"api", fmt.Sprintf("repos/:owner/:repo/issues/%d", epicNumber),
		"-X", "PATCH",
		"-f", "body="+body,
	)
	if err != nil {
		return ghWrapError(patchOut, err, "update epic body")
	}
	return nil
}

// epicChildItem matches a checklist line that refers to an issue, which is what
// tells an epic's list of children from its other checklists.
var epicChildItem = regexp.MustCompile(`^\s*[-*+] \[[ xX]\] .*#\d+`)

// appendEpicChild adds a child item at the end of an epic's list of children.
// When the body ends with the (empty) sub-issue heading that createEpicBody
// leaves, the item goes under it, separated by a blank line. Otherwise it
// follows the last child already listed, and failing that the last line.
func appendEpicChild(body, item string) string {
	body = strings.TrimRight(body, "\r\n \t")
	if body == "" {
		return item
	}
	lines := strings.Split(body, "\n")
	if strings.HasPrefix(strings.TrimSpace(lines[len(lines)-1]), "#") {
		return body + "\n\n" + item
	}
	last, fenced := -1, false
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fenced = !fenced
		} else if !fenced && epicChildItem.MatchString(line) {
			last = i
		}
	}
	if last < 0 {
		return body + "\n" + item
	}
	// Step over what is indented beneath the last child.
	indent := func(line string) int { return len(line) - len(strings.TrimLeft(line, " \t")) }
	end := last + 1
	for end < len(lines) && strings.TrimSpace(lines[end]) != "" && indent(lines[end]) > indent(lines[last]) {
		end++
	}
	if strings.HasSuffix(lines[last], "\r") {
		item += "\r"
	}
	return strings.Join(slices.Insert(lines, end, item), "\n")
}

// buildEpicBody is the description followed by an empty sub-issue section,
// which later children are appended into.
func buildEpicBody(description, heading string) string {
	return strings.TrimRight(description, "\r\n \t") + "\n\n## " + heading + "\n"
}

// ensureFleetGitHubLabel creates the label when the repo lacks it and reports
// whether it did.
func ensureFleetGitHubLabel(ctx context.Context, repoPath, name string) (bool, error) {
	out, err := runProcessCombinedOutput(ctx, ProcessOptions{Dir: repoPath, Timeout: 30 * time.Second}, "gh",
		"api", "repos/:owner/:repo/labels", "-X", "POST", "-f", "name="+name,
	)
	if err != nil {
		if strings.Contains(strings.ToLower(string(out)), "already_exists") {
			return false, nil
		}
		return false, ghWrapError(out, err, "create label")
	}
	return true, nil
}

// checkFleetGitHubStatus reports whether gh is logged in and the repo has a
// GitHub remote: nil, errTasksGitHubAuthRequired or errTasksNoGitHubRepo.
func checkFleetGitHubStatus(ctx context.Context, repoPath string) error {
	out, err := runProcessCombinedOutput(ctx, ProcessOptions{Dir: repoPath, Timeout: 20 * time.Second}, "gh",
		"repo", "view", "--json", "url",
	)
	if err != nil {
		return ghWrapError(out, err, "check github")
	}
	return nil
}

// Commands under which GitHub writes appear in the fleet's audit log.
const (
	fleetAuditCreateMilestone = "create_milestone"
	fleetAuditCreateLabel     = "create_label"
	fleetAuditCreateIssue     = "create_issue"
	fleetAuditCreateEpic      = "create_epic"
	fleetAuditUpdateEpic      = "update_epic"
)

// auditGitHubWrite records one change made on GitHub. The core only records its
// own commands, so this writes the shared audit schema, as CheckActorAction
// does. A failure to record never fails the command.
func (f *FleetService) auditGitHubWrite(ctx context.Context, actor fleet.Actor, command, projectID, taskID, detail string, err error) {
	if f.Accept == nil {
		return
	}
	failure := ""
	if err != nil {
		failure = fleetClip(err.Error(), 500)
	}
	// The write has happened even if the request has already gone away.
	_, auditErr := f.Accept.db.ExecContext(context.WithoutCancel(ctx), `INSERT INTO fleet_audit
		(actor,via,command,project_id,task_id,detail,ok,error,created_at)
		VALUES (?,?,?,?,?,?,?,?,?)`, actor.ID, actor.Via, command, projectID, taskID,
		fleetClip(detail, 500), err == nil, failure, time.Now().UnixMilli())
	if auditErr != nil {
		log.Printf("[fleet] record %s audit: %v", command, auditErr)
	}
}

// githubMilestone finds or creates the milestone, recording a creation or a
// failure. Using an existing one changes nothing and is not recorded.
func (f *FleetService) githubMilestone(ctx context.Context, actor fleet.Actor, project fleet.Project, title string) (int, error) {
	number, created, err := fleetGitHubMilestoneGetOrCreate(ctx, project.RepoPath, title)
	if created || err != nil {
		f.auditGitHubWrite(ctx, actor, fleetAuditCreateMilestone, project.ID, "", title, err)
	}
	return number, err
}

// ghWrapError maps gh CLI errors onto the sentinel errors used by the fleet API.
func ghWrapError(out []byte, err error, op string) error {
	lower := strings.ToLower(string(out) + " " + err.Error())
	switch {
	case strings.Contains(lower, "authentication required"),
		strings.Contains(lower, "not logged in"),
		strings.Contains(lower, "gh auth login"),
		strings.Contains(lower, "must authenticate"),
		strings.Contains(lower, "401 unauthorized"),
		strings.Contains(lower, "authentication failed"):
		return errTasksGitHubAuthRequired
	case strings.Contains(lower, "could not resolve to a repository"),
		strings.Contains(lower, "not a git repository"),
		strings.Contains(lower, "no remotes configured"),
		strings.Contains(lower, "no remotes found"),
		strings.Contains(lower, "no github remote found"):
		return errTasksNoGitHubRepo
	default:
		return fmt.Errorf("%s: %w (%s)", op, err, strings.TrimSpace(string(out)))
	}
}

// isEpicIssue reports whether the issue is an epic based on its labels and
// title, using the labels and pattern from the fleet config.
func isEpicIssue(labels []string, title string, epicLabels []string, epicPattern *regexp.Regexp) bool {
	for _, label := range labels {
		for _, epicLabel := range epicLabels {
			if strings.EqualFold(label, epicLabel) {
				return true
			}
		}
	}
	if epicPattern != nil && epicPattern.MatchString(title) {
		return true
	}
	return false
}

// compileEpicPattern compiles the fleet config's epic title regexp, or returns
// nil when the pattern is empty. Logs a warning and returns nil on bad syntax.
func compileEpicPattern(pattern string) *regexp.Regexp {
	if strings.TrimSpace(pattern) == "" {
		return nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil
	}
	return re
}

// resolvedEpicLabels returns the epic labels from config, defaulting to ["epic"].
func resolvedEpicLabels(labels []string) []string {
	if len(labels) > 0 {
		return labels
	}
	return []string{"epic"}
}

// defaultFleetDispatchFromIssue is the real implementation: it calls
// Commander.Dispatch with the issue number, which triggers the alice-core
// IssueTracker to fetch the issue from GitHub.
func defaultFleetDispatchFromIssue(ctx context.Context, svc *FleetService, actor fleet.Actor, projectID string, issueNum int, opts fleet.DispatchRequest) (fleet.Task, error) {
	return fleetCommanderDispatch(ctx, svc, actor, opts)
}

// defaultEpicChildrenHeading is used when fleet.epic_children_heading is unset.
const defaultEpicChildrenHeading = "Sub-issues"

// fleetCreateEpic handles POST /api/fleet/epics: it creates an epic issue on
// GitHub and nothing else. No task is created.
func (wi *WebInterface) fleetCreateEpic(ctx context.Context, svc *FleetService, r *http.Request) (proto.Message, error) {
	var req alicev1.FleetEpicRequest
	if err := readFleetBody(r, &req); err != nil {
		return nil, err
	}
	description := strings.TrimSpace(req.Description)
	switch {
	case strings.TrimSpace(req.ProjectId) == "":
		return nil, fleetBadRequest("project_id is required")
	case description == "":
		return nil, fleetBadRequest("a description is required")
	}
	project, err := svc.Commander.GetProject(ctx, strings.TrimSpace(req.ProjectId))
	if err != nil {
		return nil, err
	}
	actor := wi.fleetWebActor()
	title := firstNonEmptyLine(description)
	heading := strings.TrimSpace(svc.Config.EpicChildrenHeading)
	if heading == "" {
		heading = defaultEpicChildrenHeading
	}
	label := resolvedEpicLabels(svc.Config.EpicLabels)[0]

	var milestoneNumber int
	if ms := strings.TrimSpace(req.Milestone); ms != "" {
		if milestoneNumber, err = svc.githubMilestone(ctx, actor, project, ms); err != nil {
			return nil, err
		}
	}
	labelCreated, err := fleetGitHubLabelEnsureFunc(ctx, project.RepoPath, label)
	if labelCreated || err != nil {
		svc.auditGitHubWrite(ctx, actor, fleetAuditCreateLabel, project.ID, "", label, err)
	}
	if err != nil {
		return nil, err
	}
	created, err := fleetGitHubLabeledIssueFunc(ctx, project.RepoPath, title, buildEpicBody(description, heading), milestoneNumber, []string{label})
	detail := title
	if err == nil {
		detail = fmt.Sprintf("#%d %s", created.Number, title)
	}
	svc.auditGitHubWrite(ctx, actor, fleetAuditCreateEpic, project.ID, "", detail, err)
	if err != nil {
		return nil, err
	}
	out := &alicev1.FleetEpicReply{
		Number: int32(created.Number), Title: title, Url: created.HTMLURL,
		Label: label, LabelCreated: labelCreated,
	}
	if pattern := strings.TrimSpace(svc.Config.EpicTitlePattern); pattern != "" {
		if re := compileEpicPattern(pattern); re != nil && !re.MatchString(title) {
			out.TitlePatternMismatch, out.TitlePattern = true, pattern
		}
	}
	return out, nil
}
