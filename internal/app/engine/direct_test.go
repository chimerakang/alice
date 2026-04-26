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

type recordingSink struct {
	events []string
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
