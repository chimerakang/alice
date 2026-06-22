# Hermes Task State Management

This document describes the Hermes task-state persistence layer implemented in `internal/app/hermes/` for [issue #97](https://github.com/chimerakang/alice/issues/97).

---

## Overview

Hermes is Alice's long-task engine. In the current runtime architecture it
should be treated as the **Task Agent** implementation, not as the owner of chat
routing, backend session policy, or retry strategy.

Hermes may use multiple backend sessions per task:
- **Planner** — owns task breakdown and may resume within a task/re-plan path.
- **Executor** — executes sub-tasks through the shared execution path.
- **Reviewer** — optional quality gate, treated as an execution/recovery phase.

The state layer bridges these roles. The Planner writes the plan; the Executor
reads only what it needs (goal, accumulated summary, current sub-task).

---

## Data Model

### TaskState

Top-level record for one Hermes planning session.

| Field | Type | Description |
|---|---|---|
| `ID` | `string` | UUID |
| `ChatID` | `int64` | Telegram chat |
| `PlannerSessionID` | `string` | Claude Code `--resume` session ID |
| `Goal` | `string` | Original user goal |
| `Plan` | `[]SubTask` | Ordered list of atomic tasks |
| `CurrentIdx` | `int` | Index of the sub-task currently executing |
| `Accumulated` | `string` | Rolling executor summary (see below) |
| `Artifacts` | `[]Artifact` | Files changed by executors |
| `Status` | `TaskStatus` | `planning / executing / done / failed / interrupted` |
| `InterruptedBy` | `*int64` | Telegram message ID that triggered interrupt |
| `TokenBudget` | `TokenBudget` | Task-level resource limits |

`validating` is not a current public task state. Review/validation is a phase
inside `executing`.

### SubTask

```go
type SubTask struct {
    ID          string
    Description string
    ToolHints   []string      // suggested tools from Planner
    Status      SubTaskStatus // pending / in_progress / done / skipped / failed
    Result      string        // short summary written by Executor
    Attempts    int
    TokensUsed  int
}
```

### Artifact

```go
type Artifact struct {
    Path      string  // relative to project root
    Hash      string  // SHA-256 hex of file after write
    SubTaskID string
}
```

### TokenBudget

```go
type TokenBudget struct {
    MaxTotalTokens      int       // 0 = unlimited
    MaxWallclockSeconds int       // 0 = unlimited
    UsedTokens          int
    StartedAt           time.Time
}
```

Call `budget.Exceeded()` at the start of each coordinator step.

---

## Storage

### SQLite Tables

**`hermes_task_states`** — one row per task, plan stored as JSON blob.

**`hermes_task_artifacts`** — one row per artifact, foreign keyed to task.

Tables are created automatically by `NewSQLiteTaskStore` and are prefixed with `hermes_` to avoid conflicts with existing Alice tables.

### Sharing the Database Connection

Pass the existing `*sql.DB` from `SQLiteStorage`:

```go
// In app init:
taskStore, err := hermes.NewSQLiteTaskStore(sqliteStorage.GetDB().(*sql.DB))
```

The store uses the same `execWithRetry` pattern (5 retries, 50 ms backoff) as the rest of the Alice storage layer.

### Known Constraint: Single Connection

SQLite is configured with `MaxOpenConns(1)`. Code that iterates rows must close them **before** issuing any sub-queries — otherwise a deadlock occurs. `ListTasksForChat` handles this by collecting task stubs first, closing the rows, then loading artifacts.

---

## TaskStateStore Interface

```go
type TaskStateStore interface {
    CreateTask(task TaskState) (TaskState, error)
    GetTask(id string) (TaskState, error)
    GetActiveTaskForChat(chatID int64) (TaskState, error)

    UpdateSubTask(taskID string, idx int, status SubTaskStatus, result string, tokensUsed int) error
    AdvanceTask(taskID string, nextIdx int, status TaskStatus) error
    AppendArtifact(taskID string, artifact Artifact) error
    UpdateAccumulated(taskID string, accumulated string) error
    UpdatePlannerSession(taskID string, sessionID string) error
    MarkInterrupted(taskID string, messageID int64) error
    MarkStatus(taskID string, status TaskStatus) error
    AddTokenUsage(taskID string, delta int) error

    ListTasksForChat(chatID int64, limit int) ([]TaskState, error)
}
```

---

## Accumulated Summary

The `Accumulated` field is the rolling context passed to each cold-start Executor.

### Cheap Path (default)

After each `SubTask.Done`, append `SubTask.Result` to `Accumulated`, then FIFO-truncate to **2 KB** (keeping the tail — the most recent content).

```go
updated, needsCompression := hermes.AppendResult(
    state.Accumulated, subtask.Result, completedCount, cfg)
store.UpdateAccumulated(taskID, updated)
```

### Expensive Path (compression)

Triggered when:
- `len(Accumulated) > 8 KB`, **or**
- Completed sub-task count ≥ 10

When triggered, `needsCompression = true` is returned. The **Coordinator** (implemented in #98) sends `CompressPrompt(CompressRequest{...})` to the Planner, gets a condensed summary back, and calls `UpdateAccumulated` with the result.

Thresholds are configurable:

```json
{
  "hermes": {
    "hooks": {
      "cheap_max_bytes": 2048,
      "expensive_trigger_kb": 8,
      "expensive_trigger_n": 10
    }
  }
}
```

---

## Executor Prompt Assembly

`BuildExecutorPrompt(state, coreRules)` produces the context injected at the top of each cold-start Executor invocation:

```
=== Hermes Executor Context ===
Goal: <goal>

Accumulated summary:
<accumulated>

Artifacts so far:
  internal/auth/auth.go [deadbeef]

Current sub-task (2/5): write unit tests for auth.go
Suggested tools: Edit, Bash
Budget: tokens=3500/100000 elapsed=12s/300s
```

No full conversation history is injected — skills are loaded automatically by Claude Code CLI.

---

## Interrupt Handling

Current Hermes behavior is fixed to the former `inject` policy: user feedback
that arrives while a task is active is appended to task context and execution
continues. Queue/abort/interrupt policy selection is no longer part of Hermes
task state.

Future interrupt classification belongs to the Chat Agent FSM. Hermes should
only receive explicit task events such as `UserFeedbackReceived` or
`InterruptRequested`.

---

## Token Budget Enforcement

At the start of each coordinator step (before dispatching an Executor):

```go
state, _ := store.GetTask(taskID)
if state.TokenBudget.Exceeded() {
    store.MarkStatus(taskID, hermes.TaskStatusFailed)
    // notify user
    return
}
```

After each Executor completes, report tokens back:

```go
store.AddTokenUsage(taskID, response.TokensUsed)
```

---

## Configuration

```json
{
  "hermes": {
    "enabled": false,
    "hooks": {
      "path_guard": true,
      "post_validators": ["go_build"]
    }
  }
}
```

Hermes mode is opt-in (`enabled: false` by default). Path guard is automatically active when Hermes mode is enabled.

---

## Related

- [hooks.md](hooks.md) — tool execution hooks (#96, prerequisite)
- Issue [#98](https://github.com/chimerakang/alice/issues/98) — Planner-Executor dual CLI sessions
- Issue [#99](https://github.com/chimerakang/alice/issues/99) — Prompt core rules prepend
