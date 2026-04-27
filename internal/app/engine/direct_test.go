package engine

import (
	"context"
	"reflect"
	"testing"
)

type fakeDirectRunner struct {
	updates []directUpdate
}

type directUpdate struct {
	text   string
	silent bool
}

func (r fakeDirectRunner) Run(userMessage string, onUpdate func(string, bool)) (string, error) {
	for _, update := range r.updates {
		onUpdate(update.text, update.silent)
	}
	return "done", nil
}

type metricsRunner struct {
	model    string
	inTokens int
	outToken int
}

func (m metricsRunner) Run(string, func(string, bool)) (string, error) {
	return "ok", nil
}

func (m metricsRunner) LastCallMetrics() (string, int, int) {
	return m.model, m.inTokens, m.outToken
}

func TestDirectEngine_PopulatesModelAndTokensFromMetricsProvider(t *testing.T) {
	runner := metricsRunner{model: "claude-haiku-4-5", inTokens: 1234, outToken: 567}
	engine := NewDirectEngine(runner)
	res, err := engine.Run(context.Background(), "do thing", nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Model != "claude-haiku-4-5" || res.InputTokens != 1234 || res.OutputTokens != 567 {
		t.Fatalf("expected metrics propagated, got %+v", res)
	}
}

func TestDirectEngine_NoMetricsProviderLeavesZeroMetrics(t *testing.T) {
	engine := NewDirectEngine(fakeDirectRunner{})
	res, err := engine.Run(context.Background(), "do thing", nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Model != "" || res.InputTokens != 0 || res.OutputTokens != 0 {
		t.Fatalf("expected zero metrics for runner without provider, got %+v", res)
	}
}

type recordingSink struct {
	events []string
}

type recordingDirectReviewPhase struct {
	calls int
	last  ReviewRequest
}

func (r *recordingDirectReviewPhase) Review(ctx context.Context, req ReviewRequest) (ReviewResult, error) {
	r.calls++
	r.last = req
	return ReviewResult{
		Verdict:      VerdictPass,
		OverallScore: 91,
		Feedback:     "review ok",
		IssueTags:    []ReviewTag{"missing_validation"},
	}, nil
}

func (s *recordingSink) OnSubTaskStart(idx, total int, desc string) {
	s.events = append(s.events, "start")
}

func (s *recordingSink) OnToolUse(tool string, input map[string]any) {
	s.events = append(s.events, "tool-use:"+tool)
}

func (s *recordingSink) OnContent(kind, text string) {
	s.events = append(s.events, "content:"+kind+":"+text)
}

func (s *recordingSink) OnSubTaskDone(idx int, result string) {
	s.events = append(s.events, "done:"+result)
}

func (s *recordingSink) OnComplete(summary string) {
	s.events = append(s.events, "complete:"+summary)
}

func TestDirectEngineProgressEventOrder(t *testing.T) {
	sink := &recordingSink{}
	engine := NewDirectEngine(fakeDirectRunner{updates: []directUpdate{
		{text: "processing", silent: false},
		{text: "Read file", silent: true},
	}})

	result, err := engine.Run(context.Background(), "goal", nil, sink)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Text != "done" {
		t.Fatalf("result text: got %q", result.Text)
	}

	want := []string{
		"start",
		"content:status:processing",
		"tool-use:Read file",
		"done:done",
		"complete:done",
	}
	if !reflect.DeepEqual(sink.events, want) {
		t.Fatalf("events:\n got %#v\nwant %#v", sink.events, want)
	}
}

func TestDirectEngineSkipsReviewWithoutTrigger(t *testing.T) {
	reviewPhase := &recordingDirectReviewPhase{}
	engine := NewDirectEngine(fakeDirectRunner{}, WithReviewPhase(reviewPhase))

	result, err := engine.Run(context.Background(), "normal task", nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Review != nil {
		t.Fatalf("review should be nil, got %+v", result.Review)
	}
	if reviewPhase.calls != 0 {
		t.Fatalf("review calls = %d, want 0", reviewPhase.calls)
	}
}

func TestDirectEngineRunsReviewWithExplicitTrigger(t *testing.T) {
	reviewPhase := &recordingDirectReviewPhase{}
	engine := NewDirectEngine(fakeDirectRunner{}, WithReviewPhase(reviewPhase))

	result, err := engine.Run(context.Background(), "/review improve login flow", nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if reviewPhase.calls != 1 {
		t.Fatalf("review calls = %d, want 1", reviewPhase.calls)
	}
	if got := reviewPhase.last.Goal; got != "improve login flow" {
		t.Fatalf("review goal = %q, want cleaned goal", got)
	}
	if got := reviewPhase.last.SubTaskResults; len(got) != 1 || got[0].Result != "done" {
		t.Fatalf("unexpected review payload: %+v", got)
	}
	if result.Review == nil || result.Review.Verdict != VerdictPass {
		t.Fatalf("review result = %+v", result.Review)
	}
}
