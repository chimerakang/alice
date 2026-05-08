package hermes

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestPlannerSessionResumesAcrossJSONRetries(t *testing.T) {
	var gotSessionIDs []string
	calls := 0
	planFn := func(ctx context.Context, message, projectDir, sessionID string) (CallPlanResult, error) {
		gotSessionIDs = append(gotSessionIDs, sessionID)
		calls++
		if calls == 1 {
			return CallPlanResult{Text: "not json", SessionID: "planner-session-1"}, nil
		}
		return CallPlanResult{
			Text:      "```json\n" + `[{"id":"s1","description":"Execute directly","tool_hints":["Read"]}]` + "\n```",
			SessionID: "planner-session-1",
		}, nil
	}

	planner := NewPlannerSession(planFn, 2, "")
	if _, _, _, _, err := planner.Plan(context.Background(), "implement feature", "/repo"); err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if want := []string{"", "planner-session-1"}; !reflect.DeepEqual(gotSessionIDs, want) {
		t.Fatalf("session ids = %#v, want %#v", gotSessionIDs, want)
	}
	if got := planner.SessionID(); got != "planner-session-1" {
		t.Fatalf("planner session = %q, want planner-session-1", got)
	}
}

func TestPlannerSessionUsesRecoveryDeciderForJSONRetry(t *testing.T) {
	calls := 0
	var recoveryReqs []PlannerRecoveryRequest
	planFn := func(ctx context.Context, message, projectDir, sessionID string) (CallPlanResult, error) {
		calls++
		return CallPlanResult{Text: "not json"}, nil
	}

	planner := NewPlannerSession(planFn, 3, "")
	planner.SetRecoveryDecider(func(req PlannerRecoveryRequest) PlannerRecoveryDecision {
		recoveryReqs = append(recoveryReqs, req)
		return PlannerRecoveryDecision{Retry: false, Reason: "test_denied"}
	})
	if _, _, _, _, err := planner.Plan(context.Background(), "implement feature", "/repo"); err == nil {
		t.Fatal("Plan error = nil, want planner JSON failure")
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1 when recovery denies retry", calls)
	}
	if len(recoveryReqs) != 1 {
		t.Fatalf("recovery requests = %d, want 1", len(recoveryReqs))
	}
	req := recoveryReqs[0]
	if req.Mode != "planner_retry" || req.Attempt != 1 || req.MaxAttempts != 3 || req.Reason != "json_parse_failed" {
		t.Fatalf("unexpected recovery request: %+v", req)
	}
}

func TestPlannerSessionUsesSeededSessionID(t *testing.T) {
	var gotSessionID string
	planFn := func(ctx context.Context, message, projectDir, sessionID string) (CallPlanResult, error) {
		gotSessionID = sessionID
		return CallPlanResult{
			Text:      "```json\n" + `[{"id":"s1","description":"Execute directly","tool_hints":["Read"]}]` + "\n```",
			SessionID: "planner-session-next",
		}, nil
	}

	planner := NewPlannerSession(planFn, 1, "")
	planner.SetSessionID("planner-session-prev")
	if _, _, _, _, err := planner.Plan(context.Background(), "implement feature", "/repo"); err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if gotSessionID != "planner-session-prev" {
		t.Fatalf("call session = %q, want planner-session-prev", gotSessionID)
	}
	if got := planner.SessionID(); got != "planner-session-next" {
		t.Fatalf("planner session = %q, want planner-session-next", got)
	}
}

