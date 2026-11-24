package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chimerakang/alice-core/fleet"
)

// webFleetEnv serves the real router over a fleet that is built but not
// started, with the command token set to "secret".
type webFleetEnv struct {
	*notifyEnv
	handler http.Handler
}

func newWebFleetEnv(t *testing.T) *webFleetEnv {
	t.Helper()
	e := newNotifyTestEnv(t)
	wi := &WebInterface{bot: e.bot, apiToken: "secret"}
	return &webFleetEnv{e, wi.CreateRouter()}
}

// call makes a request and decodes the JSON reply. With token true it sends
// the command token.
func (e *webFleetEnv) call(method, path, body string, token bool) (int, map[string]any) {
	e.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token {
		req.Header.Set("Authorization", "Bearer secret")
	}
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		e.t.Fatalf("%s %s: status %d, body is not JSON: %s", method, path, rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		e.t.Errorf("%s %s: Content-Type = %q", method, path, ct)
	}
	return rec.Code, out
}

func (e *webFleetEnv) get(path string) map[string]any {
	e.t.Helper()
	status, out := e.call(http.MethodGet, path, "", false)
	if status != http.StatusOK {
		e.t.Fatalf("GET %s: status %d: %v", path, status, out)
	}
	return out
}

func list(t *testing.T, m map[string]any, key string) []map[string]any {
	t.Helper()
	raw, _ := m[key].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		obj, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("%s holds a %T, want objects", key, item)
		}
		out = append(out, obj)
	}
	return out
}

func TestFleetAPIDisabled(t *testing.T) {
	prev := globalFleet
	globalFleet = nil
	defer func() { globalFleet = prev }()
	wi := &WebInterface{}
	handler := wi.CreateRouter()
	for _, path := range []string{"/api/fleet/digest", "/api/fleet/projects", "/api/fleet/tasks", "/api/fleet/events", "/api/fleet/audit"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if rec.Code != http.StatusServiceUnavailable || body["code"] != "disabled" {
			t.Errorf("GET %s with the fleet off: %d %v", path, rec.Code, body)
		}
	}
}

func TestFleetAPIDigestAndProjects(t *testing.T) {
	e := newWebFleetEnv(t)
	running := e.dispatch("/ship one")
	e.walk(running.ID, "slot free", fleet.StatusRunning)
	e.dispatch("/ship two")

	digest := e.get("/api/fleet/digest")
	totals, _ := digest["totals"].(map[string]any)
	if totals["running"] != float64(1) || totals["queued"] != float64(1) || totals["review_ready"] != float64(0) {
		t.Fatalf("totals = %v", totals)
	}
	if digest["notify_mode"] != "normal" {
		t.Errorf("notify_mode = %v", digest["notify_mode"])
	}
	if projects := list(t, digest, "projects"); len(projects) != 1 || projects[0]["project_id"] != e.project.ID {
		t.Fatalf("digest projects = %v", projects)
	}

	projects := list(t, e.get("/api/fleet/projects"), "projects")
	if len(projects) != 1 {
		t.Fatalf("projects = %v", projects)
	}
	p := projects[0]
	if p["id"] != e.project.ID || p["repo_path"] != e.project.RepoPath || p["trust_mode"] != "local-only" || p["max_concurrent"] != float64(1) {
		t.Fatalf("project = %v", p)
	}
	if _, ok := p["verify_commands"].([]any); !ok {
		t.Errorf("verify_commands = %#v, want an array even when empty", p["verify_commands"])
	}
	// The topic the project was bound from is listed with it.
	topics := list(t, p, "topics")
	if len(topics) != 1 || topics[0]["thread_id"] != float64(e.key.threadID) {
		t.Fatalf("topics = %v", topics)
	}

	one := e.get("/api/fleet/projects/" + e.project.ID)
	if one["id"] != e.project.ID {
		t.Fatalf("project by id = %v", one)
	}
	if status, body := e.call(http.MethodGet, "/api/fleet/projects/nope", "", false); status != http.StatusNotFound || body["code"] != "not_found" {
		t.Fatalf("unknown project: %d %v", status, body)
	}
}

