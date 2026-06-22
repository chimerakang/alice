package hermes

// readOnlyTools lists Claude Code tool names whose execution cannot mutate
// project state. A sub-task whose ToolHints is a non-empty subset of this set
// is safe to run concurrently with other read-only sub-tasks because parallel
// reads cannot conflict on the file system.
//
// Bash is intentionally NOT here: even an "innocent-looking" bash invocation
// can shell out to git commit, npm install, etc. The classifier needs to err
// on the side of "this might write" — only explicitly read-only tools qualify.
var readOnlyTools = map[string]struct{}{
	"Read": {}, "Grep": {}, "Glob": {}, "WebFetch": {}, "WebSearch": {},
	"NotebookRead": {},
}

// IsReadOnlySubTask reports whether a sub-task is safe to execute in parallel
// with other read-only sub-tasks. A sub-task qualifies when:
//   - ToolHints is non-empty (no hints → cannot prove it is read-only), AND
//   - every hint is a member of readOnlyTools.
func IsReadOnlySubTask(t SubTask) bool {
	if len(t.ToolHints) == 0 {
		return false
	}
	for _, hint := range t.ToolHints {
		if _, ok := readOnlyTools[hint]; !ok {
			return false
		}
	}
	return true
}

// MaxParallelReadOnly bounds how many read-only sub-tasks may run concurrently
// in one batch. Each one cold-starts a CLI session, so memory and process
// pressure rises linearly. Four is a safe ceiling on a 16 GB machine and
// usually saturates I/O for repo-wide grep/read patterns.
const MaxParallelReadOnly = 4

// GatherReadOnlyBatch returns the slice of contiguous indices, starting at
// startIdx, whose sub-tasks are all read-only. Length is capped at
// MaxParallelReadOnly. Returns at least one index — the caller then decides
// whether the batch is large enough to warrant goroutine dispatch.
func GatherReadOnlyBatch(tasks []SubTask, startIdx int) []int {
	if startIdx < 0 || startIdx >= len(tasks) {
		return nil
	}
	if !IsReadOnlySubTask(tasks[startIdx]) {
		return []int{startIdx}
	}
	out := make([]int, 0, MaxParallelReadOnly)
	for i := startIdx; i < len(tasks) && len(out) < MaxParallelReadOnly; i++ {
		if !IsReadOnlySubTask(tasks[i]) {
			break
		}
		out = append(out, i)
	}
	return out
}
