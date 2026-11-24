package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	alicev1 "claude-tg-agent/gen/go/alice/v1"

	"github.com/chimerakang/alice-core/fleet"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// The fleet's HTTP surface. Every body, in and out, is one of the Fleet*
// messages in proto/alice/v1/fleet.proto, as JSON with the proto's field
// names.
//
// Queries are open like the rest of the dashboard's read endpoints.
// Commands change things, so they pass the same Bearer token check as the
// other control endpoints.

// fleetWebTimeout bounds one fleet request. Dispatching from an issue and
// merging a pull request talk to GitHub, so it is well above the server's
// default write timeout, which each handler extends to match.
const fleetWebTimeout = 90 * time.Second

// Limits on what one request may ask for.
const (
	fleetWebDefaultTasks  = 200
	fleetWebMaxTasks      = 1000
	fleetWebDefaultEvents = 200
	fleetWebMaxEvents     = 1000
	fleetWebMaxBody       = 1 << 20
)

var (
	fleetJSON = protojson.MarshalOptions{UseProtoNames: true, EmitUnpopulated: true}
	// Unknown fields are refused: a mistyped field name should not be
	// silently dropped from a command.
	fleetJSONIn = protojson.UnmarshalOptions{}
)

// registerFleetRoutes adds the /api/fleet/* routes.
func (wi *WebInterface) registerFleetRoutes(mux *http.ServeMux) {
	query := func(pattern string, h func(context.Context, *FleetService, *http.Request) (proto.Message, error)) {
		mux.HandleFunc("GET "+pattern, wi.fleetHandler(false, h))
	}
	command := func(pattern string, h func(context.Context, *FleetService, *http.Request) (proto.Message, error)) {
		mux.HandleFunc(pattern, wi.fleetHandler(true, h))
	}

	query("/api/fleet/digest", wi.fleetDigest)
	query("/api/fleet/projects", wi.fleetListProjects)
	query("/api/fleet/projects/{id}", wi.fleetGetProject)
	query("/api/fleet/tasks", wi.fleetListTasks)
	query("/api/fleet/tasks/{id}", wi.fleetGetTask)
	query("/api/fleet/tasks/{id}/events", wi.fleetTaskEvents)
	query("/api/fleet/tasks/{id}/diff", wi.fleetTaskDiff)
	query("/api/fleet/tasks/{id}/verifications", wi.fleetTaskVerifications)
	query("/api/fleet/events", wi.fleetEvents)
	query("/api/fleet/audit", wi.fleetAudit)

	command("POST /api/fleet/projects", wi.fleetRegisterProject)
	command("PUT /api/fleet/projects/{id}", wi.fleetUpdateProject)
	command("POST /api/fleet/tasks", wi.fleetDispatchTask)
	command("POST /api/fleet/tasks/{id}/{action}", wi.fleetTaskAction)

	wi.registerFleetProjectRoutes(mux)
}

// fleetHandler wraps a fleet endpoint: recovery, the token check for
// commands, the fleet being enabled, the timeout, and the JSON reply.
func (wi *WebInterface) fleetHandler(isCommand bool, h func(context.Context, *FleetService, *http.Request) (proto.Message, error)) http.HandlerFunc {
	return wi.fleetHandlerWithin(isCommand, fleetWebTimeout, h)
}

// fleetHandlerWithin is fleetHandler with its own time limit, for the few
// requests that legitimately take longer.
func (wi *WebInterface) fleetHandlerWithin(isCommand bool, timeout time.Duration, h func(context.Context, *FleetService, *http.Request) (proto.Message, error)) http.HandlerFunc {
	return wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		if isCommand && wi.apiToken != "" && r.Header.Get("Authorization") != "Bearer "+wi.apiToken {
			writeFleetError(w, http.StatusUnauthorized, "unauthorized", "a valid API token is required")
			return
		}
		svc := wi.fleetService()
		if svc == nil {
			writeFleetError(w, http.StatusServiceUnavailable, "disabled", "the fleet is not enabled")
			return
		}
		// The server's write timeout is sized for quick reads.
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(timeout + 5*time.Second))
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()

		msg, err := h(ctx, svc, r)
		if err != nil {
			status, code := fleetErrorStatus(err)
			writeFleetError(w, status, code, err.Error())
			return
		}
		writeFleetJSON(w, http.StatusOK, msg)
	})
}

