package app

import (
	"context"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/chimerakang/alice-core/fleet"
)

// The delivery summary is what a reviewer needs before approving a ship task:
// what was delivered, how each check came out and in which run, what is not
// done, and what to do next. It is derived on every read from the fleet's own
// records (events, verifications, diff), so it never drifts from them and a
// task that predates it still gets one from whatever it has.

// Verification states of a run.
const (
	deliveryVerifyNotConfigured = "not_configured"
	deliveryVerifyNotRun        = "not_run"
	deliveryVerifyFailed        = "failed"
	deliveryVerifyPassed        = "passed"
)

// Outcomes of one run.
const (
	deliveryRunRunning     = "running"
	deliveryRunPassed      = "passed"
	deliveryRunFailed      = "failed"
	deliveryRunNotVerified = "not_verified"
)

// Next actions the reviewer can take.
const (
	deliveryNextApprove = "approve"
	deliveryNextDecide  = "decide"
	deliveryNextRerun   = "rerun"
	deliveryNextWait    = "wait"
	deliveryNextNone    = "none"
)

// deliveryNoteLimit caps the agent's delivery note kept in a summary.
const deliveryNoteLimit = 3000

// fleetDeliveryItem is one line of a summary list. Code names the kind of
// line so each surface can word it in the reader's language; Detail is the
// fact that goes with it, when there is one.
type fleetDeliveryItem struct {
	Code   string
	Detail string
}

// fleetDeliveryCheck is one acceptance check of the round shown.
type fleetDeliveryCheck struct {
	Name       string
	Command    string
	Passed     bool
	ExitCode   int
	DurationMS int64
	TimedOut   bool
}

// fleetDeliveryRun is one launch of the task's agent and what became of it.
type fleetDeliveryRun struct {
	Number     int
	StartedAt  time.Time
	Outcome    string
	Attempt    int // verification round of this run; 0 when not verified
	StopReason string
}

// fleetDelivery is the summary of one task.
type fleetDelivery struct {
	TaskID     string
	Shape      string
	Status     string
	Approvable bool

	// Verification is the state of the latest run's acceptance. Checks are
	// the checks shown with it: those of ChecksRun, which is an earlier run
	// than the latest when ChecksPrevious is set.
	Verification   string
	ChecksRun      int
	ChecksAttempt  int
	ChecksPrevious bool
	Checks         []fleetDeliveryCheck

	DiffKnown    bool
	Files        int
	Insertions   int
	Deletions    int
	Untracked    int
	DiffTruncate bool

	Note        string
	NoteMissing bool

	StopReason string

	Runs      []fleetDeliveryRun
	Completed []fleetDeliveryItem
	Pending   []fleetDeliveryItem
	Limits    []fleetDeliveryItem
	Next      string

	PRURL    string
	PRNumber int

	// Per-item acceptance, separate from the automatic verification above.
	Accept              acceptState
	Gate                acceptGate
	EvidenceOnly        bool
	VerificationSkipped bool
}

// fleetDeliveryInput is everything a summary is built from.
type fleetDeliveryInput struct {
	Task   fleet.Task
	Events []fleet.Event
	Checks []fleet.Verification
	// Diff is nil when the task has no workspace or it could not be read.
	Diff *fleet.TaskDiff
	// VerifyCommands is how many acceptance commands the project has.
	VerifyCommands int
}

type deliveryRunState struct {
	run      fleetDeliveryRun
	verified bool
	passed   bool
	text     string
	final    string
}