func TestFleetAPITasks(t *testing.T) {
	e := newWebFleetEnv(t)
	first := e.dispatch("/ship one --budget 3 --model opus")
	second := e.dispatch("/scout two")
	e.walk(second.ID, "agent crashed", fleet.StatusRunning, fleet.StatusFailed)

	tasks := list(t, e.get("/api/fleet/tasks"), "tasks")
	if len(tasks) != 2 {
		t.Fatalf("tasks = %d", len(tasks))
	}
	got := e.get("/api/fleet/tasks/" + first.ID)
	for key, want := range map[string]any{
		"id": first.ID, "project_id": e.project.ID, "project_name": e.project.Name, "shape": "ship",
		"goal": "one", "status": "queued", "liveness": "unknown", "model": "opus", "budget_cost_usd": float64(3),
		"source_kind": "manual", "trust_mode": "local-only",
	} {
		if got[key] != want {
			t.Errorf("task.%s = %v, want %v", key, got[key], want)
		}
	}
	if _, err := time.Parse(time.RFC3339Nano, got["created_at"].(string)); err != nil {
		t.Errorf("created_at = %v: %v", got["created_at"], err)
	}
	if got["started_at"] != nil {
		t.Errorf("started_at of a queued task = %v, want null", got["started_at"])
	}

	// Filters.
	if failed := list(t, e.get("/api/fleet/tasks?status=failed"), "tasks"); len(failed) != 1 || failed[0]["id"] != second.ID {
		t.Fatalf("status=failed: %v", failed)
	}
	if both := list(t, e.get("/api/fleet/tasks?status=failed,queued&project="+e.project.ID), "tasks"); len(both) != 2 {
		t.Fatalf("status=failed,queued: %d", len(both))
	}
	if none := list(t, e.get("/api/fleet/tasks?project=other"), "tasks"); len(none) != 0 {
		t.Fatalf("project=other: %v", none)
	}
	if limited := list(t, e.get("/api/fleet/tasks?limit=1"), "tasks"); len(limited) != 1 {
		t.Fatalf("limit=1: %d", len(limited))
	}
	for _, bad := range []string{"/api/fleet/tasks?status=bogus", "/api/fleet/tasks?limit=-1", "/api/fleet/tasks?limit=x"} {
		if status, body := e.call(http.MethodGet, bad, "", false); status != http.StatusBadRequest || body["code"] != "invalid" {
			t.Errorf("GET %s: %d %v", bad, status, body)
		}
	}
	if status, body := e.call(http.MethodGet, "/api/fleet/tasks/nosuchtask00", "", false); status != http.StatusNotFound || body["code"] != "not_found" {
		t.Fatalf("unknown task: %d %v", status, body)
	}
}

func TestFleetAPIEventsResumeFromACursor(t *testing.T) {
	e := newWebFleetEnv(t)
	ctx := context.Background()
	task := e.dispatch("/ship one")
	e.walk(task.ID, "slot free", fleet.StatusRunning)
	if _, err := e.svc.Store.AppendEvent(ctx, task.ID, fleet.EventAgentToolUse, map[string]string{"tool": "Read"}); err != nil {
		t.Fatal(err)
	}
	e.walk(task.ID, "agent finished", fleet.StatusVerifying)

	all := e.get("/api/fleet/tasks/" + task.ID + "/events")
	events := list(t, all, "events")
	if len(events) != 4 || all["has_more"] != false {
		t.Fatalf("events = %d, has_more = %v", len(events), all["has_more"])
	}
	if events[2]["type"] != "agent.tool_use" || events[2]["payload_json"] != `{"tool":"Read"}` || events[2]["project_id"] != e.project.ID {
		t.Fatalf("tool event = %v", events[2])
	}

	// Page by page, nothing is skipped or repeated.
	var seen []float64
	after := "0"
	for page := 0; page < 10; page++ {
		got := e.get("/api/fleet/tasks/" + task.ID + "/events?limit=3&after_id=" + after)
		for _, ev := range list(t, got, "events") {
			seen = append(seen, ev["id"].(float64))
		}
		after = strings.TrimSuffix(strings.TrimSuffix(jsonNumber(got["next_after_id"]), ".0"), ".")
		if got["has_more"] != true {
			break
		}
	}
	if len(seen) != 4 {
		t.Fatalf("paged events = %v, want 4", seen)
	}
	for i := 1; i < len(seen); i++ {
		if seen[i] <= seen[i-1] {
			t.Fatalf("paged events out of order: %v", seen)
		}
	}

	// Only lifecycle events: the tool call is left out but still counted
	// past, so the cursor does not stall on it.
	life := e.get("/api/fleet/events?types=lifecycle")
	for _, ev := range list(t, life, "events") {
		if ev["type"] != "status" && ev["type"] != "liveness" && ev["type"] != "verify.result" && ev["type"] != "wake" {
			t.Errorf("lifecycle filter let %v through", ev["type"])
		}
	}
	if life["next_after_id"].(float64) != seen[len(seen)-1] {
		t.Errorf("next_after_id = %v, want the last event %v", life["next_after_id"], seen[len(seen)-1])
	}
	if status, body := e.call(http.MethodGet, "/api/fleet/tasks/nosuchtask00/events", "", false); status != http.StatusNotFound {
		t.Fatalf("events of an unknown task: %d %v", status, body)
	}
}

