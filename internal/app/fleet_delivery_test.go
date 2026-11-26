package app

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/chimerakang/alice-core/fleet"
)

func dvEvent(id int64, typ string, payload any) fleet.Event {
	raw, _ := json.Marshal(payload)
	return fleet.Event{ID: id, TaskID: "t1", Type: typ, Payload: raw, CreatedAt: time.Unix(1000+id, 0)}
}

func dvCheck(attempt int, cmd string, passed bool) fleet.Verification {
	return fleet.Verification{TaskID: "t1", Attempt: attempt, Name: cmd, Command: cmd, Passed: passed}
}

func dvCodes(items []fleetDeliveryItem) string {
	var codes []string
	for _, it := range items {
		codes = append(codes, it.Code)
	}
	return strings.Join(codes, ",")
}

var dvDiff = &fleet.TaskDiff{Stat: " 2 files changed, 10 insertions(+), 1 deletion(-)"}

func TestDelivery_PassedShowsChecksAndNote(t *testing.T) {
	d := buildFleetDelivery(fleetDeliveryInput{
		Task: fleet.Task{ID: "t1", Shape: fleet.ShapeShip, Status: fleet.StatusReviewReady},
		Events: []fleet.Event{
			dvEvent(1, fleet.EventRunStarted, nil),
			dvEvent(2, fleet.EventAgentText, map[string]string{"text": "Added the summary panel."}),
			dvEvent(3, fleet.EventAgentResult, map[string]any{"subtype": "success"}),
			dvEvent(4, fleet.EventVerification, map[string]any{"attempt": 1, "passed": true}),
		},
		Checks: []fleet.Verification{dvCheck(1, "go test ./...", true), dvCheck(1, "go vet ./...", true)},
		Diff:   dvDiff, VerifyCommands: 2,
	})
	if d.Verification != deliveryVerifyPassed || !d.Approvable || d.Next != deliveryNextApprove {
		t.Fatalf("got %+v", d)
	}
	if len(d.Checks) != 2 || d.ChecksPrevious || d.Note != "Added the summary panel." || d.NoteMissing {
		t.Fatalf("got %+v", d)
	}
	if d.Files != 2 || d.Insertions != 10 || dvCodes(d.Completed) != "changes_made,checks_passed" {
		t.Fatalf("got %+v", d)
	}
}

func TestDelivery_NotConfiguredIsNotNotRun(t *testing.T) {
	task := fleet.Task{ID: "t1", Shape: fleet.ShapeShip, Status: fleet.StatusReviewReady}
	events := []fleet.Event{dvEvent(1, fleet.EventRunStarted, nil), dvEvent(2, fleet.EventVerification, map[string]any{"attempt": 1, "passed": true})}
	d := buildFleetDelivery(fleetDeliveryInput{Task: task, Events: events, Diff: dvDiff})
	if d.Verification != deliveryVerifyNotConfigured {
		t.Fatalf("got %q", d.Verification)
	}
	d = buildFleetDelivery(fleetDeliveryInput{Task: task, Events: events[:1], Diff: dvDiff, VerifyCommands: 1})
	if d.Verification != deliveryVerifyNotRun {
		t.Fatalf("got %q", d.Verification)
	}
}

func TestDelivery_Failed(t *testing.T) {
	d := buildFleetDelivery(fleetDeliveryInput{
		Task: fleet.Task{ID: "t1", Shape: fleet.ShapeShip, Status: fleet.StatusNeedsDecision, StatusReason: "verification failed after 3 attempts"},
		Events: []fleet.Event{
			dvEvent(1, fleet.EventRunStarted, nil),
			dvEvent(2, fleet.EventVerification, map[string]any{"attempt": 1, "passed": false}),
		},
		Checks: []fleet.Verification{dvCheck(1, "go test ./...", false)},
		Diff:   dvDiff, VerifyCommands: 1,
	})
	if d.Verification != deliveryVerifyFailed || d.Next != deliveryNextDecide || d.Approvable {
		t.Fatalf("got %+v", d)
	}
	if !strings.Contains(dvCodes(d.Pending), "check_failed") || d.StopReason == "" {
		t.Fatalf("got %+v", d)
	}
}

