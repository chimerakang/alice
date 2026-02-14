---
description: Create a new GitHub Issue as a task (e.g., /task-add P13 新功能描述)
argument-hint: <phase> <title> [--body <description>]
allowed-tools: Bash, Read
---

# Task Add — Create GitHub Issue

## Task

Create a new GitHub Issue and assign to a milestone: **$ARGUMENTS**

## Steps

### 1. Parse Arguments

Parse `$ARGUMENTS` as: `<phase> <title> [--body <description>]`

- Phase: milestone name prefix (e.g., "P13", "P8.5")
- Title: issue title
- Body (optional): detailed description

If only a title is given without a phase, ask the user which milestone to assign it to.

### 2. Detect Repository

```bash
gh repo view --json nameWithOwner -q .nameWithOwner
```

### 3. Find Milestone

Look up the milestone matching the phase prefix:

```bash
gh api "repos/{owner}/{repo}/milestones?state=all&per_page=100" \
  --jq '.[] | select(.title | startswith("P13"))'
```

If no matching milestone exists, ask the user if they want to create one:
```bash
gh api "repos/{owner}/{repo}/milestones" -X POST \
  -f title="P14 - New Phase" -f description="Phase description"
```

### 4. Create Issue

```bash
gh issue create \
  --title "{title}" \
  --body "{body_with_task_list}" \
  --milestone "{milestone_title}" \
  --label "enhancement"
```

If the user provides sub-tasks, format the body with a task list:
```markdown
## Tasks

- [ ] Sub-task 1
- [ ] Sub-task 2
- [ ] Sub-task 3
```

### 5. Confirm and Suggest

After creation:
- Show the issue URL
- Suggest running `/task-sync` to update MASTER_TASKS.md
- Mention `/task-status` to update progress later

### 6. Label Convention

Apply appropriate labels based on context:
- `enhancement` — new feature
- `bug` — bug fix
- `documentation` — docs work
- Priority labels: `P0` (highest), `P1` (high), `P2` (medium)