// fleetService returns the running fleet, or nil when it is disabled.
func (wi *WebInterface) fleetService() *FleetService {
	if wi.bot != nil && wi.bot.fleet != nil {
		return wi.bot.fleet
	}
	return globalFleet
}

// fleetWebActor names the dashboard in the fleet's audit log. The dashboard
// has no users of its own: whoever holds the token acts as the owner.
func (wi *WebInterface) fleetWebActor() fleet.Actor {
	id := "web"
	if wi.bot != nil && wi.bot.fleetNotices != nil && wi.bot.fleetNotices.Owner != 0 {
		id = strconv.FormatInt(wi.bot.fleetNotices.Owner, 10)
	}
	return fleet.Actor{ID: id, Via: "web"}
}

func writeFleetJSON(w http.ResponseWriter, status int, msg proto.Message) {
	body, err := fleetJSON.Marshal(msg)
	if err != nil {
		log.Printf("[web] encode fleet response: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func writeFleetError(w http.ResponseWriter, status int, code, message string) {
	writeFleetJSON(w, status, &alicev1.FleetError{Error: message, Code: code})
}

// errFleetBadRequest marks a request the handler itself found malformed.
var errFleetBadRequest = errors.New("bad request")

func fleetBadRequest(message string) error {
	return &fleetRequestError{message}
}

type fleetRequestError struct{ message string }

func (e *fleetRequestError) Error() string        { return e.message }
func (e *fleetRequestError) Is(target error) bool { return target == errFleetBadRequest }

// fleetErrorStatus maps a fleet error onto an HTTP status and a stable code.
func fleetErrorStatus(err error) (int, string) {
	switch {
	case errors.Is(err, errFleetBadRequest),
		errors.Is(err, fleet.ErrInvalidTask), errors.Is(err, fleet.ErrInvalidProject),
		errors.Is(err, fleet.ErrNotGitRepo), errors.Is(err, fleet.ErrInvalidNotifyMode):
		return http.StatusBadRequest, "invalid"
	case errors.Is(err, fleet.ErrTaskNotFound), errors.Is(err, fleet.ErrProjectNotFound):
		return http.StatusNotFound, "not_found"
	case errors.Is(err, fleet.ErrOverBudget):
		return http.StatusConflict, "over_budget"
	case errors.Is(err, fleet.ErrInvalidTransition), errors.Is(err, fleet.ErrConflict),
		errors.Is(err, fleet.ErrProjectExists), errors.Is(err, fleet.ErrIssueBusy),
		errors.Is(err, fleet.ErrIssueClosed), errors.Is(err, fleet.ErrNothingToApprove),
		errors.Is(err, fleet.ErrNotFastForward):
		return http.StatusConflict, "conflict"
	case errors.Is(err, errTasksNoGitHubRepo):
		return http.StatusConflict, "no_github_repo"
	case errors.Is(err, errTasksGitHubAuthRequired):
		return http.StatusBadGateway, "github_auth"
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout, "timeout"
	}
	return http.StatusInternalServerError, "internal"
}

// readFleetBody decodes a JSON request body into msg. An empty body leaves
// msg as it is.
func readFleetBody(r *http.Request, msg proto.Message) error {
	raw, err := io.ReadAll(io.LimitReader(r.Body, fleetWebMaxBody+1))
	if err != nil {
		return fleetBadRequest("could not read the request body")
	}
	if len(raw) > fleetWebMaxBody {
		return fleetBadRequest("the request body is too large")
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil
	}
	if err := fleetJSONIn.Unmarshal(raw, msg); err != nil {
		return fleetBadRequest("invalid JSON body: " + err.Error())
	}
	return nil
}

// fleetIntParam reads a non-negative integer query parameter.
func fleetIntParam(r *http.Request, name string, fallback, max int) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0, fleetBadRequest(name + " must be a non-negative integer")
	}
	if max > 0 && n > max {
		n = max
	}
	return n, nil
}

func fleetIDParam(r *http.Request, name string) (int64, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 0 {
		return 0, fleetBadRequest(name + " must be a non-negative integer")
	}
	return n, nil
}