// The run was cancelled part-way with changes on disk: it never reached the
// checks, and the summary says so and keeps the stop reason.
func TestDelivery_CancelledBeforeVerification(t *testing.T) {
	d := buildFleetDelivery(fleetDeliveryInput{
		Task: fleet.Task{ID: "t1", Shape: fleet.ShapeShip, Status: fleet.StatusCancelled, StatusReason: "cancelled by owner"},
		Events: []fleet.Event{
			dvEvent(1, fleet.EventRunStarted, nil),
			dvEvent(2, fleet.EventAgentText, map[string]string{"text": "Done with the backend, UI left."}),
			dvEvent(3, fleet.EventStatus, fleet.StatusChange{From: fleet.StatusRunning, To: fleet.StatusCancelled, Reason: "cancelled by owner"}),
		},
		Diff: dvDiff, VerifyCommands: 2,
	})
	if d.Verification != deliveryVerifyNotRun || d.StopReason != "cancelled by owner" || d.Next != deliveryNextRerun {
		t.Fatalf("got %+v", d)
	}
	if d.Runs[0].Outcome != deliveryRunNotVerified || d.Runs[0].StopReason != "cancelled by owner" {
		t.Fatalf("got %+v", d.Runs)
	}
	if !strings.Contains(dvCodes(d.Pending), "not_verified") || !strings.Contains(dvCodes(d.Pending), "stopped") || !strings.Contains(dvCodes(d.Completed), "changes_made") {
		t.Fatalf("got %+v", d)
	}
	if !d.NoteMissing {
		t.Fatal("progress text must not be presented as delivery")
	}
}

// #205: one run cancelled with changes, then a rerun whose two checks passed.
func TestDelivery_RerunKeepsRunsApart(t *testing.T) {
	d := buildFleetDelivery(fleetDeliveryInput{
		Task: fleet.Task{ID: "t1", Shape: fleet.ShapeShip, Status: fleet.StatusReviewReady},
		Events: []fleet.Event{
			dvEvent(1, fleet.EventRunStarted, nil),
			dvEvent(2, fleet.EventStatus, fleet.StatusChange{To: fleet.StatusCancelled, Reason: "cancelled"}),
			dvEvent(3, fleet.EventRunStarted, nil),
			dvEvent(4, fleet.EventVerification, map[string]any{"attempt": 1, "passed": true}),
		},
		Checks: []fleet.Verification{dvCheck(1, "a", true), dvCheck(1, "b", true)},
		Diff:   dvDiff, VerifyCommands: 2,
	})
	if len(d.Runs) != 2 || d.Runs[0].Outcome != deliveryRunNotVerified || d.Runs[1].Outcome != deliveryRunPassed || d.Runs[1].Attempt != 1 {
		t.Fatalf("got %+v", d.Runs)
	}
	if d.ChecksRun != 2 || d.ChecksPrevious || d.Verification != deliveryVerifyPassed {
		t.Fatalf("got %+v", d)
	}
}

// While a resumed run is going, the earlier run's pass stays readable and is
// labelled as earlier.
func TestDelivery_ResumedRunShowsPreviousPass(t *testing.T) {
	d := buildFleetDelivery(fleetDeliveryInput{
		Task: fleet.Task{ID: "t1", Shape: fleet.ShapeShip, Status: fleet.StatusRunning},
		Events: []fleet.Event{
			dvEvent(1, fleet.EventRunStarted, nil),
			dvEvent(2, fleet.EventVerification, map[string]any{"attempt": 1, "passed": true}),
			dvEvent(3, fleet.EventRunStarted, nil),
		},
		Checks: []fleet.Verification{dvCheck(1, "go test ./...", true)},
		Diff:   dvDiff, VerifyCommands: 1,
	})
	if d.Verification != deliveryVerifyNotRun || !d.ChecksPrevious || d.ChecksRun != 1 || len(d.Checks) != 1 {
		t.Fatalf("got %+v", d)
	}
	if d.Runs[1].Outcome != deliveryRunRunning || !strings.Contains(dvCodes(d.Limits), "checks_from_earlier_run") {
		t.Fatalf("got %+v", d)
	}
	if d.Next != deliveryNextWait {
		t.Fatalf("next %q", d.Next)
	}
}

