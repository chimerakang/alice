package hermes

import "testing"

func TestValidTaskStatusTransition(t *testing.T) {
	tests := []struct {
		from TaskStatus
		to   TaskStatus
		want bool
	}{
		{TaskStatusPlanning, TaskStatusExecuting, true},
		{TaskStatusExecuting, TaskStatusExecuting, true},
		{TaskStatusExecuting, TaskStatusDone, true},
		{TaskStatusExecuting, TaskStatusFailed, true},
		{TaskStatusDone, TaskStatusExecuting, false},
		{TaskStatusFailed, TaskStatusExecuting, false},
		{TaskStatusInterrupted, TaskStatusPlanning, false},
		{TaskStatusDone, TaskStatusDone, true},
	}

	for _, tt := range tests {
		if got := ValidTaskStatusTransition(tt.from, tt.to); got != tt.want {
			t.Fatalf("ValidTaskStatusTransition(%q, %q) = %v, want %v", tt.from, tt.to, got, tt.want)
		}
	}
}
