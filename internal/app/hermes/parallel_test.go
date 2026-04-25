package hermes

import "testing"

func TestIsReadOnlySubTask(t *testing.T) {
	cases := []struct {
		name string
		st   SubTask
		want bool
	}{
		{"empty hints", SubTask{ToolHints: nil}, false},
		{"pure read", SubTask{ToolHints: []string{"Read"}}, true},
		{"read+grep", SubTask{ToolHints: []string{"Read", "Grep"}}, true},
		{"glob", SubTask{ToolHints: []string{"Glob"}}, true},
		{"web fetch", SubTask{ToolHints: []string{"WebFetch"}}, true},
		{"includes Edit", SubTask{ToolHints: []string{"Read", "Edit"}}, false},
		{"bash is not safe", SubTask{ToolHints: []string{"Bash"}}, false},
		{"includes write", SubTask{ToolHints: []string{"Write"}}, false},
		{"includes file_patch", SubTask{ToolHints: []string{"Read", "file_patch"}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsReadOnlySubTask(tc.st); got != tc.want {
				t.Fatalf("got=%v want=%v", got, tc.want)
			}
		})
	}
}

func TestGatherReadOnlyBatch(t *testing.T) {
	tasks := []SubTask{
		{ID: "1", ToolHints: []string{"Read"}},
		{ID: "2", ToolHints: []string{"Grep"}},
		{ID: "3", ToolHints: []string{"Glob"}},
		{ID: "4", ToolHints: []string{"Edit"}},
		{ID: "5", ToolHints: []string{"Read"}},
		{ID: "6", ToolHints: []string{"Read"}},
		{ID: "7", ToolHints: []string{"Read"}},
		{ID: "8", ToolHints: []string{"Read"}},
		{ID: "9", ToolHints: []string{"Read"}},
	}

	// From idx 0: three read-only then Edit blocks
	got := GatherReadOnlyBatch(tasks, 0)
	if len(got) != 3 {
		t.Fatalf("idx 0 batch len got=%d want=3", len(got))
	}

	// From idx 3 (Edit): single non-read-only
	got = GatherReadOnlyBatch(tasks, 3)
	if len(got) != 1 || got[0] != 3 {
		t.Fatalf("idx 3 batch got=%v want=[3]", got)
	}

	// From idx 4: capped at MaxParallelReadOnly even though 5 tasks remain
	got = GatherReadOnlyBatch(tasks, 4)
	if len(got) != MaxParallelReadOnly {
		t.Fatalf("idx 4 batch len got=%d want=%d", len(got), MaxParallelReadOnly)
	}

	// Out of range
	if got := GatherReadOnlyBatch(tasks, 99); got != nil {
		t.Fatalf("out-of-range got=%v want=nil", got)
	}
}