func TestPlannerSessionRejectsSplitSingleActionAndRetries(t *testing.T) {
	var prompts []string
	calls := 0
	planFn := func(ctx context.Context, message, projectDir, sessionID string) (CallPlanResult, error) {
		prompts = append(prompts, message)
		calls++
		if calls == 1 {
			return CallPlanResult{
				Text: "```json\n" + `[
{"id":"s1","description":"Read existing order e2e tests to understand the pattern","tool_hints":["Read"]},
{"id":"s2","description":"Add one e2e test for the new field","tool_hints":["Edit"]},
{"id":"s3","description":"Run go test ./internal/order/... to verify the new e2e test","tool_hints":["Bash"]}
]` + "\n```",
				SessionID: "planner-session-1",
			}, nil
		}
		return CallPlanResult{
			Text:      "```json\n" + `[{"id":"s1","description":"Add one e2e test for the new field: read the nearby test pattern, implement the test, then run go test ./internal/order/... to verify it passes.","tool_hints":["Read","Edit","Bash"]}]` + "\n```",
			SessionID: "planner-session-1",
		}, nil
	}

	planner := NewPlannerSession(planFn, 2, "")
	tasks, _, _, _, err := planner.Plan(context.Background(), "補一個 order e2e 測試", "/repo")
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
	if len(tasks) != 1 {
		t.Fatalf("tasks = %d, want 1: %#v", len(tasks), tasks)
	}
	if len(prompts) < 2 || !containsAny(prompts[1], "granularity violation", "grouping related read/modify/verify") {
		t.Fatalf("retry prompt did not include granularity feedback:\n%v", prompts)
	}
}

func TestPlannerSessionRejectsImplementationPlanWithoutMutation(t *testing.T) {
	var prompts []string
	var gates []PlanQualityGateEvent
	calls := 0
	planFn := func(ctx context.Context, message, projectDir, sessionID string) (CallPlanResult, error) {
		prompts = append(prompts, message)
		calls++
		if calls == 1 {
			return CallPlanResult{
				Text: "```json\n" + `[
{"id":"s1","description":"Read the auth service to understand the current implementation","tool_hints":["Read"]},
{"id":"s2","description":"Run go test ./internal/auth/... and report the result","tool_hints":["Bash"]}
]` + "\n```",
			}, nil
		}
		return CallPlanResult{
			Text: "```json\n" + `[
{"id":"s1","description":"Fix the auth refresh-token bug in the service, update the focused tests, then run go test ./internal/auth/... to verify the fix.","tool_hints":["Read","Edit","Bash"]}
]` + "\n```",
		}, nil
	}

	planner := NewPlannerSession(planFn, 2, "")
	planner.SetPlanQualityGateReporter(func(event PlanQualityGateEvent) {
		gates = append(gates, event)
	})
	tasks, _, _, _, err := planner.Plan(context.Background(), "[GitHub #12] 修正 auth refresh-token bug", "/repo")
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
	if len(tasks) != 1 || !containsAny(tasks[0].Description, "Fix the auth refresh-token bug") {
		t.Fatalf("unexpected tasks: %#v", tasks)
	}
	if len(prompts) < 2 || !containsAny(prompts[1], "Plan quality gate rejected", "missing implementation step") {
		t.Fatalf("retry prompt did not include implementation feedback:\n%v", prompts)
	}
	if len(gates) != 2 {
		t.Fatalf("quality gate events = %d, want 2: %#v", len(gates), gates)
	}
	if gates[0].Action != "replan" || gates[0].Reason != "plan_quality_rejected" || !containsAny(gates[0].Violation, "missing implementation step") {
		t.Fatalf("first quality gate = %+v, want replan rejection", gates[0])
	}
	if gates[1].Action != "allow" || gates[1].Reason != "gate_passed" {
		t.Fatalf("second quality gate = %+v, want allow", gates[1])
	}
}

func TestPlannerSessionRejectsImplementationPlanWithoutValidation(t *testing.T) {
	calls := 0
	planFn := func(ctx context.Context, message, projectDir, sessionID string) (CallPlanResult, error) {
		calls++
		if calls == 1 {
			return CallPlanResult{
				Text: "```json\n" + `[
{"id":"s1","description":"Update the tournament service to pass tv_blocks_json through create and update paths","tool_hints":["Read","Edit"]}
]` + "\n```",
			}, nil
		}
		return CallPlanResult{
			Text: "```json\n" + `[
{"id":"s1","description":"Update the tournament service to pass tv_blocks_json through create and update paths, then run go test ./internal/service/... to verify the mapping.","tool_hints":["Read","Edit","Bash"]}
]` + "\n```",
		}, nil
	}

	planner := NewPlannerSession(planFn, 2, "")
	tasks, _, _, _, err := planner.Plan(context.Background(), "[GitHub #12] 實作 tv_blocks_json service mapping", "/repo")
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
	if len(tasks) != 1 || !containsAny(tasks[0].Description, "go test ./internal/service/...") {
		t.Fatalf("unexpected tasks: %#v", tasks)
	}
}