// fleetListParam reads a comma-separated query parameter.
func fleetListParam(r *http.Request, name string) []string {
	var out []string
	for _, part := range strings.Split(r.URL.Query().Get(name), ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// ── conversions ──────────────────────────────────────────────────────────────

func fleetTime(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t)
}

func fleetTimePtr(t *time.Time) *timestamppb.Timestamp {
	if t == nil {
		return nil
	}
	return fleetTime(*t)
}

func fleetProjectProto(p fleet.Project, topics []topicBinding) *alicev1.FleetProject {
	out := &alicev1.FleetProject{
		Id:             p.ID,
		Name:           p.Name,
		RepoPath:       p.RepoPath,
		Remote:         p.Remote,
		DefaultBranch:  p.DefaultBranch,
		TrustMode:      string(p.TrustMode),
		VerifyCommands: append([]string{}, p.VerifyCommands...),
		MaxConcurrent:  int32(p.MaxConcurrent),
		BranchPrefix:   p.BranchPrefix,
		Budget: &alicev1.FleetProjectBudget{
			TaskCostUsd:  p.Budget.Task.CostUSD,
			DailyCostUsd: p.Budget.Daily.CostUSD,
		},
		CreatedAt: fleetTime(p.CreatedAt),
		UpdatedAt: fleetTime(p.UpdatedAt),
	}
	for _, b := range topics {
		if b.Dir == p.RepoPath || strings.HasPrefix(b.Dir, p.RepoPath+string(os.PathSeparator)) {
			out.Topics = append(out.Topics, &alicev1.FleetTopic{ChatId: b.Key.chatID, ThreadId: int32(b.Key.threadID)})
		}
	}
	return out
}

func fleetTaskProto(t fleet.Task, projectName string) *alicev1.FleetTask {
	return &alicev1.FleetTask{
		Id:             t.ID,
		ProjectId:      t.ProjectID,
		ProjectName:    projectName,
		Shape:          string(t.Shape),
		Goal:           t.Goal,
		SourceKind:     string(t.Source.Kind),
		IssueNumber:    int32(t.Source.IssueNumber),
		TrustMode:      string(t.TrustMode),
		Model:          t.Model,
		Priority:       int32(t.Priority),
		Status:         string(t.Status),
		StatusReason:   t.StatusReason,
		Liveness:       string(t.Liveness),
		Branch:         t.Branch,
		WorktreePath:   t.WorktreePath,
		SessionId:      t.SessionID,
		PrUrl:          t.PRURL,
		PrNumber:       int32(t.PRNumber),
		InputTokens:    float64(t.InputTokens),
		OutputTokens:   float64(t.OutputTokens),
		CostUsd:        t.CostUSD,
		BudgetCostUsd:  t.Budget.CostUSD,
		CreatedAt:      fleetTime(t.CreatedAt),
		UpdatedAt:      fleetTime(t.UpdatedAt),
		StartedAt:      fleetTimePtr(t.StartedAt),
		FinishedAt:     fleetTimePtr(t.FinishedAt),
		LastActivityAt: fleetTimePtr(t.LastActivityAt),
	}
}

func fleetEventProto(ev fleet.Event, projectID string) *alicev1.FleetEvent {
	return &alicev1.FleetEvent{
		Id:          float64(ev.ID),
		TaskId:      ev.TaskID,
		ProjectId:   projectID,
		Type:        ev.Type,
		PayloadJson: string(ev.Payload),
		CreatedAt:   fleetTime(ev.CreatedAt),
	}
}

func fleetCountsProto(c fleet.DigestCounts) *alicev1.FleetCounts {
	return &alicev1.FleetCounts{
		Queued: int32(c.Queued), Running: int32(c.Running), ReviewReady: int32(c.ReviewReady),
		NeedsDecision: int32(c.NeedsDecision), FailedToday: int32(c.FailedToday), DoneToday: int32(c.DoneToday),
	}
}

func fleetUsageProto(u fleet.Usage) *alicev1.FleetUsage {
	return &alicev1.FleetUsage{InputTokens: float64(u.InputTokens), OutputTokens: float64(u.OutputTokens), CostUsd: u.CostUSD}
}

// fleetProjectNames maps project IDs to names, for labelling tasks.
func fleetProjectNames(ctx context.Context, svc *FleetService) map[string]string {
	names := map[string]string{}
	if projects, err := svc.Store.ListProjects(ctx); err == nil {
		for _, p := range projects {
			names[p.ID] = p.Name
		}
	}
	return names
}

func (wi *WebInterface) fleetTopics() []topicBinding {
	if lister, ok := globalStorage.(topicBindingLister); ok {
		if bindings, err := lister.ListTopicBindings(); err == nil {
			return bindings
		}
	}
	return nil
}

// ── queries ──────────────────────────────────────────────────────────────────

func (wi *WebInterface) fleetDigest(ctx context.Context, svc *FleetService, _ *http.Request) (proto.Message, error) {
	d, err := svc.Commander.Digest(ctx)
	if err != nil {
		return nil, err
	}
	out := &alicev1.FleetDigest{
		GeneratedAt:  fleetTime(d.GeneratedAt),
		DayStart:     fleetTime(d.DayStart),
		Totals:       fleetCountsProto(d.Totals),
		Today:        fleetUsageProto(d.Today),
		PendingWakes: int32(d.PendingWakes),
		NotifyMode:   string(fleet.NotifyNormal),
	}
	if wi.bot != nil {
		out.NotifyMode = string(wi.bot.fleetMode(ctx))
	}
	for _, p := range d.Projects {
		out.Projects = append(out.Projects, &alicev1.FleetProjectDigest{
			ProjectId: p.ProjectID, Name: p.Name, Counts: fleetCountsProto(p.Counts), Today: fleetUsageProto(p.Today),
		})
	}
	return out, nil
}

func (wi *WebInterface) fleetListProjects(ctx context.Context, svc *FleetService, _ *http.Request) (proto.Message, error) {
	projects, err := svc.Commander.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	topics := wi.fleetTopics()
	out := &alicev1.FleetProjectList{}
	for _, p := range projects {
		out.Projects = append(out.Projects, fleetProjectProto(p, topics))
	}
	return out, nil
}

func (wi *WebInterface) fleetGetProject(ctx context.Context, svc *FleetService, r *http.Request) (proto.Message, error) {
	p, err := svc.Commander.GetProject(ctx, r.PathValue("id"))
	if err != nil {
		return nil, err
	}
	return fleetProjectProto(p, wi.fleetTopics()), nil
}

func (wi *WebInterface) fleetListTasks(ctx context.Context, svc *FleetService, r *http.Request) (proto.Message, error) {
	limit, err := fleetIntParam(r, "limit", fleetWebDefaultTasks, fleetWebMaxTasks)
	if err != nil {
		return nil, err
	}
	filter := fleet.TaskFilter{ProjectID: strings.TrimSpace(r.URL.Query().Get("project")), Limit: limit}
	for _, s := range fleetListParam(r, "status") {
		status := fleet.Status(s)
		if !status.Valid() {
			return nil, fleetBadRequest("unknown status " + strconv.Quote(s))
		}
		filter.Statuses = append(filter.Statuses, status)
	}
	tasks, err := svc.Commander.ListTasks(ctx, filter)
	if err != nil {
		return nil, err
	}
	names := fleetProjectNames(ctx, svc)
	out := &alicev1.FleetTaskList{}
	for _, t := range tasks {
		out.Tasks = append(out.Tasks, fleetTaskProto(t, names[t.ProjectID]))
	}
	return out, nil
}

func (wi *WebInterface) fleetTaskReply(ctx context.Context, svc *FleetService, task fleet.Task) *alicev1.FleetTask {
	name := task.ProjectID
	if p, err := svc.Store.GetProject(ctx, task.ProjectID); err == nil {
		name = p.Name
	}
	return fleetTaskProto(task, name)
}

func (wi *WebInterface) fleetGetTask(ctx context.Context, svc *FleetService, r *http.Request) (proto.Message, error) {
	task, err := svc.Commander.GetTask(ctx, r.PathValue("id"))
	if err != nil {
		return nil, err
	}
	return wi.fleetTaskReply(ctx, svc, task), nil
}

// fleetEventPage reads events after a cursor, optionally of some types only.
// Filtering happens after the page is read, so a page may hold fewer events
// than the limit while more remain; has_more says so.
func fleetEventPage(ctx context.Context, svc *FleetService, r *http.Request, taskID string) (*alicev1.FleetEventList, error) {
	after, err := fleetIDParam(r, "after_id")
	if err != nil {
		return nil, err
	}
	limit, err := fleetIntParam(r, "limit", fleetWebDefaultEvents, fleetWebMaxEvents)
	if err != nil {
		return nil, err
	}
	if limit == 0 {
		limit = fleetWebDefaultEvents
	}
	wanted := map[string]bool{}
	for _, t := range fleetListParam(r, "types") {
		if t == "lifecycle" {
			for _, lt := range fleet.LifecycleEvents {
				wanted[lt] = true
			}
			continue
		}
		wanted[t] = true
	}
	// One more than asked for, to learn whether anything follows.
	events, err := svc.Store.ListEvents(ctx, taskID, after, limit+1)
	if err != nil {
		return nil, err
	}
	out := &alicev1.FleetEventList{NextAfterId: float64(after)}
	if len(events) > limit {
		events, out.HasMore = events[:limit], true
	}
	projectOf := map[string]string{}
	for _, ev := range events {
		out.NextAfterId = float64(ev.ID)
		if len(wanted) > 0 && !wanted[ev.Type] {
			continue
		}
		project, ok := projectOf[ev.TaskID]
		if !ok && ev.TaskID != "" {
			if task, err := svc.Store.GetTask(ctx, ev.TaskID); err == nil {
				project = task.ProjectID
			}
			projectOf[ev.TaskID] = project
		}
		out.Events = append(out.Events, fleetEventProto(ev, project))
	}
	return out, nil
}

func (wi *WebInterface) fleetTaskEvents(ctx context.Context, svc *FleetService, r *http.Request) (proto.Message, error) {
	id := r.PathValue("id")
	task, err := svc.Commander.GetTask(ctx, id)
	if err != nil {
		return nil, err
	}
	// last_of=<type> asks only for the task's most recent event of a type,
	// e.g. the last thing its agent said, however long its history is.
	if kind := strings.TrimSpace(r.URL.Query().Get("last_of")); kind != "" {
		out := &alicev1.FleetEventList{}
		ev, ok, err := svc.Store.LastEvent(ctx, id, kind)
		if err != nil {
			return nil, err
		}
		if ok {
			out.Events = append(out.Events, fleetEventProto(ev, task.ProjectID))
			out.NextAfterId = float64(ev.ID)
		}
		return out, nil
	}
	return fleetEventPage(ctx, svc, r, id)
}

func (wi *WebInterface) fleetEvents(ctx context.Context, svc *FleetService, r *http.Request) (proto.Message, error) {
	return fleetEventPage(ctx, svc, r, "")
}

func (wi *WebInterface) fleetTaskDiff(ctx context.Context, svc *FleetService, r *http.Request) (proto.Message, error) {
	diff, err := svc.Commander.TaskDiff(ctx, r.PathValue("id"))
	if errors.Is(err, fleet.ErrNoWorkspace) {
		// A task that has not started has no differences to show.
		return &alicev1.FleetDiff{}, nil
	}
	if err != nil {
		return nil, err
	}
	return &alicev1.FleetDiff{
		Base: diff.Base, Stat: diff.Stat, Patch: diff.Patch,
		Untracked: append([]string{}, diff.Untracked...), Truncated: diff.Truncated,
	}, nil
}

func (wi *WebInterface) fleetTaskVerifications(ctx context.Context, svc *FleetService, r *http.Request) (proto.Message, error) {
	id := r.PathValue("id")
	if _, err := svc.Commander.GetTask(ctx, id); err != nil {
		return nil, err
	}
	checks, err := svc.Commander.Verifications(ctx, id)
	if err != nil {
		return nil, err
	}
	out := &alicev1.FleetVerificationList{}
	for _, c := range checks {
		out.Verifications = append(out.Verifications, &alicev1.FleetVerification{
			Id: float64(c.ID), TaskId: c.TaskID, Attempt: int32(c.Attempt), StepIndex: int32(c.StepIndex),
			Name: c.Name, Command: c.Command, Passed: c.Passed, ExitCode: int32(c.ExitCode),
			OutputTail: c.OutputTail, DurationMs: float64(c.Duration.Milliseconds()), TimedOut: c.TimedOut,
			Error: c.Err, CreatedAt: fleetTime(c.CreatedAt),
		})
	}
	return out, nil
}

func (wi *WebInterface) fleetAudit(ctx context.Context, svc *FleetService, r *http.Request) (proto.Message, error) {
	limit, err := fleetIntParam(r, "limit", 100, 500)
	if err != nil {
		return nil, err
	}
	before, err := fleetIDParam(r, "before_id")
	if err != nil {
		return nil, err
	}
	q := r.URL.Query()
	entries, err := svc.Commander.Audit(ctx, fleet.AuditFilter{
		ProjectID: strings.TrimSpace(q.Get("project")),
		TaskID:    strings.TrimSpace(q.Get("task")),
		Actor:     strings.TrimSpace(q.Get("actor")),
		BeforeID:  before,
		Limit:     limit,
	})
	if err != nil {
		return nil, err
	}
	out := &alicev1.FleetAuditList{}
	for _, e := range entries {
		out.Entries = append(out.Entries, &alicev1.FleetAuditEntry{
			Id: float64(e.ID), Actor: e.Actor, Via: e.Via, Command: e.Command, ProjectId: e.ProjectID,
			TaskId: e.TaskID, Detail: e.Detail, Ok: e.OK, Error: e.Error, CreatedAt: fleetTime(e.CreatedAt),
		})
	}
	return out, nil
}

// ── commands ─────────────────────────────────────────────────────────────────

func (wi *WebInterface) fleetRegisterProject(ctx context.Context, svc *FleetService, r *http.Request) (proto.Message, error) {
	var req alicev1.FleetProjectRequest
	if err := readFleetBody(r, &req); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.RepoPath) == "" {
		return nil, fleetBadRequest("repo_path is required")
	}
	mode := svc.TrustMode
	if req.TrustMode != "" {
		var err error
		if mode, err = fleet.ParseTrustMode(req.TrustMode); err != nil {
			return nil, err
		}
	}
	p := fleet.Project{
		RepoPath:       strings.TrimSpace(req.RepoPath),
		Name:           strings.TrimSpace(req.Name),
		TrustMode:      mode,
		VerifyCommands: fleetCleanCommands(req.VerifyCommands),
		MaxConcurrent:  int(req.MaxConcurrent),
	}
	if req.Budget != nil {
		p.Budget.Task.CostUSD, p.Budget.Daily.CostUSD = req.Budget.TaskCostUsd, req.Budget.DailyCostUsd
	}
	out, err := svc.Commander.RegisterProject(ctx, wi.fleetWebActor(), p)
	if err != nil {
		return nil, err
	}
	return fleetProjectProto(out, wi.fleetTopics()), nil
}

