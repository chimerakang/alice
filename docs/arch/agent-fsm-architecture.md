# Alice Agent/FSM Runtime Architecture

This document supersedes the older idea of a single global FSM for Alice. The
new boundary is: **small agents own small state machines; Hermes is the long-task
implementation of the Task Agent**.

The purpose of using FSMs is not to make the whole product one giant state
diagram. The purpose is to give every new agent the same core runtime logic:
explicit states, guarded transitions, common events/commands, retry/recovery
contracts, and traceable behavior.

## Decision

Alice should not grow one central state machine that controls Telegram chat,
Hermes, backend sessions, retries, review, and GitHub sync. That shape makes
every state know too much.

Instead, Alice uses a manager-style runtime:

```text
Telegram / API input
  -> Chat Agent
       -> Direct Execution Agent       (simple turns)
       -> Hermes Task Agent            (long work)
              -> Planner role
              -> Execution Agent
              -> Reviewer role
              -> Recovery Agent
       -> Session Agent                (backend continuity)
       -> MemoryResolver               (scoped prompt memory)
```

Each agent has an explicit state enum and transition guard, but those FSMs stay
local to the agent that owns the behavior. New agents can add new capabilities
without inventing their own lifecycle rules from scratch.

## Runtime Contract

Every agent should follow the same small contract even when its business logic
is different:

```go
type RuntimeAgent interface {
    Name() string
    State() StateSnapshot
    Handle(ctx context.Context, event Event) ([]Command, error)
}
```

The shared runtime logic is:

| Runtime Piece | Shared Behavior |
| --- | --- |
| State snapshot | Every agent exposes current state, owner, timestamps, and terminal/error status. |
| Transition guard | State changes go through a validation function, not direct field mutation. |
| Event input | Agents react to typed events instead of reading another agent's private fields. |
| Command output | Agents request work through commands instead of doing cross-boundary side effects. |
| Recovery contract | Retry/fallback/cancel outcomes use common event names and terminal reasons. |
| Trace contract | Every meaningful transition can be logged as a runtime span. |

This means a future `ReviewAgent`, `PrototypeAgent`, `SecurityAgent`, or
`ScreenshotAgent` can be added by implementing the same lifecycle surface while
keeping its specialized behavior inside its own module.

## Agent Boundaries

| Agent | Owns | Does Not Own |
| --- | --- | --- |
| Chat Agent | Incoming message lifecycle, follow-up detection, user-visible busy/awaiting-input state, interrupt classification | Backend session IDs, Hermes task internals, retry strategy |
| Session Agent | Claude/Codex session IDs, sticky timeout, resume availability, session/memory policy decisions | User intent, task progress, retry outcomes |
| Task Agent | Long-task lifecycle, task/sub-task state, GitHub issue binding, task progress events | Chat UX, backend session mechanics |
| Execution Agent | One model call lifecycle, streaming/tool updates, cancellation, low-level retryable errors | Task planning, review policy, cross-run task memory |
| Recovery Agent | Retry/backoff/fallback decisions, retry budget, partial retry/replan policy | Model invocation details, Telegram message formatting |
| MemoryResolver | Prompt memory retrieval and rendering by scope/source/priority | Native backend session ownership, task transitions |

Hermes is not a parallel product inside Alice. Hermes is the Task Agent's
long-running workflow engine.

## Local FSMs

### Chat FSM

```text
idle
  -> receiving
  -> routing
  -> dispatching
  -> awaiting_input
  -> idle

dispatching
  -> busy
  -> idle

busy
  -> interrupting
  -> busy
  -> idle
```

Primary purpose: remove implicit guesses such as "idle because LastActivity is
old" or "busy because Agent.processing is true". Chat state should answer user
interaction questions:

- Is Alice ready for a new task?
- Is Alice waiting for a choice/confirmation?
- Is a user message feedback for the current task, an interrupt, or a new task?
- Should a short message such as "b 的話呢" receive recent-option context?

### Session FSM

```text
fresh
  -> active
  -> sticky
  -> expired
  -> cleared

active
  -> unavailable
  -> cleared
```

Primary purpose: decide whether a model call should use native resume,
MemoryResolver prompt memory, both, or neither.

Suggested policies:

| Policy | Behavior |
| --- | --- |
| `NativeOnly` | Use backend resume; do not inject persisted task memory. |
| `MemoryOnly` | No backend resume; assemble prompt through MemoryResolver. |
| `HybridIssueOnly` | Use native resume plus scoped issue memory when an issue is explicit. |
| `Fresh` | No resume and no persisted memory. |

Session policy must be decided before the model call, not inferred inside
`Agent.Run`.

### Task FSM

Current Hermes task state is the base:

```text
planning
  -> executing
  -> done
  -> failed
  -> interrupted
```

`validating` is not a current runtime state. Review is a phase inside
`executing`, not a public task lifecycle state.

Primary purpose: make long-task state durable and resumable. This FSM belongs
to Hermes/Task Agent only. It should not decide chat routing or backend session
reuse.

### Execution FSM

