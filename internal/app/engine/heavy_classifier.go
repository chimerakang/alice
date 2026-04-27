package engine

import (
	"strings"

	"claude-tg-agent/internal/app/hermes"
)

// heavyToolHints lists Claude Code tool names that indicate code-modifying work.
// A sub-task whose tool_hints include any of these is treated as "heavy".
var heavyToolHints = map[string]struct{}{
	"Edit":         {},
	"Write":        {},
	"MultiEdit":    {},
	"NotebookEdit": {},
	"file_patch":   {},
}

// heavyDescriptionTokens triggers the heavy executor when present in the
// sub-task description (case-insensitive substring match).
var heavyDescriptionTokens = []string{
	"refactor", "implement", "rewrite", "redesign", "migrate",
	"重構", "重寫", "實作", "實現", "修正", "修復",
}

// IsHeavySubTask reports whether a sub-task is "heavy" — substantive code
// work that benefits from a stronger executor model. A sub-task is heavy
// when it has a heavy tool_hint or its description contains an
// implementation verb. Otherwise it is "light" (read / inspect / report).
func IsHeavySubTask(t hermes.SubTask) bool {
	for _, hint := range t.ToolHints {
		if _, ok := heavyToolHints[hint]; ok {
			return true
		}
	}
	desc := strings.ToLower(t.Description)
	for _, kw := range heavyDescriptionTokens {
		if strings.Contains(desc, kw) {
			return true
		}
	}
	return false
}

// SubTaskBindable is implemented by runners that want to receive the current
// sub-task before each Run call (e.g. to switch executor models per task).
// PlanExecuteEngine type-asserts this on its DirectEngine's runner and binds
// the sub-task before invoking Run.
type SubTaskBindable interface {
	BindSubTask(hermes.SubTask)
}
