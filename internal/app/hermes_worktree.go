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
		fmt.Fprintf(&b, "⚠️ 啟動 Issue #%d 前，Git 工作樹已經有未提交變更。\n\n", issueNumber)
	} else {
		b.WriteString("⚠️ 啟動 Hermes 前，Git 工作樹已經有未提交變更。\n\n")
	}
	b.WriteString("Hermes 會繼續執行；結束時請確認本次任務沒有混入這些舊變更。\n\n")
	b.WriteString("啟動前已有：\n")
	for i, change := range changes {
		if i >= maxShown {
			fmt.Fprintf(&b, "- …另有 %d 筆\n", len(changes)-maxShown)
			break
		}
		fmt.Fprintf(&b, "- `%s`\n", change)
	}
	return b.String()
}
