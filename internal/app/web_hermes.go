package app

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"claude-tg-agent/internal/app/hermes"
)

// activeTaskStore is the storage surface the Hermes dashboard tools
// need. SQLiteTaskStore satisfies it; the noop store does not (so a
// missing-DB deployment surfaces 503 rather than blank lists).
type activeTaskStore interface {
	ListActiveTasks() ([]hermes.ActiveTaskRef, error)
	ListHermesTasks(filter hermes.HermesTaskFilter) ([]hermes.ActiveTaskRef, int, error)
	ListSnapshotHistory(taskID string) ([]hermes.Snapshot, error)
	ApplyInterruptResolution(taskID string, decision hermes.InterruptResolution) error
	GetTask(id string) (hermes.TaskState, error)
}

// handleHermesActive returns every non-terminal Hermes task with its
// latest snapshot's NextStep + Interrupt. Used by the dashboard's
// Hermes Tasks panel (#171 Class C UI) so operators can see paused
// tasks without tailing Telegram.
func (wi *WebInterface) handleHermesActive(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		store, ok := buildHermesTaskStore().(activeTaskStore)
		if !ok {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "hermes task store unavailable"})
			return
		}
		tasks, err := store.ListActiveTasks()
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tasks": tasks,
			"total": len(tasks),
		})
	})(w, r)
}

// hermesResolveRequest is the POST body for the resolve endpoint.
type hermesResolveRequest struct {
	TaskID   string `json:"task_id"`
	Decision string `json:"decision"` // retry | skip | abort
}

// handleHermesResolve applies an InterruptResolution to a paused task
// and (for retry) relaunches the Hermes goroutine via the same
// startHermesTaskWithIssueTierFromState path the Telegram cold-restart
// flow uses. Auth is gated on the same Bearer token that other control
// endpoints use.
//
// Decision mapping:
//
//	retry  → InterruptResolutionRetry  → re-run paused step (failure
//	         pause re-runs sub-task; budget pause clears + resets
//	         BudgetStartedAt + resumes at ResumeStep)
//	skip   → InterruptResolutionSkip   → mark sub-task Skipped, advance
//	abort  → InterruptResolutionAbort  → mark task failed
//
// Resume: only retry / skip relaunch the goroutine. abort just marks
// the task failed and returns.
func (wi *WebInterface) handleHermesResolve(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if wi.apiToken != "" && r.Header.Get("Authorization") != "Bearer "+wi.apiToken {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req hermesResolveRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		req.TaskID = strings.TrimSpace(req.TaskID)
		req.Decision = strings.TrimSpace(strings.ToLower(req.Decision))
		if req.TaskID == "" {
			http.Error(w, "task_id is required", http.StatusBadRequest)
			return
		}
		var resolution hermes.InterruptResolution
		switch req.Decision {
		case "retry":
			resolution = hermes.InterruptResolutionRetry
		case "skip":
			resolution = hermes.InterruptResolutionSkip
		case "abort":
			resolution = hermes.InterruptResolutionAbort
		default:
			http.Error(w, "decision must be retry | skip | abort", http.StatusBadRequest)
			return
		}

		store, ok := buildHermesTaskStore().(activeTaskStore)
		if !ok {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "hermes task store unavailable"})
			return
		}
		task, err := store.GetTask(req.TaskID)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": fmt.Sprintf("task not found: %v", err)})
			return
		}
		if err := store.ApplyInterruptResolution(req.TaskID, resolution); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
			return
		}
		log.Printf("[hermes.web] resolution applied task=%s decision=%s by=web", req.TaskID, resolution)

		// Abort is terminal — no goroutine to relaunch.
		if resolution == hermes.InterruptResolutionAbort {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":         true,
				"task_id":    req.TaskID,
				"decision":   string(resolution),
				"relaunched": false,
			})
			return
		}

		// retry / skip: relaunch via the bot's existing cold-restart-style
		// path. The web handler does not have direct access to per-chat
		// hermesCoord state, but startHermesTaskWithIssueTierFromState
		// rebuilds that state from snapshot, which is what we want.
		relaunched := false
		if wi.bot != nil {
			resumeState, _ := store.GetTask(req.TaskID)
			if resumeState.ID == "" {
				resumeState = task
			}
			key := chatKey{chatID: resumeState.ChatID, threadID: resumeState.ThreadID}
			go wi.bot.runTrackedJob("hermes.web_resume", func() {
				wi.bot.startHermesTaskWithIssueTierFromState(
					key,
					resumeState.Goal,
					resumeState.ProjectDir,
					resumeState.GithubIssueNumber,
					HermesBudgetConfig{},
					wi.bot.config.Hermes.GithubIntegration,
					wi.bot.hermesTierFor(key),
					&resumeState,
				)
			})
			relaunched = true
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":         true,
			"task_id":    req.TaskID,
			"decision":   string(resolution),
			"relaunched": relaunched,
		})
	})(w, r)
}

