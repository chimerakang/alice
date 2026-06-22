# Hermes Architecture: Brain-Executor Collaboration

This document describes the Hermes Planner-Executor architecture implemented in
`internal/app/hermes/` for [issue #98](https://github.com/chimerakang/alice/issues/98).
For the current runtime ownership model, also see
[../arch/agent-fsm-architecture.md](../arch/agent-fsm-architecture.md).

---

## Overview

Hermes transforms Alice from a **single-model routing** system into a
**long-task Task Agent** with Planner/Executor/Reviewer roles:

| Mode | Model allocation | Context management |
|---|---|---|
| Classic (non-Hermes) | One model per message, chosen by triage | Sticky session |
| **Hermes** | Planner plans; Executor executes; Reviewer may gate quality | Session policy is owned outside Hermes |

The key architecture boundary: Hermes owns durable task progress; it does not
own chat follow-up state, sticky session policy, or global recovery rules.

---

## Component Map

```
User message
    ↓
┌─ TelegramBot ────────────────────────────────────────┐
│  Chat Agent routes long-task intent → startHermesTask() │
└────────────────────────┬─────────────────────────────┘
                         ↓
┌─ Coordinator (hermes/coordinator.go) ───────────────┐
│  Goroutine: plan → execute loop → done              │
│  Budget check at each step                          │
│  Calls ProgressReporter for Telegram notifications  │
└──────────┬────────────────────────┬─────────────────┘
           ↓                        ↓
┌─ Planner Role ──────┐  ┌─ Executor Role ────────────┐
│  planner.go         │  │  executor.go               │
│  CLIClient (Opus)   │  │  CLIClient (Haiku)         │
│  task planning      │  │  model execution           │
│  JSON parse + retry │  │  BuildExecutorPrompt()     │
└─────────────────────┘  └────────────────────────────┘
           │                        │
           └──────────┬─────────────┘
                      ↓
           ┌─ TaskStateStore ────────┐
           │  SQLiteTaskStore       │
           │  hermes_task_states    │
           │  hermes_task_artifacts │
           └────────────────────────┘
```

---

## File Structure

```
internal/app/hermes/
  state.go        TaskState, SubTask, Artifact, TokenBudget, lifecycle enums
  store.go        TaskStateStore interface + SQLiteTaskStore implementation
  noop_store.go   NoopTaskStore (fallback when DB unavailable)
  accumulated.go  AppendResult, CompressPrompt, BuildExecutorPrompt
  hooks.go        PreHook, PostHook, PathGuard, GoBuild, TscCheck, JsonLint
  planner.go      PlannerSession — long-lived CLI session, JSON parse/retry
  coordinator.go  Coordinator — task lifecycle goroutine and budget checks
  progress.go     ProgressReporter interface + TextProgressReporter

internal/app/
  hermes_bridge.go    makePlanFn / makeExecFn — wire CLIClient → hermes funcs
  hermes_storage.go   buildHermesTaskStore — wire SQLiteStorage → hermes store
```

---

## Lifecycle

```
User: /hermes
  → TelegramBot enables Hermes mode for this chat

User: "refactor the auth module to use JWT"
  → startHermesTask() creates Coordinator and calls Start()

Phase 1 — Planning (PlannerSession):
  Planner receives: plannerSystemPrompt + "\n\nGoal: " + goal
  Planner outputs: ```json [...SubTask array...] ```
  Retry up to maxPlannerJSONRetries if JSON parse fails
  Fallback to classic routing if still failing

Phase 2 — Execution loop:
  For each SubTask:
    1. Check TokenBudget.Exceeded() → abort if true
    2. Apply task feedback already accepted by Chat Agent
    3. BuildExecutorPrompt(state, coreRules) → Executor
    4. Executor runs tools, returns result text
    5. Retry up to maxRetriesPerSubtask if validation_error set
    6. AppendResult() → UpdateAccumulated()
    7. ProgressReporter.OnSubTaskDone()

Phase 3 — Done:
  MarkStatus(done)
  ProgressReporter.OnDone() → sends summary + artifact list to Telegram
```

---

## Planner JSON Protocol

The Planner must output exactly one fenced code block:

```json
[
  {"id":"s1","description":"Read internal/auth/auth.go","tool_hints":["Read"]},
  {"id":"s2","description":"Add JWT validation","tool_hints":["Edit"]},
  {"id":"s3","description":"Run tests","tool_hints":["Bash"]}
]
```

**Parsing strategy** (in `parsePlannerJSON`):
1. Extract `\`\`\`json ... \`\`\`` block via regex
2. Fallback: find first `[` … last `]`
3. Validate required fields (`id`, `description`)
4. On failure: re-inject error as feedback, retry up to `maxPlannerJSONRetries`
5. Final failure: `ErrPlannerJSONFailed` → caller falls back to classic routing

---

## Executor Prompt

Each cold-start Executor invocation receives:

```
=== Hermes Executor Context ===
Goal: <goal>

Accumulated summary:
<up to 2KB of recent results>

Artifacts so far:
  internal/auth/jwt.go [a1b2c3d4]

Current sub-task (2/5): Add JWT validation to auth.go
Suggested tools: Edit, Bash
Budget: tokens=5000/500000 elapsed=30s/600s
```

No conversation history is injected. Claude Code CLI loads skills automatically.

---

## Budget Control

Budget is checked at the start of each sub-task iteration:

```go
if state.TokenBudget.Exceeded() {
    progress.OnBudgetWarning(state.TokenBudget)
    store.MarkStatus(taskID, TaskStatusFailed)
    return
}
```

Defaults (overridable via config):
- `max_total_tokens`: 500,000
- `max_wallclock_seconds`: 600 (10 minutes)

---

## Interrupt Handling

Hermes task state currently uses fixed feedback injection: messages accepted as
task feedback are appended to accumulated context and the task continues.

Classification of "feedback vs interrupt vs new task" belongs to the Chat Agent
FSM described in `docs/arch/agent-fsm-architecture.md`. Hermes should receive a
clear event and should not infer chat intent from timestamps or sticky session
state.

---

## Progress Notifications (Telegram)

Events sent at `normal` verbosity:

| Event | Message |
|---|---|
| Plan ready | "計畫完成，共 N 個子任務：\n  1. ...\n  2. ..." |
| Sub-task start | "[2/5] 執行：Add JWT validation to auth.go" |
| Sub-task done (✅) | "✅ [2/5] Add JWT validation to auth.go" |
| Sub-task failed (❌) | "❌ [2/5] Add JWT validation to auth.go" |
| Budget warning | "⚠️ 預算即將耗盡（tokens=400000/500000）" |
| Task done | "✅ Hermes 任務完成（3/5 子任務成功）\n摘要：..." |

---

## Commands

| Command | Effect |
|---|---|
| `/hermes` | Enable Hermes mode; next message triggers Brain-Executor |
| `/hermes status` | Show current task ID and running state |
| `/hermes stop` | Cancel current task, disable Hermes mode |
| `/auto` | Disable Hermes mode, switch back to classic routing |

---

## Configuration

```json
{
  "hermes": {
    "enabled": false,
    "planner_model": "claude-opus-4-7",
    "executor_model": "claude-haiku-4-5-20251001",
    "max_retries_per_subtask": 3,
    "max_planner_json_retries": 3,
    "progress_verbosity": "normal",
    "hooks": {
      "path_guard": true,
      "post_validators": ["go_build", "json_lint"]
    },
    "budget": {
      "max_total_tokens": 500000,
      "max_wallclock_seconds": 600
    }
  }
}
```

---

## Metrics & Observability

Token counts are tracked via `store.AddTokenUsage()` at each step and are visible through the existing Alice dashboard. Planner JSON success rate can be inferred from log output:

```
[hermes] chat 123 started task <uuid>
[hermes] compression failed for task <uuid>: ...
```

Future: feed per-task token/retry metrics into `performance.go` (#98 deferred).

---

## Related

- [hooks.md](hooks.md) — tool execution hooks (#96)
- [state-management.md](state-management.md) — task state persistence (#97)
- Issue [#99](https://github.com/chimerakang/alice/issues/99) — Prompt core rules prepend
- Issue [#100](https://github.com/chimerakang/alice/issues/100) — SDK hybrid fallback (deferred)
