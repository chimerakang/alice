---
description: Add a new project or task to tracking (e.g., /task-add NEW-PROJECT)
argument-hint: <project-code> [task-description]
allowed-tools: Read, Grep, Glob, Write, Edit
---

# Task Add - Add Project or Task

## Task

Add a new item to task tracking: **$ARGUMENTS**

## Steps

### 1. Read Configuration

Read `.tasks/config.yaml` for status definitions and valid status keys.

### 2. Determine Action

If `$ARGUMENTS` is a new project code (no matching file in `.tasks/projects/`):
- Create a new project YAML file at `.tasks/projects/<code-lowercase>.yaml`
- Ask user for: name, description, status, estimated hours
- Use the ProjectSchema format:
  ```yaml
  code: NEW-CODE
  name: "Project Name"
  status: planning
  progress: 0
  description: "What this project does"
  ```

If matching an existing project:
- Read the existing YAML
- Add a new phase or task to it
- Save the updated YAML

### 3. Remind to Regenerate

After changes, tell user to run `npx devtask generate` to update MASTER_TASKS.md.
