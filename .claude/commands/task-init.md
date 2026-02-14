---
description: Initialize GitHub Milestones for task tracking (one-time setup per project)
argument-hint: [--force]
allowed-tools: Bash, Read, Glob
---

# Task Init — Setup GitHub Milestones

## Task

Initialize GitHub Milestones (= Phases) for task tracking. Args: **$ARGUMENTS**

This is a one-time setup command. It creates milestones and assigns existing issues to the correct milestones based on the project's MASTER_TASKS.md structure.

## Steps

### 1. Detect Repository

```bash
gh repo view --json nameWithOwner -q .nameWithOwner
```

### 2. Check Existing Milestones

```bash
gh api "repos/{owner}/{repo}/milestones?state=all&per_page=100" --jq '.[].title'
```

If milestones already exist and `$ARGUMENTS` does not contain `--force`, show the existing milestones and ask if the user wants to proceed.

### 3. Read Current Structure

Read `docs/MASTER_TASKS.md` to discover the existing phase structure:
- Look for the "Phase Overview" table
- Extract phase names and descriptions (e.g., "P1 - Core Backend | Telegram Bot + CLI 整合")
- Note which phases are completed (✅) vs active

Also read the "Issue Tracker" section to find existing issue-to-phase mappings.

### 4. Create Milestones

For each phase found in MASTER_TASKS.md, create a GitHub Milestone:

```bash
gh api "repos/{owner}/{repo}/milestones" -X POST \
  -f title="P1 - Core Backend" \
  -f description="Telegram Bot + Claude CLI 整合" \
  -f state="closed"   # closed if phase is 100% complete
```

- Completed phases (✅ 100%) → create with `state: "closed"`
- Active/planned phases → create with `state: "open"`
- Skip if milestone with same title already exists (idempotent)

### 5. Assign Issues to Milestones

For each issue found in the Issue Tracker section of MASTER_TASKS.md:

```bash
gh issue edit {number} --milestone "{milestone_title}"
```

Map issues to milestones based on the Phase column in the Issue Tracker table.

### 6. Report

Show summary:
- Number of milestones created
- Number of issues assigned
- Suggest running `/task-sync` to regenerate MASTER_TASKS.md from the new structure
- Suggest running `/tasks` to verify the result
