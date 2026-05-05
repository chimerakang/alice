# P15 Issue Realignment

Snapshot date: 2026-05-05. Source of truth checked with `gh issue list` against
`chimerakang/alice`.

This document answers the question: which existing P15/Hermes/runtime issues
should continue, which should be merged into the new agent/FSM architecture, and
which should not be developed as originally written.

## Architecture Baseline

The new baseline is [agent-fsm-architecture.md](agent-fsm-architecture.md):

- Alice runtime is a set of small agents with local FSMs.
- Hermes is the Task Agent's long-task engine.
- Chat, Session, Execution, Recovery, Memory, and Task state are separate
  ownership boundaries.
- FSM is the shared runtime contract for adding future agents, not one global
  controller. New agents should reuse the same state snapshot, transition,
  event/command, recovery, and trace patterns.
- We do not build one central FSM library before clarifying these boundaries.

## Do Not Develop As Originally Written

| Issue | Current Title | Decision | Reason |
| --- | --- | --- | --- |
| #120 | `[Epic] Alice 架構統一：ExecutionEngine + Review 反饋機制` | Replace with new runtime epic | Its completed parts are real, but the remaining ChatRouter/StrictReview/cleanup sequence assumes ExecutionEngine is the central unifier. The new architecture makes Chat/Session/Execution/Recovery separate owners. |
| #144 | `Hermes mode 架構精簡：路由規則、狀態機、訊息流` | Superseded and closed on 2026-05-05 | The pain is correct, but this issue is Hermes-centric. The fix is not "simplify Hermes rules" alone; it is moving chat/session/recovery decisions out of Hermes. |
| #149 | `Spike: Single-session walking agent via Python Claude Agent SDK` | Superseded and closed on 2026-05-05 | The spike already produced the useful result. Do not implement a Python helper as the main path. If session reuse continues, it should be via the existing Go CLI resume path and Session Agent policy. |
| #115 | `拆掉舊 Coordinator / DecisionLog / hermes_bridge 收尾` | Pause | Cleanup should wait until Chat/Session/Execution/Recovery boundaries exist. Removing old adapters now risks deleting useful transition scaffolding. |
| #146 | `SessionPolicy: direct bridge / model switch memory source policy` | Merged into SessionPolicy work | This is a symptom of missing session/memory policy. It should not remain a standalone memory tweak once Session Agent is defined. |

These issues should not receive new implementation work in their current form.
Recommended GitHub action: comment with a link to this document, then close or
relabel as `superseded` once the replacement epic/issues are opened.

## Keep, But Reframe

| Issue | Current Title | New Owner | Reframed Scope |
| --- | --- | --- | --- |
| #154 | `Chat FSM: short follow-up option references lose context` | Chat Agent | First Chat FSM slice. Detect option references, awaiting-input/follow-up semantics, and inject same-thread recent option tail without broad semantic memory. |
| #143 | `建立 Alice Unified Memory Architecture` | MemoryResolver + Session Agent | Keep MemoryResolver, but do not let memory policy decide session ownership. Memory provides scoped sections; Session Agent decides whether to use them with native resume. |
| #151 | `Task Agent: TaskState resume + partial retry` | Task Agent + Recovery Agent | Keep as TaskState resume/partial retry. Remove any assumption that cross-run state belongs to ChatContext or native session. |
| #152 | `Planner 結構性停止 + 子任務粒度規則` | Task Agent / Planner guardrail | Keep the single-action rule and planner guardrail. StopAtTools is optional if CLI support remains uncertain; do not block Chat FSM on it. |
| #148 | `Token/Cost 後台數字...` | Trace/Observability | Keep reporting fixes and cache metrics. Expand later into runtime spans after agent boundaries exist. |
| #150 | `Codex CLI VS Code interception` | Observability / hooks | Unrelated to the FSM split; keep as its own P1 if still desired. |

## New Development Issues

### New Epic: Alice Runtime Agent/FSM Architecture

Goal: replace the outdated #120/#144 framing with explicit runtime owners.

Checklist:

- [ ] Document Chat/Session/Task/Execution/Recovery FSM boundaries.
- [ ] Define the shared runtime contract for future agents: state snapshot,
  transition guard, event input, command output, recovery outcome, and tracing.
- [ ] Add local transition helpers where state already exists.
- [ ] Move session/memory policy decisions out of `Agent.Run`.
- [ ] Keep Hermes as Task Agent, not a chat/session owner.
- [ ] Emit trace events for routing/session/task/execution/recovery decisions.
- [ ] Retire or close superseded issues after replacement slices land.

Suggested labels: `architecture`, `backend`, `P1`, `epic`.

### Issue A: Chat FSM + Follow-Up/Awaiting-Input State

Replaces/extends #154.

Scope:

- Add explicit `ChatState`.
- Define states: `idle`, `receiving`, `routing`, `dispatching`, `busy`,
  `awaiting_input`, `interrupting`.
- Detect local option references such as `a/b`, `選 b`, `處理完 b`, `第二個`.
- Preserve assistant answer tails where options/next steps usually appear.
- For continuation, inject same-thread recent option context before semantic
  memory.
- Add regression tests for the #154 example.

Out of scope: broad memory platform, Hermes partial retry.

### Issue B: Session Agent / SessionPolicy

Absorbs #146 and part of #143.

Scope:

- Introduce `SessionPolicy`: `NativeOnly`, `MemoryOnly`, `HybridIssueOnly`,
  `Fresh`.