func jsonNumber(v any) string {
	raw, _ := json.Marshal(v)
	return string(raw)
}

func TestFleetAPIDiffAndVerifications(t *testing.T) {
	e := newWebFleetEnv(t)
	task := e.dispatch("/ship one")

	// Not started: no workspace, so an empty diff rather than an error.
	diff := e.get("/api/fleet/tasks/" + task.ID + "/diff")
	if diff["patch"] != "" || diff["truncated"] != false {
		t.Fatalf("diff of a queued task = %v", diff)
	}
	checks := e.get("/api/fleet/tasks/" + task.ID + "/verifications")
	if got := list(t, checks, "verifications"); len(got) != 0 {
		t.Fatalf("verifications = %v", got)
	}
	for _, path := range []string{"/diff", "/verifications"} {
		if status, _ := e.call(http.MethodGet, "/api/fleet/tasks/nosuchtask00"+path, "", false); status != http.StatusNotFound {
			t.Errorf("%s of an unknown task: %d", path, status)
		}
	}
}

func TestFleetAPICommandsNeedTheToken(t *testing.T) {
	e := newWebFleetEnv(t)
	task := e.dispatch("/ship one")
	commands := []struct{ method, path, body string }{
		{http.MethodPost, "/api/fleet/tasks", `{"project_id":"` + e.project.ID + `","goal":"x"}`},
		{http.MethodPost, "/api/fleet/tasks/" + task.ID + "/cancel", ""},
		{http.MethodPost, "/api/fleet/tasks/" + task.ID + "/retry", ""},
		{http.MethodPost, "/api/fleet/tasks/" + task.ID + "/reply", `{"text":"x"}`},
		{http.MethodPost, "/api/fleet/tasks/" + task.ID + "/approve", ""},
		{http.MethodPost, "/api/fleet/tasks/" + task.ID + "/merge", ""},
		{http.MethodPost, "/api/fleet/tasks/" + task.ID + "/discard", ""},
		{http.MethodPost, "/api/fleet/tasks/" + task.ID + "/pass", ""},
		{http.MethodPost, "/api/fleet/projects", `{"repo_path":"/tmp"}`},
		{http.MethodPut, "/api/fleet/projects/" + e.project.ID, `{"trust_mode":"local-only","max_concurrent":1}`},
	}
	for _, c := range commands {
		status, body := e.call(c.method, c.path, c.body, false)
		if status != http.StatusUnauthorized || body["code"] != "unauthorized" {
			t.Errorf("%s %s without the token: %d %v", c.method, c.path, status, body)
		}
	}
	if tasks, _ := e.svc.Store.ListTasks(context.Background(), fleet.TaskFilter{}); len(tasks) != 1 || tasks[0].Status != fleet.StatusQueued {
		t.Fatalf("commands without the token changed something: %+v", tasks)
	}
}