// buildFleetDelivery derives the summary. It is pure so every case can be
// tested without a fleet.
func buildFleetDelivery(in fleetDeliveryInput) fleetDelivery {
	task := in.Task
	d := fleetDelivery{
		TaskID: task.ID, Shape: string(task.Shape), Status: string(task.Status),
		Approvable: task.Status == fleet.StatusReviewReady,
		PRURL:      task.PRURL, PRNumber: task.PRNumber,
	}

	runs := deliveryRuns(in.Events, in.Checks)
	if len(runs) == 0 {
		d.Limits = append(d.Limits, fleetDeliveryItem{Code: "no_run_records"})
	}
	live := task.Status == fleet.StatusRunning || task.Status == fleet.StatusVerifying || task.Status == fleet.StatusQueued
	for i := range runs {
		r := &runs[i]
		switch {
		case r.verified && r.passed && len(deliveryChecksOf(in.Checks, r.run.Attempt)) > 0:
			r.run.Outcome = deliveryRunPassed
		case r.verified && !r.passed:
			r.run.Outcome = deliveryRunFailed
		case i == len(runs)-1 && live:
			r.run.Outcome = deliveryRunRunning
		default:
			r.run.Outcome = deliveryRunNotVerified
		}
		d.Runs = append(d.Runs, r.run)
	}

	// The latest run decides the state; an earlier round is shown only when
	// the latest has no result of its own, and then it says so.
	var latest *deliveryRunState
	if len(runs) > 0 {
		latest = &runs[len(runs)-1]
	}
	shown := -1 // index of the run whose checks are shown
	switch {
	case latest != nil && latest.verified:
		shown = len(runs) - 1
	default:
		for i := len(runs) - 2; i >= 0; i-- {
			if runs[i].verified {
				shown = i
				break
			}
		}
	}
	emptyRound := latest != nil && latest.verified && latest.passed && len(deliveryChecksOf(in.Checks, latest.run.Attempt)) == 0
	currentVerified := latest != nil && latest.verified && !emptyRound
	if latest == nil && len(in.Checks) > 0 {
		// Records from before runs were tracked: the stored rounds stand in.
		currentVerified = true
	}
	switch {
	case currentVerified && deliveryAllPassed(in.Checks, latest):
		d.Verification = deliveryVerifyPassed
	case currentVerified:
		d.Verification = deliveryVerifyFailed
	case task.Shape == fleet.ShapeShip && (emptyRound || in.VerifyCommands == 0):
		d.Verification = deliveryVerifyNotConfigured
	default:
		d.Verification = deliveryVerifyNotRun
	}
	if shown >= 0 {
		d.ChecksRun = runs[shown].run.Number
		d.ChecksAttempt = runs[shown].run.Attempt
		d.ChecksPrevious = shown != len(runs)-1
		d.Checks = deliveryChecksOf(in.Checks, d.ChecksAttempt)
	} else if latest == nil && len(in.Checks) > 0 {
		d.ChecksAttempt = deliveryLatestAttempt(in.Checks)
		d.Checks = deliveryChecksOf(in.Checks, d.ChecksAttempt)
	}

	// What the agent said about its own delivery, from the latest run only:
	// an earlier run's note describes work that has since been continued.
	if latest != nil {
		d.Note = deliveryClip(latest.final, deliveryNoteLimit)
	}
	d.NoteMissing = d.Note == ""

	if in.Diff != nil {
		if files, ins, del, ok := parseDiffStat(in.Diff.Stat); ok {
			d.Files, d.Insertions, d.Deletions = files, ins, del
		}
		d.Untracked = len(in.Diff.Untracked)
		d.DiffTruncate = in.Diff.Truncated
		d.DiffKnown = true
	}

	switch task.Status {
	case fleet.StatusNeedsDecision, fleet.StatusFailed, fleet.StatusCancelled:
		d.StopReason = task.StatusReason
	}

	landing := deliveryLanding(in.Events)
	deliveryLists(&d, task, landing.Code == "")
	if landing.Code != "" {
		d.Completed = append(d.Completed, landing)
	}
	d.Next = deliveryNext(task)
	return d
}

