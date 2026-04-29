package app

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const hermesWorktreeCheckTimeout = 10 * time.Second

func checkHermesCleanWorktree(ctx context.Context, projectDir string) ([]string, error) {
	if strings.TrimSpace(projectDir) == "" {
		return nil, fmt.Errorf("project directory is empty")
	}

	checkCtx, cancel := context.WithTimeout(ctx, hermesWorktreeCheckTimeout)
	defer cancel()

	output, err := runProcessOutput(checkCtx, ProcessOptions{Dir: projectDir}, "git", "status", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("git status --porcelain failed: %w", err)
	}

	rawStatus := string(output)
	if strings.TrimSpace(rawStatus) == "" {
		return nil, nil
	}

	lines := strings.Split(strings.TrimRight(rawStatus, "\r\n"), "\n")
	changes := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		changes = append(changes, line)
	}
	return changes, nil
}

func formatHermesDirtyWorktreeMessage(changes []string) string {
	const maxShown = 12

	var b strings.Builder
	b.WriteString("⚠️ Hermes 未啟動：目前工作樹有未提交或未追蹤的變更。\n\n")
	b.WriteString("為避免 executor 把先前任務殘留誤判為本次修改，請先 commit、stash 或清理這些檔案後再執行 `/hermes`。\n\n")
	b.WriteString("目前偵測到：\n")
	for i, change := range changes {
		if i >= maxShown {
			fmt.Fprintf(&b, "- …另有 %d 筆\n", len(changes)-maxShown)
			break
		}
		fmt.Fprintf(&b, "- `%s`\n", change)
	}
	return b.String()
}