func TestFleetAPIDispatch(t *testing.T) {
	e := newWebFleetEnv(t)
	status, task := e.call(http.MethodPost, "/api/fleet/tasks",
		`{"project_id":"`+e.project.ID+`","goal":"  修好登入測試  ","shape":"scout","model":"opus","budget_usd":2.5,"priority":3}`, true)
	if status != http.StatusOK {
		t.Fatalf("dispatch: %d %v", status, task)
	}
	for key, want := range map[string]any{
		"goal": "修好登入測試", "shape": "scout", "model": "opus", "budget_cost_usd": 2.5,
		"priority": float64(3), "status": "queued", "project_name": e.project.Name,
	} {
		if task[key] != want {
			t.Errorf("task.%s = %v, want %v", key, task[key], want)
		}
	}
	// It is in the audit log as a command from the web.
	audit := list(t, e.get("/api/fleet/audit?task="+task["id"].(string)), "entries")
	if len(audit) != 1 || audit[0]["command"] != "dispatch" || audit[0]["via"] != "web" || audit[0]["ok"] != true || audit[0]["actor"] != "42" {
		t.Fatalf("audit = %v", audit)
	}

	bad := []struct{ body, code string }{
		{`{"goal":"x"}`, "invalid"},
		{`{"project_id":"` + e.project.ID + `"}`, "invalid"},
		{`{"project_id":"` + e.project.ID + `","goal":"x","shape":"yolo"}`, "invalid"},
		{`{"project_id":"` + e.project.ID + `","goal":"x","budget_usd":-1}`, "invalid"},
		{`{"project_id":"` + e.project.ID + `","goal":"x","surprise":1}`, "invalid"},
		{`not json`, "invalid"},
		{`{"project_id":"nope","goal":"x"}`, "not_found"},
	}
	for _, c := range bad {
		status, body := e.call(http.MethodPost, "/api/fleet/tasks", c.body, true)
		if body["code"] != c.code || status < 400 {
			t.Errorf("dispatch %s: %d %v, want code %s", c.body, status, body, c.code)
		}
	}
	if tasks, _ := e.svc.Store.ListTasks(context.Background(), fleet.TaskFilter{}); len(tasks) != 1 {
		t.Fatalf("refused dispatches created tasks: %d", len(tasks))
	}
}

func TestFleetAPITaskActions(t *testing.T) {
	e := newWebFleetEnv(t)
	ctx := context.Background()
	post := func(id, action, body string) (int, map[string]any) {
		return e.call(http.MethodPost, "/api/fleet/tasks/"+id+"/"+action, body, true)
	}

	task := e.dispatch("/ship one")
	if status, got := post(task.ID, "cancel", `{"reason":"changed my mind"}`); status != http.StatusOK || got["status"] != "cancelled" {
		t.Fatalf("cancel: %d %v", status, got)
	}
	// A finished task cannot be cancelled again.
	if status, got := post(task.ID, "cancel", ""); status != http.StatusConflict || got["code"] != "conflict" {
		t.Fatalf("cancel twice: %d %v", status, got)
	}

	failed := e.dispatch("/ship two")
	e.walk(failed.ID, "agent crashed", fleet.StatusRunning, fleet.StatusFailed)
	if status, got := post(failed.ID, "retry", ""); status != http.StatusOK || got["status"] != "queued" {
		t.Fatalf("retry: %d %v", status, got)
	}

	blocked := e.dispatch("/scout three")
	e.walk(blocked.ID, "checks kept failing", fleet.StatusRunning, fleet.StatusNeedsDecision)
	if status, got := post(blocked.ID, "pass", ""); status != http.StatusOK || got["status"] != "review_ready" {
		t.Fatalf("pass: %d %v", status, got)
	}

	// Approving something that is not awaiting approval is a conflict.
	if status, got := post(failed.ID, "approve", ""); status != http.StatusConflict {
		t.Fatalf("approve a queued task: %d %v", status, got)
	}
	if status, got := post(failed.ID, "merge", ""); status != http.StatusConflict {
		t.Fatalf("merge a queued task: %d %v", status, got)
	}

	// A reply is recorded against the task with its text.
	waiting := e.dispatch("/ship four")
	e.walk(waiting.ID, "agent is waiting for an answer", fleet.StatusRunning, fleet.StatusNeedsDecision)
	if status, got := post(waiting.ID, "reply", `{"text":"用方案 B"}`); status != http.StatusOK {
		t.Fatalf("reply: %d %v", status, got)
	}
	audit, _ := e.svc.Commander.Audit(ctx, fleet.AuditFilter{TaskID: waiting.ID})
	if len(audit) == 0 || audit[0].Command != fleet.CmdReply || audit[0].Detail != "用方案 B" || audit[0].Via != "web" {
		t.Fatalf("reply audit = %+v", audit)
	}

	if status, got := post(task.ID, "discard", ""); status != http.StatusOK {
		t.Fatalf("discard: %d %v", status, got)
	}
	if status, got := post(task.ID, "explode", ""); status != http.StatusBadRequest || got["code"] != "invalid" {
		t.Fatalf("unknown action: %d %v", status, got)
	}
	if status, got := post("nosuchtask00", "cancel", ""); status != http.StatusNotFound {
		t.Fatalf("cancel an unknown task: %d %v", status, got)
	}
	if status, got := post(failed.ID, "reply", `{"txt":"typo"}`); status != http.StatusBadRequest {
		t.Fatalf("reply with an unknown field: %d %v", status, got)
	}
}