// deliveryRuns splits the history into runs. A run starts at run.started;
// the verification round that follows it, before the next run, is its own.
// Without run.started events (older tasks) the stored rounds make the runs.
func deliveryRuns(events []fleet.Event, checks []fleet.Verification) []deliveryRunState {
	var runs []deliveryRunState
	for _, ev := range events {
		switch ev.Type {
		case fleet.EventRunStarted:
			runs = append(runs, deliveryRunState{run: fleetDeliveryRun{Number: len(runs) + 1, StartedAt: ev.CreatedAt}})
		case fleet.EventAgentText:
			if len(runs) == 0 {
				continue
			}
			var body struct {
				Text string `json:"text"`
			}
			if json.Unmarshal(ev.Payload, &body) == nil && strings.TrimSpace(body.Text) != "" {
				runs[len(runs)-1].text = strings.TrimSpace(body.Text)
			}
		case fleet.EventAgentToolUse, fleet.EventAgentToolResult:
			if len(runs) > 0 {
				runs[len(runs)-1].text = ""
			}
		case fleet.EventAgentResult:
			var result struct {
				Subtype string `json:"subtype"`
				IsError bool   `json:"is_error"`
			}
			if len(runs) > 0 && json.Unmarshal(ev.Payload, &result) == nil && result.Subtype == "success" && !result.IsError {
				r := &runs[len(runs)-1]
				r.final = r.text
			}
		case fleet.EventVerification:
			if len(runs) == 0 {
				continue
			}
			var body struct {
				Attempt int  `json:"attempt"`
				Passed  bool `json:"passed"`
			}
			if json.Unmarshal(ev.Payload, &body) != nil {
				continue
			}
			r := &runs[len(runs)-1]
			r.verified, r.passed, r.run.Attempt = true, body.Passed, body.Attempt
		case fleet.EventStatus:
			if len(runs) == 0 {
				continue
			}
			var ch fleet.StatusChange
			if json.Unmarshal(ev.Payload, &ch) != nil {
				continue
			}
			switch ch.To {
			case fleet.StatusNeedsDecision, fleet.StatusFailed, fleet.StatusCancelled:
				runs[len(runs)-1].run.StopReason = ch.Reason
			}
		}
	}
	if len(runs) > 0 {
		return runs
	}
	// No run records: one run per stored round.
	seen := map[int]bool{}
	for _, c := range checks {
		if seen[c.Attempt] {
			continue
		}
		seen[c.Attempt] = true
		passed := true
		for _, o := range checks {
			if o.Attempt == c.Attempt && !o.Passed {
				passed = false
			}
		}
		runs = append(runs, deliveryRunState{
			run:      fleetDeliveryRun{Number: len(runs) + 1, StartedAt: c.CreatedAt, Attempt: c.Attempt},
			verified: true, passed: passed,
		})
	}
	return runs
}

func deliveryLatestAttempt(checks []fleet.Verification) int {
	latest := 0
	for _, c := range checks {
		if c.Attempt > latest {
			latest = c.Attempt
		}
	}
	return latest
}

func deliveryChecksOf(checks []fleet.Verification, attempt int) []fleetDeliveryCheck {
	var out []fleetDeliveryCheck
	for _, c := range checks {
		if c.Attempt != attempt {
			continue
		}
		out = append(out, fleetDeliveryCheck{
			Name: c.Name, Command: c.Command, Passed: c.Passed, ExitCode: c.ExitCode,
			DurationMS: c.Duration.Milliseconds(), TimedOut: c.TimedOut,
		})
	}
	return out
}

// deliveryAllPassed reports whether the current round passed. It reads the
// stored checks. An empty round supplies no verification evidence.
func deliveryAllPassed(checks []fleet.Verification, latest *deliveryRunState) bool {
	attempt := 0
	if latest != nil {
		attempt = latest.run.Attempt
	} else {
		attempt = deliveryLatestAttempt(checks)
	}
	found := false
	for _, c := range checks {
		if c.Attempt != attempt {
			continue
		}
		found = true
		if !c.Passed {
			return false
		}
	}
	if found {
		return true
	}
	return false
}

// deliveryLists fills what is done, what is not, and what the reviewer should
// know, from the records alone.
func deliveryLists(d *fleetDelivery, task fleet.Task, awaitingLanding bool) {
	if d.DiffKnown && d.Files+d.Untracked > 0 {
		d.Completed = append(d.Completed, fleetDeliveryItem{Code: "changes_made"})
	} else if d.DiffKnown && task.Shape == fleet.ShapeShip {
		d.Limits = append(d.Limits, fleetDeliveryItem{Code: "no_changes"})
	} else if !d.DiffKnown && task.Shape == fleet.ShapeShip {
		d.Limits = append(d.Limits, fleetDeliveryItem{Code: "diff_unavailable"})
	}

	switch d.Verification {
	case deliveryVerifyPassed:
		d.Completed = append(d.Completed, fleetDeliveryItem{Code: "checks_passed"})
	case deliveryVerifyFailed:
		for _, c := range d.Checks {
			if !c.Passed {
				d.Pending = append(d.Pending, fleetDeliveryItem{Code: "check_failed", Detail: firstNonEmpty(c.Command, c.Name)})
			}
		}
	case deliveryVerifyNotRun:
		d.Pending = append(d.Pending, fleetDeliveryItem{Code: "not_verified"})
	case deliveryVerifyNotConfigured:
		d.Limits = append(d.Limits, fleetDeliveryItem{Code: "no_verify_commands"})
	}
	if d.ChecksPrevious && len(d.Checks) > 0 {
		d.Limits = append(d.Limits, fleetDeliveryItem{Code: "checks_from_earlier_run"})
	}
	if task.PRURL != "" && awaitingLanding {
		d.Completed = append(d.Completed, fleetDeliveryItem{Code: "pr_opened"})
	}

	switch task.Status {
	case fleet.StatusNeedsDecision, fleet.StatusFailed, fleet.StatusCancelled:
		d.Pending = append(d.Pending, fleetDeliveryItem{Code: "stopped", Detail: task.StatusReason})
	case fleet.StatusRunning, fleet.StatusVerifying, fleet.StatusQueued:
		d.Pending = append(d.Pending, fleetDeliveryItem{Code: "in_progress"})
	}
	if d.NoteMissing {
		d.Limits = append(d.Limits, fleetDeliveryItem{Code: "no_note"})
	}
	if d.DiffTruncate {
		d.Limits = append(d.Limits, fleetDeliveryItem{Code: "diff_truncated"})
	}
}

