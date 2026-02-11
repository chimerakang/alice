---
name: tasks
description: Display and manage development task progress. Activated when user says "tasks", "progress", "todo", "what to do", "next step", "任務", "進度", "待辦".
allowed-tools: Read, Grep, Glob
---

# Tasks - Task Tracking Assistant

## When to Use

When the user:
- Says "show tasks", "task progress", "current status"
- Says "what to do", "next step", "todo items"
- Says "查看任務", "任務進度", "要做什麼", "下一步", "待辦"
- Runs `/tasks` or `/tasks <project-code>`

## Execution Steps

### 1. Load Configuration

Read `.tasks/config.yaml` to discover:
- Project name and settings
- Status definitions and their emojis
- Language preference

### 2. Discover All Projects

Scan `.tasks/projects/*.yaml` to find all project definitions.

For each project YAML file, extract:
- `code`: Project code (e.g., BIL-SVC)
- `name`: Project name
- `status`: Current status key (must match config statuses)
- `progress`: Completion percentage (0-100)
- `phases`: Phase breakdown if available

### 3. Display Status Summary

Group projects by status:
- **Active**: statuses listed in `active_statuses` config
- **Completed**: statuses listed in `completed_statuses` config
- **Cancelled**: statuses listed in `cancelled_statuses` config

### 4. If User Specifies a Project Code

When a specific project code is mentioned:
1. Read `.tasks/projects/<code-lowercase>.yaml`
2. Display phase-by-phase breakdown with task statuses
3. If `detail_link` field exists, also read that file for full details
4. Show time estimates vs actuals if available

### 5. Suggest Next Steps

Based on project statuses, suggest:
- Projects needing attention (in_progress with low progress)
- Next logical tasks to work on