func (wi *WebInterface) fleetUpdateProject(ctx context.Context, svc *FleetService, r *http.Request) (proto.Message, error) {
	var req alicev1.FleetProjectRequest
	if err := readFleetBody(r, &req); err != nil {
		return nil, err
	}
	mode, err := fleet.ParseTrustMode(req.TrustMode)
	if err != nil {
		return nil, err
	}
	if req.MaxConcurrent < 1 {
		return nil, fleetBadRequest("max_concurrent must be at least 1")
	}
	if req.Budget != nil && (req.Budget.TaskCostUsd < 0 || req.Budget.DailyCostUsd < 0) {
		return nil, fleetBadRequest("a budget cannot be negative")
	}
	out, err := svc.Commander.UpdateProject(ctx, wi.fleetWebActor(), r.PathValue("id"), "settings edited on the dashboard",
		func(p *fleet.Project) error {
			if name := strings.TrimSpace(req.Name); name != "" {
				p.Name = name
			}
			p.TrustMode = mode
			p.VerifyCommands = fleetCleanCommands(req.VerifyCommands)
			p.MaxConcurrent = int(req.MaxConcurrent)
			if req.Budget != nil {
				p.Budget.Task.CostUSD, p.Budget.Daily.CostUSD = req.Budget.TaskCostUsd, req.Budget.DailyCostUsd
			}
			return nil
		})
	if err != nil {
		return nil, err
	}
	return fleetProjectProto(out, wi.fleetTopics()), nil
}