func TestFleetAPIProjectCommands(t *testing.T) {
	e := newWebFleetEnv(t)
	repo := newFleetTestRepo(t)

	status, created := e.call(http.MethodPost, "/api/fleet/projects",
		`{"repo_path":"`+repo+`","verify_commands":["go build ./...","  ","go test ./..."],"max_concurrent":2}`, true)
	if status != http.StatusOK {
		t.Fatalf("register: %d %v", status, created)
	}
	cmds, _ := created["verify_commands"].([]any)
	if len(cmds) != 2 || created["max_concurrent"] != float64(2) || created["trust_mode"] != "local-only" {
		t.Fatalf("registered project = %v", created)
	}
	id := created["id"].(string)

	// Registering the same repository again is a conflict; a path that is
	// not a repository is invalid.
	if status, body := e.call(http.MethodPost, "/api/fleet/projects", `{"repo_path":"`+repo+`"}`, true); status != http.StatusConflict {
		t.Fatalf("register twice: %d %v", status, body)
	}
	if status, body := e.call(http.MethodPost, "/api/fleet/projects", `{"repo_path":"`+t.TempDir()+`"}`, true); status != http.StatusBadRequest {
		t.Fatalf("register a plain directory: %d %v", status, body)
	}
	if status, body := e.call(http.MethodPost, "/api/fleet/projects", `{}`, true); status != http.StatusBadRequest {
		t.Fatalf("register nothing: %d %v", status, body)
	}

	status, updated := e.call(http.MethodPut, "/api/fleet/projects/"+id,
		`{"name":"renamed","trust_mode":"local-only","verify_commands":["make test"],"max_concurrent":3,"budget":{"task_cost_usd":4,"daily_cost_usd":20}}`, true)
	if status != http.StatusOK {
		t.Fatalf("update: %d %v", status, updated)
	}
	budget, _ := updated["budget"].(map[string]any)
	if updated["name"] != "renamed" || updated["max_concurrent"] != float64(3) || budget["task_cost_usd"] != float64(4) || budget["daily_cost_usd"] != float64(20) {
		t.Fatalf("updated project = %v", updated)
	}

	bad := []string{
		`{"trust_mode":"yolo","max_concurrent":1}`,
		`{"trust_mode":"local-only","max_concurrent":0}`,
		`{"trust_mode":"local-only","max_concurrent":1,"budget":{"task_cost_usd":-1}}`,
		// The test repository has no remote, so a mode that pushes is refused.
		`{"trust_mode":"direct-pr","max_concurrent":1}`,
	}
	for _, body := range bad {
		if status, got := e.call(http.MethodPut, "/api/fleet/projects/"+id, body, true); status != http.StatusBadRequest {
			t.Errorf("update %s: %d %v", body, status, got)
		}
	}
	if status, got := e.call(http.MethodPut, "/api/fleet/projects/nope", `{"trust_mode":"local-only","max_concurrent":1}`, true); status != http.StatusNotFound {
		t.Fatalf("update an unknown project: %d %v", status, got)
	}
	// Refused updates changed nothing.
	if after := e.get("/api/fleet/projects/" + id); after["max_concurrent"] != float64(3) || after["trust_mode"] != "local-only" {
		t.Fatalf("project after refused updates = %v", after)
	}
}

