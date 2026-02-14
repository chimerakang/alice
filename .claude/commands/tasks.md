---
description: Show project tasks and progress from GitHub Issues (e.g., /tasks or /tasks P13)
argument-hint: [phase-name]
allowed-tools: Bash, Read, Grep, Glob
---

# Tasks — Show Project Progress from GitHub Issues

## Task

Show development task progress. Filter: **$ARGUMENTS**

## Steps

### 1. Detect Repository

```bash
gh repo view --json nameWithOwner -q .nameWithOwner
```

### 2. Show Overview (No Arguments)

If `$ARGUMENTS` is empty, show all milestones as a summary:

```bash
gh api "repos/{owner}/{repo}/milestones?state=all&sort=title&direction=asc&per_page=100" \
  --jq '.[] | "\(.title)\t\(.open_issues)\t\(.closed_issues)\t\(.description)"'
```

Calculate progress for each: `closed / (open + closed) * 100`

Display as:
```
📊 專案進度 ({repo_name})

| Phase | Description | Progress | Status |
|-------|-------------|----------|--------|
| P1 - Core Backend | Telegram Bot + CLI | 100% | ✅ |
| P13 - Future | 未來增強 | 0% | 📋 |

📋 Open issues: {count}  ✅ Closed: {count}  Total: {count}
```

### 3. Show Phase Detail (With Arguments)

If `$ARGUMENTS` specifies a phase (e.g., "P13", "P8.5"):

```bash
# Find the milestone matching the argument
gh api "repos/{owner}/{repo}/milestones?state=all&per_page=100" \
  --jq '.[] | select(.title | test("^P13"; "i"))'

# Fetch issues for that milestone
gh issue list --milestone "{milestone_title}" --state all \
  --json number,title,state,labels,body,assignees,updatedAt
```

Display each issue as a task:
```
📋 P13 - Future Enhancements (📋 0%)

| # | Task | Status | Labels |
|---|------|--------|--------|
| #38 | 多模型支持 | 🔄 open | enhancement |
| #39 | GitHub Actions 整合 | 📋 open | planning |

Sub-tasks for #38:
  ✅ Research multi-model API patterns
  📋 Implement model switching logic
  📋 Add configuration options
```

### 4. Suggest Next Steps

After displaying tasks:
- Highlight high-priority items (P0/P1 labels)
- Suggest running `/task-sync` if MASTER_TASKS.md is outdated
- Mention `/task-add` to create new tasks
