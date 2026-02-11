---
description: Show project tasks and progress (e.g., /tasks or /tasks BIL-SVC)
argument-hint: [project-code]
allowed-tools: Read, Grep, Glob
---

# Tasks - Show Project Progress

## Task

Show development task progress. Project filter: **$ARGUMENTS**

## Steps

### 1. Read Configuration

Read `.tasks/config.yaml` to get project settings and status definitions.

### 2. Discover Projects

Scan `.tasks/projects/*.yaml` to find all project YAML files.

### 3. Display Results

If `$ARGUMENTS` is empty:
- Show summary table of ALL projects grouped by status
- Include progress percentage

If `$ARGUMENTS` specifies a project code:
- Read `.tasks/projects/<code-lowercase>.yaml`
- Show detailed phase and task breakdown
- If project has `detail_link`, also read that file for full details

## Output Format

Use a clear table showing: project code, name, status emoji, progress %.
For detailed view: phase breakdown with task statuses.
