---
name: tasks
description: Display and manage development task progress via GitHub Issues. Activated when user says "tasks", "progress", "todo", "what to do", "next step", "任務", "進度", "待辦".
allowed-tools: Bash, Read, Grep, Glob, Write
---

# Tasks - GitHub Issues Task Tracking

## Overview

This skill manages project tasks using **GitHub Issues as the single source of truth**.

**Architecture:**
```
GitHub Issues + Milestones (source of truth)
       ↕ gh CLI
Claude Code Skills/Commands (interactive)
       ↓
docs/MASTER_TASKS.md (auto-generated, read-only)
```

**Conventions:**
- **Milestones** = Phases (e.g., "P1 - Core Backend", "P13 - Future Enhancements")
- **Issues** = Tasks within a phase (assigned to milestone)
- **Task lists in Issue body** = Sub-tasks (`- [x] done`, `- [ ] todo`)
- **Issue state** = open (🔄 進行中) / closed (✅ 已完成)
- **Labels** for refinement: `planning` (📋), `testing` (🧪), `paused` (⏸️)

## When to Use

When the user:
- Says "show tasks", "task progress", "current status"
- Says "what to do", "next step", "todo items"
- Says "查看任務", "任務進度", "要做什麼", "下一步", "待辦"
- Runs `/tasks` or `/tasks <milestone-name>`

## Execution Steps

### 1. Detect Repository

Run `gh repo view --json nameWithOwner -q .nameWithOwner` in the project directory to auto-detect the GitHub repo. No configuration needed.

### 2. Fetch Project Status

Use `gh` CLI to query milestones and issues:

```bash
# List all milestones (= phases) with progress
gh api "repos/{owner}/{repo}/milestones?state=all&sort=title&direction=asc&per_page=100"

# List issues for a specific milestone
gh issue list --milestone "P13 - Future Enhancements" --state all --json number,title,state,labels,body
```

### 3. Display Status Summary

Show all milestones as phases with progress:

For each milestone, calculate:
- **Progress** = closed_issues / (open_issues + closed_issues) × 100%
- **Status emoji**: 100% → ✅, >0% → 🔄, 0% → 📋

Format as a table:
```
📊 專案進度

| Phase | Progress | Status |
|-------|----------|--------|
| P1 - Core Backend | 100% | ✅ |
| P13 - Future | 0% | 📋 |
```

### 4. If User Specifies a Phase

When a specific phase/milestone is mentioned:
1. Fetch issues for that milestone: `gh issue list --milestone "<name>" --state all --json number,title,state,labels,body`
2. Display each issue as a task with status
3. Parse task lists from issue body (`- [x]` / `- [ ]`) as sub-tasks
4. Show completion details

### 5. Suggest Next Steps

Based on open issues:
- Highlight issues with `P0` or `P1` labels (high priority)
- Suggest next logical tasks to work on
- Flag stale issues (open but no recent activity)

## Related Commands

- `/task-sync` — Generate MASTER_TASKS.md from GitHub Issues
- `/task-add` — Create a new GitHub Issue
- `/task-status` — Update issue state (close, label, etc.)