```text
idle
  -> running
  -> retrying
  -> succeeded
  -> failed
  -> cancelled
```

Primary purpose: replace scattered `processing bool`, `cancelFunc`, retry loops,
and stream update side effects with one model-call lifecycle. The Execution
Agent emits events; it does not decide whether a whole Hermes task is failed.

### Recovery FSM

```text
none
  -> retry_scheduled
  -> retrying
  -> fallback
  -> exhausted
  -> resolved
```

Primary purpose: centralize retry/fallback decisions that are currently split
between direct Agent retry, Hermes planner retry, `/retry`, review feedback, and
startup recovery.

## Event Contract

Agents coordinate through events and commands, not by reaching into each
other's fields.

Examples:

```go
type Event struct {
    Type      string
    ChatID    int64
    ThreadID  int
    TaskID    string
    Issue     int
    Payload   any
    Timestamp time.Time
}

type Command struct {
    Type    string
    Target  string
    Payload any
}
```

Recommended event names:

| Source | Event |
| --- | --- |
| Chat Agent | `UserMessageReceived`, `FollowUpDetected`, `InterruptRequested`, `AwaitingInputTimedOut` |
| Session Agent | `SessionActivated`, `SessionExpired`, `SessionUnavailable`, `SessionCleared` |
| Task Agent | `TaskPlanned`, `TaskStarted`, `SubTaskStarted`, `SubTaskCompleted`, `TaskCompleted`, `TaskFailed` |
| Execution Agent | `ExecutionStarted`, `ExecutionRetryableError`, `ExecutionSucceeded`, `ExecutionFailed`, `ExecutionCancelled` |
| Recovery Agent | `RetryScheduled`, `RetryExhausted`, `FallbackSelected`, `PartialRetrySelected` |

## Implementation Order

1. **Chat FSM first**
   - Fixes the highest user-facing pain: short follow-up references, inserted
     user messages while Hermes is running, and awaiting-input behavior.
   - Existing issue #154 should become the first concrete slice.
   - Status 2026-05-05: first slice landed. `ChatContext` exposes explicit
     `ChatState`, option-reference follow-ups inject same-thread recent context,
     assistant choice prompts can leave the chat in `awaiting_input`, and active
     task abort/interrupt paths mark `interrupting`.

2. **SessionPolicy / Session Agent**
   - Pull sticky session, model-switch bridge, resume fallback, and memory
     injection decisions out of `Agent.Run`.
   - This resolves the class of #146 issues without removing useful cross-backend
     continuity.
   - Status 2026-05-05: first slice landed. `SessionPolicy`,
     `SessionRunRequest`, and `SessionRunDecision` live in `internal/app/engine`;
     direct bridge, direct run routing/session lookup, Hermes goal memory, and
     Telegram runner memory prompts share the same policy vocabulary.

3. **Execution FSM**
   - Wrap `processing`, `cancelFunc`, retry loops, and stream callbacks behind a
     single model-call lifecycle.
   - Status 2026-05-05: first slice landed under #155. `ExecutionLifecycle`
     exposes guarded `starting/streaming/retrying/cancelling/succeeded/failed/
     cancelled` transitions; `Agent.IsProcessing()` is backed by this lifecycle,
     and agent API responses include `execution` / `execution_state`.

4. **Recovery Agent**
   - Consolidate direct retry, planner JSON retry, review retry, partial retry,
     and fallback behavior.
   - Status 2026-05-05: first slice landed under #156. `RecoveryPolicy` defines
     a pure retry/fallback/fail decision shape, and `Agent.Run` direct stream
     retry now uses it while preserving existing retry timing. Plan/Execute
     fallback, Planner JSON/empty-plan/quality retry, Hermes task-level review
     retry, strict sub-task review retry, manual `/retry` sub-task retry limits,
     and retry watchdog cancellation also use the same decision shape while
     preserving existing behavior. Recovery decisions now emit a normalized
     `RecoveryDecision` runtime event payload and log line that can become a
     persisted trace span in the observability slice.

5. **Hermes Task Agent cleanup**
   - Keep Hermes as the long-task engine.
   - Add TaskState resume/partial retry.
   - Stop giving Hermes ownership of ChatContext/session policy.
   - Status 2026-05-05: intra-run partial retry already preserves high-score
     sub-tasks on task review retry. `TaskResumeDecision` classifies persisted
     `TaskState`, and `PlanExecuteEngine.RunFromState` is wired into explicit
     Hermes continuation paths so completed/skipped sub-tasks are preserved and
     remaining work resumes under the original task ID.

6. **Trace/observability**
   - Emit trace spans for chat routing, session policy, task phases, execution,
     guardrails, retry, and GitHub sync.

## Non-Goals

- No global FSM that controls all of Alice.
- No new microservices or queues in the first implementation.
- No Python helper process just to get "agent" terminology.
- No broad rewrite of Telegram handlers before the state boundaries exist.
- No removal of Hermes; Hermes is kept and narrowed.

## Issue Realignment

See [p15-issue-realignment.md](p15-issue-realignment.md) for the concrete
GitHub issue plan that follows from this architecture.
