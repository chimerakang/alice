package engine

import "testing"

func TestChatContextInitialStateSnapshot(t *testing.T) {
	ctx := NewChatContext(42, 7, "/repo")

	snapshot := ctx.StateSnapshot()
	if snapshot.Agent != "chat" {
		t.Fatalf("Agent = %q, want chat", snapshot.Agent)
	}
	if snapshot.State != string(ChatStateIdle) {
		t.Fatalf("State = %q, want idle", snapshot.State)
	}
	if snapshot.Since.IsZero() {
		t.Fatal("Since is zero")
	}
}

func TestChatStateTransitions(t *testing.T) {
	ctx := NewChatContext(42, 7, "/repo")

	for _, state := range []ChatState{
		ChatStateReceiving,
		ChatStateRouting,
		ChatStateDispatching,
		ChatStateBusy,
		ChatStateAwaitingInput,
		ChatStateReceiving,
		ChatStateRouting,
		ChatStateIdle,
	} {
		if err := ctx.TransitionState(state, "test"); err != nil {
			t.Fatalf("TransitionState(%q): %v", state, err)
		}
	}
	if got := ctx.StateSnapshot().Reason; got != "test" {
		t.Fatalf("Reason = %q, want test", got)
	}
}

func TestChatStateRejectsInvalidTransition(t *testing.T) {
	ctx := NewChatContext(42, 7, "/repo")
	if err := ctx.TransitionState(ChatStateInterrupting, "test"); err == nil {
		t.Fatal("TransitionState(idle -> interrupting) returned nil, want error")
	}
}

func TestAssistantAwaitsInput(t *testing.T) {
	tests := []struct {
		name string
		text string
		want bool
	}{
		{
			name: "choice question",
			text: "接下來合理的後續：(a) 補 service-level 測試；(b) highlight 鄰居並帶入 nodeId。你想先做哪個？",
			want: true,
		},
		{
			name: "continue question",
			text: "已完成分析。要我繼續嗎？",
			want: true,
		},
		{
			name: "ordinary answer",
			text: "分析完成，node graph 的資料流正常。",
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AssistantAwaitsInput(tt.text); got != tt.want {
				t.Fatalf("AssistantAwaitsInput() = %v, want %v", got, tt.want)
			}
		})
	}
}