func TestFleetAPIAuditFilters(t *testing.T) {
	e := newWebFleetEnv(t)
	one := e.dispatch("/ship one")
	e.dispatch("/ship two")
	e.call(http.MethodPost, "/api/fleet/tasks/"+one.ID+"/cancel", "", true)
	e.call(http.MethodPost, "/api/fleet/tasks/"+one.ID+"/approve", "", true) // refused, and recorded as such

	all := list(t, e.get("/api/fleet/audit"), "entries")
	if len(all) < 4 {
		t.Fatalf("audit entries = %d, want at least 4", len(all))
	}
	mine := list(t, e.get("/api/fleet/audit?task="+one.ID), "entries")
	if len(mine) != 3 {
		t.Fatalf("audit for the task = %d entries, want dispatch, cancel and the refused approve", len(mine))
	}
	// Newest first: the refused approve, with why.
	if mine[0]["command"] != "approve" || mine[0]["ok"] != false || mine[0]["error"] == "" || mine[0]["via"] != "web" {
		t.Fatalf("refused approve = %v", mine[0])
	}
	if mine[2]["command"] != "dispatch" || mine[2]["via"] != "telegram" {
		t.Fatalf("dispatch entry = %v", mine[2])
	}
	if byProject := list(t, e.get("/api/fleet/audit?project="+e.project.ID+"&limit=2"), "entries"); len(byProject) != 2 {
		t.Fatalf("project filter with limit=2: %d", len(byProject))
	}
	if none := list(t, e.get("/api/fleet/audit?project=other"), "entries"); len(none) != 0 {
		t.Fatalf("other project: %v", none)
	}
}

func TestFleetWebSocketReplay(t *testing.T) {
	e := newWebFleetEnv(t)
	ctx := context.Background()
	task := e.dispatch("/ship one")
	e.walk(task.ID, "slot free", fleet.StatusRunning)
	if _, err := e.svc.Store.AppendEvent(ctx, task.ID, fleet.EventAgentToolUse, map[string]string{"tool": "Read"}); err != nil {
		t.Fatal(err)
	}
	e.walk(task.ID, "agent crashed", fleet.StatusFailed)

	collect := func(afterID int64) []map[string]any {
		var out []map[string]any
		replayFleetEvents(ctx, e.svc, afterID, func(body []byte) bool {
			var msg map[string]any
			if err := json.Unmarshal(body, &msg); err != nil {
				t.Fatalf("replayed message is not JSON: %s", body)
			}
			out = append(out, msg)
			return true
		})
		return out
	}

	all := collect(0)
	if len(all) != 3 {
		t.Fatalf("replayed %d messages, want the three status changes", len(all))
	}
	var lastID float64
	for _, msg := range all {
		data, _ := msg["data"].(map[string]any)
		if msg["type"] != "fleet.status" || data["task_id"] != task.ID || data["project_id"] != e.project.ID {
			t.Fatalf("replayed message = %v", msg)
		}
		if id := data["id"].(float64); id <= lastID {
			t.Fatalf("replay out of order at %v", id)
		} else {
			lastID = id
		}
	}
	// From the last one seen, nothing is repeated.
	firstID := all[0]["data"].(map[string]any)["id"].(float64)
	if rest := collect(int64(firstID)); len(rest) != 2 {
		t.Fatalf("resume after the first event replayed %d, want 2", len(rest))
	}
	if rest := collect(int64(lastID)); len(rest) != 0 {
		t.Fatalf("resume after the last event replayed %d", len(rest))
	}
	// A client that goes away stops the replay.
	n := 0
	replayFleetEvents(ctx, e.svc, 0, func([]byte) bool { n++; return false })
	if n != 1 {
		t.Fatalf("replay kept sending to a closed client: %d", n)
	}
}

// The live broadcast and the REST endpoint describe an event identically.
func TestFleetEventHasOneShape(t *testing.T) {
	e := newWebFleetEnv(t)
	task := e.dispatch("/ship one")
	events, err := e.svc.Store.ListEvents(context.Background(), task.ID, 0, 1)
	if err != nil || len(events) != 1 {
		t.Fatalf("events = %v, %v", events, err)
	}
	var ws map[string]any
	if err := json.Unmarshal(fleetEventWSData(events[0], e.project.ID), &ws); err != nil {
		t.Fatal(err)
	}
	rest := list(t, e.get("/api/fleet/tasks/"+task.ID+"/events?limit=1"), "events")[0]
	for _, key := range []string{"id", "task_id", "project_id", "type", "payload_json", "created_at"} {
		if ws[key] != rest[key] {
			t.Errorf("%s: websocket %v, rest %v", key, ws[key], rest[key])
		}
	}
}
