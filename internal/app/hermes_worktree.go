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

func formatHermesDirtyWorktreeWarning(issueNumber int, changes []string) string {
	const maxShown = 8

	var b strings.Builder
	if issueNumber > 0 {
		fmt.Fprintf(&b, "⚠️ Git 工作樹目前有未提交變更；Hermes 會以此作為 Issue #%d 的啟動 baseline 繼續執行。\n\n", issueNumber)
	} else {
		b.WriteString("⚠️ Git 工作樹目前有未提交變更；Hermes 會以此作為啟動 baseline 繼續執行。\n\n")
	}
	b.WriteString("請注意：executor 會看見這些既有變更；最後驗證時應比對本次新增/修改內容，並確認工作樹沒有混入無關任務。\n\n")
	b.WriteString("啟動 baseline 偵測到：\n")
	for i, change := range changes {
		if i >= maxShown {
			fmt.Fprintf(&b, "- …另有 %d 筆\n", len(changes)-maxShown)
			break
		}
		fmt.Fprintf(&b, "- `%s`\n", change)
	}
	return b.String()
}
