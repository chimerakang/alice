---
description: Update GitHub Issue status (e.g., /task-status 38 close, /task-status 38 label testing)
argument-hint: <issue-number> <action> [value]
allowed-tools: Bash, Read
---

# Task Status — Update GitHub Issue

## Task

Update status for: **$ARGUMENTS**

## Steps

### 1. Parse Arguments

Parse `$ARGUMENTS` as: `<issue-number> <action> [value]`

Supported actions:
- `close` — Close the issue (mark as completed ✅)
- `reopen` — Reopen a closed issue
- `label <name>` — Add a label (e.g., `testing`, `planning`, `paused`)
- `unlabel <name>` — Remove a label
- `assign <user>` — Assign to a user
- `milestone <name>` — Move to a different milestone/phase
- `progress` — Show task list completion from issue body
- `check <text>` — Check off a sub-task in the issue body
- `uncheck <text>` — Uncheck a sub-task in the issue body

### 2. Detect Repository

```bash
gh repo view --json nameWithOwner -q .nameWithOwner
```

### 3. Execute Action

#### Close Issue
```bash
gh issue close {number} --comment "✅ Completed"
```

#### Reopen Issue
```bash
gh issue reopen {number}
```

#### Add/Remove Label
```bash
gh issue edit {number} --add-label "{label}"
gh issue edit {number} --remove-label "{label}"
```

#### Change Milestone
```bash
gh issue edit {number} --milestone "{milestone_title}"
```

#### Check/Uncheck Sub-task
1. Fetch issue body: `gh issue view {number} --json body -q .body`
2. Find the matching task list item
3. Toggle `- [ ]` ↔ `- [x]`
4. Update: `gh issue edit {number} --body "{updated_body}"`

#### Show Progress
```bash
gh issue view {number} --json body,title,state,labels -q '.'
```
Parse task list items and show completion:
```
📊 Issue #38: 多模型支持
Status: 🔄 Open
Progress: 2/5 (40%)
  ✅ Research multi-model API patterns
  ✅ Design abstraction layer
  📋 Implement model switching logic
  📋 Add configuration options
  📋 Write tests
```

### 4. Confirm and Suggest

After update:
- Show the updated issue state
- Suggest running `/task-sync` to update MASTER_TASKS.md
- If closing an issue, check if all issues in the milestone are now closed (phase complete!)