// fleetCleanCommands drops blank lines from a list of verify commands.
func fleetCleanCommands(in []string) []string {
	out := []string{}
	for _, c := range in {
		if c = strings.TrimSpace(c); c != "" {
			out = append(out, c)
		}
	}
	return out
}

func (wi *WebInterface) fleetDispatchTask(ctx context.Context, svc *FleetService, r *http.Request) (proto.Message, error) {
	var req alicev1.FleetDispatchRequest
	if err := readFleetBody(r, &req); err != nil {
		return nil, err
	}
	shape := fleet.Shape(strings.TrimSpace(req.Shape))
	if shape == "" {
		shape = fleet.ShapeShip
	}
	switch {
	case strings.TrimSpace(req.ProjectId) == "":
		return nil, fleetBadRequest("project_id is required")
	case !shape.Valid():
		return nil, fleetBadRequest("shape must be ship or scout")
	case req.Issue < 0 || req.BudgetUsd < 0:
		return nil, fleetBadRequest("issue and budget_usd cannot be negative")
	case req.Issue == 0 && strings.TrimSpace(req.Goal) == "":
		return nil, fleetBadRequest("a goal or an issue is required")
	}
	task, err := svc.Commander.Dispatch(ctx, wi.fleetWebActor(), fleet.DispatchRequest{
		ProjectID: strings.TrimSpace(req.ProjectId),
		Goal:      strings.TrimSpace(req.Goal),
		Issue:     int(req.Issue),
		Shape:     shape,
		Model:     strings.TrimSpace(req.Model),
		Priority:  int(req.Priority),
		Budget:    fleet.Limit{CostUSD: req.BudgetUsd},
	})
	if err != nil {
		return nil, err
	}
	return wi.fleetTaskReply(ctx, svc, task), nil
}