func TestValidatePlanQualityAllowsVerificationOnlyGoal(t *testing.T) {
	tasks := []SubTask{{
		ID:          "s1",
		Description: "Run npm test, npm run build, and npm run lint to verify the existing ChatView implementation still passes.",
		ToolHints:   []string{"Bash"},
	}}
	if err := validatePlanQuality("verify issue #57 is already completed", tasks); err != nil {
		t.Fatalf("verification-only plan should be accepted: %v", err)
	}
}

func TestValidateChecklistDeclaration_NoMarkersBypass(t *testing.T) {
	tasks := []SubTask{{ID: "s1", Description: "do work", ToolHints: []string{"Bash"}}}
	if err := validateChecklistDeclaration("plain goal without markers", tasks); err != nil {
		t.Fatalf("legacy goal without [item-N] markers should bypass: %v", err)
	}
}

func TestValidateChecklistDeclaration_AllItemsCovered(t *testing.T) {
	goal := "[GitHub #29] do something\n  - [ ] [item-3] verify build\n  - [ ] [item-5] add tests\n"
	tasks := []SubTask{
		{ID: "s1", Description: "fix and verify build", ChecklistItemIDs: []string{"item-3"}},
		{ID: "s2", Description: "add tests for the fix", ChecklistItemIDs: []string{"item-5"}},
	}
	if err := validateChecklistDeclaration(goal, tasks); err != nil {
		t.Fatalf("full coverage should pass: %v", err)
	}
}

func TestValidateChecklistDeclaration_MultipleItemsPerSubTask(t *testing.T) {
	goal := "[GitHub #80] - [ ] [item-2] X\n - [ ] [item-4] Y\n - [ ] [item-6] Z\n"
	tasks := []SubTask{
		{ID: "s1", Description: "do X+Y together", ChecklistItemIDs: []string{"item-2", "item-4"}},
		{ID: "s2", Description: "do Z", ChecklistItemIDs: []string{"item-6"}},
	}
	if err := validateChecklistDeclaration(goal, tasks); err != nil {
		t.Fatalf("multi-item declaration should pass: %v", err)
	}
}

func TestValidateChecklistDeclaration_MissingCoverageRejected(t *testing.T) {
	// Reproduces #29 / #167 pattern: planner produces fewer sub-tasks than
	// checklist items has, leaving acceptance items uncovered.
	goal := "[GitHub #167] - [ ] [item-1] A\n - [ ] [item-2] B\n - [ ] [item-3] C\n - [ ] [item-4] D\n - [ ] [item-5] E\n"
	tasks := []SubTask{
		{ID: "s1", Description: "do A", ChecklistItemIDs: []string{"item-1"}},
		{ID: "s2", Description: "do B+C", ChecklistItemIDs: []string{"item-2", "item-3"}},
		// items 4 and 5 have no coverage
	}
	err := validateChecklistDeclaration(goal, tasks)
	if err == nil {
		t.Fatal("missing coverage should reject")
	}
	if !strings.Contains(err.Error(), "item-4") || !strings.Contains(err.Error(), "item-5") {
		t.Errorf("error should name missing items: %v", err)
	}
}

func TestValidateChecklistDeclaration_SetupTaskWithEmptyArrayAllowed(t *testing.T) {
	goal := "[GitHub #123] - [ ] [item-1] A\n - [ ] [item-2] B\n"
	tasks := []SubTask{
		{ID: "s0", Description: "scaffold migration (no acceptance item; rationale: prerequisite)", ChecklistItemIDs: []string{}},
		{ID: "s1", Description: "do A", ChecklistItemIDs: []string{"item-1"}},
		{ID: "s2", Description: "do B", ChecklistItemIDs: []string{"item-2"}},
	}
	if err := validateChecklistDeclaration(goal, tasks); err != nil {
		t.Fatalf("setup task with empty declaration should not block: %v", err)
	}
}
