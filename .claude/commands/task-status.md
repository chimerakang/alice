---
description: Update project status or progress (e.g., /task-status BIL-SVC completed 100)
argument-hint: <project-code> [status] [progress]
allowed-tools: Read, Grep, Glob, Write, Edit
---

# Task Status - Update Progress

## Task

Update status for: **$ARGUMENTS**

## Steps

### 1. Read Configuration

Read `.tasks/config.yaml` for valid status values.

### 2. Parse Arguments

Parse `$ARGUMENTS` as: <project-code> [new-status] [new-progress]

### 3. Load and Update Project

Read `.tasks/projects/<code-lowercase>.yaml`, update:
- `status` field if new status provided
- `progress` field if new progress provided
- `completed_date` automatically when status becomes completed

### 4. Save and Regenerate

Save updated YAML. Remind user to run `npx devtask generate`.