// fleetTaskAction handles POST /api/fleet/tasks/{id}/{action}.
func (wi *WebInterface) fleetTaskAction(ctx context.Context, svc *FleetService, r *http.Request) (proto.Message, error) {
	id, action := r.PathValue("id"), r.PathValue("action")
	actor, cmd := wi.fleetWebActor(), svc.Commander
	var (
		task fleet.Task
		err  error
	)
	switch action {
	case "cancel":
		var req alicev1.FleetCancelRequest
		if err := readFleetBody(r, &req); err != nil {
			return nil, err
		}
		task, err = cmd.Cancel(ctx, actor, id, strings.TrimSpace(req.Reason))
	case "retry":
		task, err = cmd.Retry(ctx, actor, id)
	case "reply":
		var req alicev1.FleetReplyRequest
		if err := readFleetBody(r, &req); err != nil {
			return nil, err
		}
		task, err = cmd.Reply(ctx, actor, fleet.ReplyRequest{TaskID: id, Text: strings.TrimSpace(req.Text), IgnoreBudget: req.IgnoreBudget})
	case "approve":
		task, err = cmd.Approve(ctx, actor, id)
	case "merge":
		task, err = cmd.Merge(ctx, actor, id)
	case "pass":
		task, err = cmd.Pass(ctx, actor, id)
	case "discard":
		task, err = cmd.Discard(ctx, actor, id)
	default:
		return nil, fleetBadRequest("unknown action " + strconv.Quote(action))
	}
	if err != nil {
		return nil, err
	}
	// Reply returns the task as it was when the run was accepted; read it
	// again so the caller sees where it stands now.
	if fresh, ferr := cmd.GetTask(ctx, id); ferr == nil {
		task = fresh
	}
	return wi.fleetTaskReply(ctx, svc, task), nil
}