- Centralize sticky timeout, model/backend switch, resume unavailable, and
  `/clear` behavior.
- Decide policy before each model call.
- Prevent low-signal short messages from triggering broad semantic memory.
- Keep cross-backend continuity through scoped recent/task memory when useful.

Out of scope: changing Claude/Codex client protocols.

### Issue C: Execution FSM

Opened as #155: `Execution FSM: centralize processing/cancel/stream lifecycle
state`.

Scope:

- Replace scattered `processing bool`, `cancelFunc`, retry loop state, and stream
  lifecycle checks with `ExecutionState`.
- Emit `ExecutionStarted`, `ExecutionRetryableError`, `ExecutionSucceeded`,
  `ExecutionFailed`, and `ExecutionCancelled`.
- Keep existing CLI clients and DirectEngine behavior.

Out of scope: TaskState partial retry and review policy.

Status 2026-05-05: first implementation slice landed. `processing bool` has
been removed from `Agent`; `ExecutionLifecycle` is now the source of truth for
processing state. `Agent.Run`, `RunWithPlan`, and `runDirect` update execution
state, and web agent status exposes `execution` plus `execution_state` while
keeping legacy `is_processing`.

### Issue D: Recovery Agent

Opened as #156: `Recovery Agent: centralize retry/fallback/backoff policy`.

Scope:

- Centralize retry/fallback/backoff decisions.
- Model direct retry, planner retry, review retry, `/retry`, and startup recovery
  with one policy surface.
- Add retry budgets and explicit terminal reasons.
- Hand partial retry decisions to Task Agent.

Out of scope: UI for manual retry menus, unless needed for tests.

Status 2026-05-05: first implementation slice landed. `RecoveryPolicy` now
provides pure retry/fallback/fail decisions, and `Agent.Run` direct stream retry
uses it while preserving the existing retry behavior. Plan/Execute fallback,
Planner JSON/empty-plan/quality retry, and Hermes task-level review retry now
route through the same decision shape without changing their existing behavior.
Manual `/retry` sub-task retry limits also use RecoveryPolicy now; the Telegram
handler still owns formatting and selection, but no longer owns the retry-budget
decision. Strict per-sub-task review retry also routes its retry/exhaustion
decision through RecoveryPolicy while preserving the existing partial/skipped
behavior at retry exhaustion. `/retry` direct watchdog cancellation now also uses
RecoveryPolicy to decide the context-done cancellation before the app layer
aborts the agent. Recovery decisions now produce a normalized
`RecoveryDecision` runtime event payload and log line, giving #148 a stable
input shape for later persisted trace/span storage.

### Issue E: Hermes Task Agent Resume + Partial Retry

Refines #151.

Scope:

- Add `RunFromState` or equivalent resume entry.
- Preserve completed/high-score sub-tasks.
- Replan or rerun only failed/low-score scopes.
- Read issue checklist state and repo snapshot before planning.
- Keep Task FSM limited to `planning/executing/done/failed/interrupted`.

Out of scope: Chat follow-up detection and backend session policy.

Status 2026-05-05: intra-run partial retry is already implemented for
task-level review retry: high-score completed sub-tasks are preserved, the
replan prompt tells the planner not to repeat them, and merged retry plans keep
preserved work. A pure `TaskResumeDecision` helper now classifies persisted
`TaskState` into preserved/remaining/fromIdx. `PlanExecuteEngine.RunFromState`
is now wired into explicit Hermes continuation paths, including issue-based and
similar-goal continuation, so resume preserves the original task ID and skips
completed/skipped sub-tasks instead of replanning from zero.

### Issue F: Runtime Trace + Token/Cache Observability

Refines #148.

Scope:

- Keep cache read/write token accounting.
- Add per-phase trace spans: chat route, session policy, planner, executor,
  reviewer, recovery, GitHub sync.
- Expose cache hit %, cache creation, estimated cost, and retry cost by phase.

Out of scope: changing billing source of truth or reconciling with provider
exports.

Status 2026-05-05: first persisted trace slice landed. `runtime_events` stores
normalized runtime events with timestamp/type/chat/thread/task/issue/payload,
and RecoveryDecision events are now written through this path. Token/cache
phase accounting remains covered by the earlier `phase_usages` slice; this
adds the event stream needed to explain retry/fallback/cancel decisions.
`GET /api/runtime/events` exposes the persisted stream with limit/offset/type
filters for dashboard and diagnostic tooling.

## Recommended Order

1. Issue A: Chat FSM + #154 fix.
2. Issue B: SessionPolicy + #146 merge.
3. Issue C: Execution FSM (#155).
4. Issue D: Recovery Agent (#156).
5. Issue E: Hermes Task Agent resume/partial retry.
6. Issue F: Trace/observability.
7. Cleanup old scaffolding (#115 replacement) only after 1-6 are stable.

## GitHub Maintenance Checklist

- [ ] Comment on #120: superseded by new runtime epic; close after replacement
  epic is opened.
- [x] Comment on #144: design superseded; keep examples as background, close or
  link to new epic.
- [x] Close or retitle #149: spike complete; no Python helper implementation as
  primary path.
- [x] Move #146 under SessionPolicy or close as duplicate after Issue B opens.
- [x] Keep #154 active and make it the first Chat FSM implementation slice.
- [x] Keep #151 active but retitle to "Task Agent: TaskState resume + partial
  retry".
- [ ] Keep #148 active but retitle to "Runtime trace + token/cache
  observability".
