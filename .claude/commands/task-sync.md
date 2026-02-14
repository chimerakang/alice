---
description: Sync GitHub Issues → MASTER_TASKS.md (e.g., /task-sync or /task-sync --dry-run)
argument-hint: [--dry-run]
allowed-tools: Bash, Read, Write, Glob
---

# Task Sync — Generate MASTER_TASKS.md from GitHub Issues

## Task

Regenerate `docs/MASTER_TASKS.md` from GitHub Issues and Milestones. Args: **$ARGUMENTS**

## Prerequisites

- `gh` CLI must be installed and authenticated
- Repository must have GitHub Milestones set up (Milestones = Phases)

## Steps

### 1. Detect Repository

```bash
gh repo view --json nameWithOwner -q .nameWithOwner
```

This auto-detects the repo. No config file needed.

### 2. Fetch All Milestones

```bash
gh api "repos/{owner}/{repo}/milestones?state=all&sort=title&direction=asc&per_page=100"
```

Each milestone represents a Phase. Extract:
- `title`: Phase name (e.g., "P1 - Core Backend")
- `description`: Phase description
- `open_issues` + `closed_issues`: For progress calculation
- `number`: For fetching issues

### 3. Fetch Issues Per Milestone

For each milestone:
```bash
gh api "repos/{owner}/{repo}/issues?milestone={number}&state=all&sort=created&direction=asc&per_page=100"
```

For each issue, extract:
- `number`, `title`, `state` (open/closed), `html_url`
- `labels[].name`: For status refinement and priority
- `body`: Parse `- [x]` / `- [ ]` task lists as sub-tasks
- `closed_at`: For completion date

### 4. Status Mapping

Map issue state + labels to emoji:

| Condition | Emoji | Label |
|-----------|-------|-------|
| Issue closed | ✅ | 已完成 |
| Label contains `testing` | 🧪 | 測試中 |
| Label contains `paused` | ⏸️ | 暫停 |
| Label contains `planning` | 📋 | 規劃中 |
| Issue open (default) | 🔄 | 開發中 |

Phase status:
- All issues closed → ✅
- Some issues closed → 🔄
- No issues closed → 📋

### 5. Generate MASTER_TASKS.md

Write the file with this structure:

```markdown
# {Project Name} - Master Tasks

> {Project description}
> Last updated: {current date}
> Auto-generated from GitHub Issues — do not edit manually.
> Run `/task-sync` to regenerate.

## Status Legend

| Status | Label |
|--------|-------|
| 📋 | 規劃中 |
| 🔄 | 開發中 |
| 🧪 | 測試中 |
| ✅ | 已完成 |
| ⏸️ | 暫停 |
| ❌ | 已取消 |

## Phase Overview

| Phase | Description | Progress | Status |
|-------|-------------|----------|--------|
| {milestone.title} | {milestone.description} | {progress}% | {status_emoji} |
...

---

## {milestone.title} ({status_emoji} {progress}%)

| # | Task | Issue | Status |
|---|------|-------|--------|
| {phase_num}.{seq} | **{issue.title}** | [#{number}]({html_url}) | {emoji} |
| | — {sub_task_text} | | {sub_emoji} |
...

(repeat for each milestone)

---

## Issue Tracker

| Issue | Title | Phase | Status |
|-------|-------|-------|--------|
| [#{number}]({url}) | {title} | {milestone} | {state_emoji} |
...
```

### 6. Phase Number Extraction

Extract phase number from milestone title for task IDs:
- "P1 - Core Backend" → phase prefix "P1", task IDs: 1.1, 1.2, ...
- "P8.5 - TG Enhancement" → phase prefix "P8.5", task IDs: 8.5.1, 8.5.2, ...
- "P13 - Future" → phase prefix "P13", task IDs: 13.1, 13.2, ...

### 7. Dry Run Mode

If `$ARGUMENTS` contains `--dry-run`:
- Display the generated content to the user
- Do NOT write the file
- Show what would change

### 8. Output

If not dry-run:
- Write to `docs/MASTER_TASKS.md` using the Write tool
- Report: number of milestones, total issues, open/closed counts
- Remind user to commit: `git add docs/MASTER_TASKS.md && git commit -m "📋 Sync MASTER_TASKS.md from GitHub Issues"`