// A task from before runs were recorded still gets a summary from what it has.
func TestDelivery_LegacyTaskWithoutEvents(t *testing.T) {
	d := buildFleetDelivery(fleetDeliveryInput{
		Task:           fleet.Task{ID: "t1", Shape: fleet.ShapeShip, Status: fleet.StatusReviewReady},
		Checks:         []fleet.Verification{dvCheck(1, "go test ./...", true)},
		VerifyCommands: 1,
	})
	if d.Verification != deliveryVerifyPassed || !d.NoteMissing || len(d.Checks) != 1 || len(d.Runs) != 1 {
		t.Fatalf("got %+v", d)
	}
	for _, want := range []string{"no_note", "diff_unavailable", "no_run_records"} {
		if !strings.Contains(dvCodes(d.Limits), want) && want != "no_run_records" {
			t.Fatalf("missing %s in %s", want, dvCodes(d.Limits))
		}
	}
	bare := buildFleetDelivery(fleetDeliveryInput{Task: fleet.Task{ID: "t1", Shape: fleet.ShapeShip, Status: fleet.StatusDone}})
	if !strings.Contains(dvCodes(bare.Limits), "no_run_records") || bare.Verification != deliveryVerifyNotConfigured {
		t.Fatalf("got %+v", bare)
	}
}

func TestRenderFleetDelivery(t *testing.T) {
	loc := func(k string, v map[string]string) string {
		out := k
		for name, val := range v {
			out += " " + name + "=" + val
		}
		return out
	}
	d := buildFleetDelivery(fleetDeliveryInput{
		Task: fleet.Task{ID: "t1", Shape: fleet.ShapeShip, Status: fleet.StatusRunning},
		Events: []fleet.Event{
			dvEvent(1, fleet.EventRunStarted, nil),
			dvEvent(2, fleet.EventVerification, map[string]any{"attempt": 1, "passed": true}),
			dvEvent(3, fleet.EventRunStarted, nil),
		},
		Checks: []fleet.Verification{dvCheck(1, "go test ./...", true)},
		Diff:   dvDiff, VerifyCommands: 1,
	})
	out := renderFleetDelivery(loc, d, "http://dash/fleet/tasks/t1")
	for _, want := range []string{"fleet_dv_verify_not_run", "fleet_dv_verify_previous", "go test ./...", "fleet_dv_item_checks_from_earlier_run", "http://dash/fleet/tasks/t1", "fleet_dv_next_wait"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestDelivery_UsesRecordedLanding(t *testing.T) {
	task := fleet.Task{Shape: fleet.ShapeShip, Status: fleet.StatusDone, PRURL: "https://example.com/pr/1"}
	for _, tc := range []struct{ outcome, want string }{
		{"", "pr_opened"},
		{"fast-forwarded main", "landed_local"},
		{"merged https://example.com/pr/1", "landed_pr"},
	} {
		in := fleetDeliveryInput{Task: task}
		if tc.outcome != "" {
			in.Events = []fleet.Event{dvEvent(1, fleet.EventDeliveryApproved, map[string]string{"landed": tc.outcome})}
		}
		if got := dvCodes(buildFleetDelivery(in).Completed); got != tc.want {
			t.Errorf("landing %q: got %q, want %q", tc.outcome, got, tc.want)
		}
	}
}