// handleHermesTasks returns Hermes tasks (active + terminal), optionally
// filtered by status. Used by the dashboard's Hermes history page
// (#171 Class C UI follow-up). Read-only — no auth.
//
// Query params:
//
//	status=planning|executing|done|failed|interrupted (optional)
//	limit=<int>  (default 100, capped at 500)
//	offset=<int> (default 0)
func (wi *WebInterface) handleHermesTasks(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		store, ok := buildHermesTaskStore().(activeTaskStore)
		if !ok {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "hermes task store unavailable"})
			return
		}
		q := r.URL.Query()
		filter := hermes.HermesTaskFilter{
			Status: strings.TrimSpace(q.Get("status")),
		}
		if v := q.Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				filter.Limit = n
			}
		}
		if v := q.Get("offset"); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				filter.Offset = n
			}
		}
		tasks, total, err := store.ListHermesTasks(filter)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tasks":  tasks,
			"total":  total,
			"limit":  filter.Limit,
			"offset": filter.Offset,
		})
	})(w, r)
}

// handleHermesSnapshots returns the snapshot history (Walker hops) for
// a single task: source_node, next_step, metadata.reason, created_at,
// step number. Used by the history page's drill-in to visualise the
// path a task took through the graph.
func (wi *WebInterface) handleHermesSnapshots(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		taskID := strings.TrimSpace(r.URL.Query().Get("task_id"))
		if taskID == "" {
			http.Error(w, "task_id is required", http.StatusBadRequest)
			return
		}
		store, ok := buildHermesTaskStore().(activeTaskStore)
		if !ok {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "hermes task store unavailable"})
			return
		}
		hist, err := store.ListSnapshotHistory(taskID)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
			return
		}
		// Project to a slim shape — full state JSON is too large to
		// stream for every hop and the dashboard only needs the path.
		type hopView struct {
			Step             int                      `json:"step"`
			SnapshotID       string                   `json:"snapshot_id"`
			ParentSnapshotID string                   `json:"parent_snapshot_id,omitempty"`
			SourceNode       string                   `json:"source_node,omitempty"`
			NextStep         string                   `json:"next_step,omitempty"`
			Reason           string                   `json:"reason,omitempty"`
			Status           string                   `json:"status,omitempty"`
			CurrentIdx       int                      `json:"current_idx"`
			HasInterrupt     bool                     `json:"has_interrupt,omitempty"`
			InterruptReason  string                   `json:"interrupt_reason,omitempty"`
			CreatedAt        string                   `json:"created_at"`
		}
		out := make([]hopView, 0, len(hist))
		for _, s := range hist {
			hop := hopView{
				Step:             s.Step,
				SnapshotID:       s.ID,
				ParentSnapshotID: s.ParentSnapshotID,
				SourceNode:       string(s.SourceNode),
				NextStep:         string(s.NextStep),
				Reason:           s.Metadata.Reason,
				Status:           string(s.State.Status),
				CurrentIdx:       s.State.CurrentIdx,
				CreatedAt:        s.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
			}
			if s.State.Interrupt != nil {
				hop.HasInterrupt = true
				hop.InterruptReason = s.State.Interrupt.Reason
			}
			out = append(out, hop)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"task_id":   taskID,
			"snapshots": out,
			"total":     len(out),
		})

		log.Printf("[hermes.web] snapshot history task=%s hops=%d", taskID, len(out))
	})(w, r)
}