// ── WebSocket ────────────────────────────────────────────────────────────────

// fleetEventWSData is a fleet event as the WebSocket carries it: the same
// JSON as the REST endpoints return.
func fleetEventWSData(ev fleet.Event, projectID string) json.RawMessage {
	body, err := fleetJSON.Marshal(fleetEventProto(ev, projectID))
	if err != nil {
		return json.RawMessage("null")
	}
	return body
}

// fleetReplayLimit caps how many missed events one reconnect is sent. A
// client further behind than this reloads its lists instead.
const fleetReplayLimit = 2000

// replayFleetEvents sends a newly connected client the lifecycle events it
// missed, oldest first, so it can carry on from the last event ID it saw.
func replayFleetEvents(ctx context.Context, svc *FleetService, afterID int64, send func([]byte) bool) {
	wanted := map[string]bool{}
	for _, t := range fleet.LifecycleEvents {
		wanted[t] = true
	}
	projectOf := map[string]string{}
	for sent := 0; sent < fleetReplayLimit; {
		events, err := svc.Store.ListEvents(ctx, "", afterID, 500)
		if err != nil || len(events) == 0 {
			return
		}
		for _, ev := range events {
			afterID = ev.ID
			if !wanted[ev.Type] {
				continue
			}
			project, ok := projectOf[ev.TaskID]
			if !ok && ev.TaskID != "" {
				if task, err := svc.Store.GetTask(ctx, ev.TaskID); err == nil {
					project = task.ProjectID
				}
				projectOf[ev.TaskID] = project
			}
			body, err := json.Marshal(WebSocketEvent{
				Type: fleetEventPrefix + ev.Type, Timestamp: ev.CreatedAt, Data: fleetEventWSData(ev, project),
			})
			if err != nil || !send(body) {
				return
			}
			sent++
		}
	}
}