// deliveryLanding reads the recorded outcome only after Fleet successfully landed the change.
func deliveryLanding(events []fleet.Event) fleetDeliveryItem {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type != fleet.EventDeliveryApproved {
			continue
		}
		var payload struct {
			Landed string `json:"landed"`
		}
		if json.Unmarshal(events[i].Payload, &payload) != nil {
			continue
		}
		if branch, ok := strings.CutPrefix(payload.Landed, "fast-forwarded "); ok && branch != "" {
			return fleetDeliveryItem{Code: "landed_local", Detail: branch}
		}
		if url, ok := strings.CutPrefix(payload.Landed, "merged "); ok && url != "" {
			return fleetDeliveryItem{Code: "landed_pr", Detail: url}
		}
	}
	return fleetDeliveryItem{}
}

func deliveryNext(task fleet.Task) string {
	switch task.Status {
	case fleet.StatusReviewReady:
		return deliveryNextApprove
	case fleet.StatusNeedsDecision:
		return deliveryNextDecide
	case fleet.StatusFailed, fleet.StatusCancelled:
		return deliveryNextRerun
	case fleet.StatusQueued, fleet.StatusRunning, fleet.StatusVerifying:
		return deliveryNextWait
	}
	return deliveryNextNone
}

func deliveryClip(s string, max int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max]) + "…"
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// Delivery reads what a task's summary is built from and builds it.
func (f *FleetService) Delivery(ctx context.Context, taskID string) (fleetDelivery, error) {
	task, err := f.Commander.GetTask(ctx, taskID)
	if err != nil {
		return fleetDelivery{}, err
	}
	in := fleetDeliveryInput{Task: task}
	var after int64
	for page := 0; page < 100; page++ {
		events, next, more, err := f.Commander.TaskEvents(ctx, taskID, after, 1000)
		if err != nil {
			return fleetDelivery{}, err
		}
		in.Events = append(in.Events, events...)
		after = next
		if !more {
			break
		}
	}
	if in.Checks, err = f.Commander.Verifications(ctx, taskID); err != nil {
		return fleetDelivery{}, err
	}
	if project, err := f.Store.GetProject(ctx, task.ProjectID); err == nil {
		in.VerifyCommands = len(project.VerifyCommands)
	}
	if task.Shape == fleet.ShapeShip {
		if diff, err := f.Commander.TaskDiff(ctx, taskID); err == nil {
			in.Diff = &diff
		}
	}
	d := buildFleetDelivery(in)
	d.VerificationSkipped = deliverySkippedVerification(in.Events)
	if d.Accept, err = f.Acceptance(ctx, taskID); err != nil {
		return fleetDelivery{}, err
	}
	if d.Accept.Structured {
		d.Gate, d.EvidenceOnly, err = f.CheckClose(ctx, taskID)
		if err != nil {
			return fleetDelivery{}, err
		}
	}
	return d, nil
}

// deliverySkippedVerification reports whether the task reached review by the
// owner waving it through, not by passing its checks.
func deliverySkippedVerification(events []fleet.Event) bool {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type != fleet.EventStatus {
			continue
		}
		var ch fleet.StatusChange
		if json.Unmarshal(events[i].Payload, &ch) != nil || ch.To != fleet.StatusReviewReady {
			continue
		}
		return strings.HasPrefix(ch.Reason, "waved through")
	}
	return false
}
