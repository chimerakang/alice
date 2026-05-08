package graph

import (
	"context"
	"errors"
	"testing"

	"claude-tg-agent/internal/app/hermes"
)

type fakeReplanDecider struct {
	calls    int
	decision ReplanDecision
	err      error
}

func (d *fakeReplanDecider) DecideReplan(_ context.Context, _ hermes.HermesState) (ReplanDecision, error) {
	d.calls++
	return d.decision, d.err
}

func TestReplanSetupNode_PartialDecisionInstallsContext(t *testing.T) {
	dec := &fakeReplanDecider{decision: ReplanDecision{
		Goal:        "augmented goal with reviewer feedback",
		Accumulated: "preserved transcript prefix\n",
		PreservedSubTasks: []hermes.SubTask{
			{ID: "s1", Description: "kept", Status: hermes.SubTaskDone, Result: "ok"},
		},
		AttemptIdx: 1,
		Trigger:    "partial",
	}}
	node := &ReplanSetupNode{Decider: dec}

	out, err := node.Handle(context.Background(), hermes.HermesState{TaskID: "t", Goal: "orig"})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if out.NextStep != hermes.RuntimeStepPlanner {
		t.Errorf("NextStep = %q, want planner", out.NextStep)
	}
	if out.Reason != "replan_setup_partial" {
		t.Errorf("Reason = %q", out.Reason)
	}
	if len(out.Updates) != 1 || out.Updates[0].Replan == nil {
		t.Fatalf("expected Replan in update: %+v", out.Updates)
	}
	rc := out.Updates[0].Replan
	if rc.Trigger != "partial" || rc.AttemptIdx != 1 {
		t.Errorf("Replan ctx: %+v", rc)
	}
	if len(rc.PreservedSubTasks) != 1 || rc.PreservedSubTasks[0].ID != "s1" {
		t.Errorf("PreservedSubTasks: %+v", rc.PreservedSubTasks)
	}
}

func TestReplanSetupNode_FullDecisionInstallsContextWithEmptyAccumulated(t *testing.T) {
	dec := &fakeReplanDecider{decision: ReplanDecision{
		Goal:       "augmented goal",
		Trigger:    "full",
		AttemptIdx: 1,
	}}
	node := &ReplanSetupNode{Decider: dec}

	out, err := node.Handle(context.Background(), hermes.HermesState{TaskID: "t", Goal: "orig", Accumulated: "old"})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if out.Reason != "replan_setup_full" {
		t.Errorf("Reason = %q", out.Reason)
	}
	rc := out.Updates[0].Replan
	if rc.Trigger != "full" || rc.Accumulated != "" {
		t.Errorf("expected full reset, got: %+v", rc)
	}
}

func TestReplanSetupNode_NoopDecisionRoutesPlannerWithoutInstall(t *testing.T) {
	dec := &fakeReplanDecider{decision: ReplanDecision{}}
	node := &ReplanSetupNode{Decider: dec}

	out, err := node.Handle(context.Background(), hermes.HermesState{TaskID: "t"})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if out.NextStep != hermes.RuntimeStepPlanner {
		t.Errorf("NextStep = %q", out.NextStep)
	}
	if out.Reason != "replan_setup_noop" {
		t.Errorf("Reason = %q", out.Reason)
	}
	if len(out.Updates) != 0 {
		t.Errorf("expected no updates on no-op: %+v", out.Updates)
	}
}

func TestReplanSetupNode_DeciderErrorPropagates(t *testing.T) {
	dec := &fakeReplanDecider{err: errors.New("boom")}
	node := &ReplanSetupNode{Decider: dec}
	if _, err := node.Handle(context.Background(), hermes.HermesState{TaskID: "t"}); err == nil {
		t.Fatalf("expected error from decider")
	}
}

func TestReplanSetupNode_NilDeciderRejected(t *testing.T) {
	node := &ReplanSetupNode{}
	if _, err := node.Handle(context.Background(), hermes.HermesState{TaskID: "t"}); err == nil {
		t.Fatalf("expected error when Decider is nil")
	}
}

func TestReplanSetupNode_NameMatches(t *testing.T) {
	node := &ReplanSetupNode{Decider: &fakeReplanDecider{}}
	if got := node.Name(); got != hermes.RuntimeStepReplanSetup {
		t.Errorf("Name = %q, want replan_setup", got)
	}
}

func TestMergePreservedSubTasks_RenamesCollidingIDs(t *testing.T) {
	preserved := []hermes.SubTask{
		{ID: "s1", Description: "kept"},
	}
	replanned := []hermes.SubTask{
		{ID: "s1", Description: "duplicate id"},   // collides → rename
		{ID: "", Description: "missing id"},       // empty → rename
		{ID: "fresh", Description: "fresh task"},  // ok
	}
	merged := mergePreservedSubTasks(preserved, replanned, 2)
	if len(merged) != 4 {
		t.Fatalf("merged length = %d, want 4", len(merged))
	}
	if merged[0].ID != "s1" || merged[0].Description != "kept" {
		t.Errorf("preserved misplaced: %+v", merged[0])
	}
	if merged[1].ID != "retry2-s1" {
		t.Errorf("collision rename: %+v", merged[1])
	}
	if merged[2].ID != "retry2-s2" {
		t.Errorf("empty-id rename: %+v", merged[2])
	}
	if merged[3].ID != "fresh" {
		t.Errorf("fresh id mutated: %+v", merged[3])
	}
}

func TestMergePreservedSubTasks_EmptyPreservedReturnsReplannedUnchanged(t *testing.T) {
	in := []hermes.SubTask{{ID: "s1"}, {ID: "s2"}}
	out := mergePreservedSubTasks(nil, in, 1)
	if len(out) != 2 || out[0].ID != "s1" || out[1].ID != "s2" {
		t.Errorf("expected pass-through, got: %+v", out)
	}
}
