# Alice Unified Memory Architecture

Issue #143 defines Alice memory as one policy surface instead of several
runner-specific context tricks. This document records the current sources, the
target layering, and the first implementation boundary.

## Goals

- Give every runner one retrieval/assembly entry point: `MemoryResolver`.
- Keep memory sections attributable, scoped, ordered, and clampable.
- Prefer explicit issue memory over generic active chat memory.
- Reduce repeated rediscovery while still allowing targeted verification.
- Make later observability straightforward by carrying source and scope metadata.

## Memory Layers

| Layer | Current Source | Lifecycle | Inject When | Stale / Invalid When | Budget Rule |
| --- | --- | --- | --- | --- | --- |
| Session memory | Claude/Codex native session IDs in `ChatContext.Sessions` | Backend-defined; can disappear after timeout or model/backend change | Same backend session is available | Resume fails, user clears context, backend changes | No prompt budget because it lives in backend state |
| Short-term recent bridge | `ChatContext.RecentMsgs` | Last 10 user/assistant messages per chat/thread | Model switch, resume fallback, Hermes follow-up | User clears context or messages age out | Clamp as a low-priority memory section |
| Structured Hermes task memory | `hermes.TaskState` in SQLite or memory store | Persisted task state | Hermes continuation, same issue request, related chat follow-up | Task is unrelated to requested issue/project/thread | Highest priority for matching issue; compact task summary only |
| General task memory | Unified `tasks/sub_tasks` rows mirrored from `decision_logs` | Until normal data retention cleanup | Direct/file/media follow-up and related issue/project requests | Task is unrelated to requested issue/project/thread | Compact request/result cards |
| Issue-scoped memory | `TaskState.GithubIssueNumber` and issue-aware follow-up detection | Persists with Hermes tasks | Message explicitly references `#N` / `＃Ｎ` | Requested issue differs | Takes priority over unrelated active tasks |
| Project/static knowledge | `CLAUDE.md`, `docs/arch/memory.md` | Repository-managed | New task planning and project-specific follow-up when `project_dir` is known | File changes or project changes | Low-priority `static_hint` section with strict attribution and per-file clamping |
| Observability/audit memory | Decision logs, tool events, future memory-card records | Persisted log data | Dashboard/API and debug inspection | Not prompt-critical unless queried | Do not inject by default |

## Current Implementation

The first version adds `internal/app/memory_resolver.go` with:

- `MemoryRequest`: chat/thread/project/user message/issue/mode/budget/recent messages.
- `MemoryBundle`: ordered `MemorySection` entries.
- `MemorySection`: `Source`, `Scope`, `Priority`, and prompt-ready `Text`.
- `UnifiedMemoryResolver`: combines recent messages, Hermes task memory, general
  task memory, and low-priority static project hints.

Hermes now enters memory retrieval through `MemoryResolver` in
`buildHermesGoalWithContext`. The older `loadHermesContextTasks` helper remains
as a compatibility wrapper for tests and existing call sites.

`GET /api/memory/preview` provides the first observability surface for memory
resolution. It accepts `chat_id`, optional `thread_id`, `project_dir`, `issue`,
`message`, `mode`, and `budget`, then returns section source/scope/priority,
size, and preview text for the bundle that would be assembled.

Static project hints are intentionally narrow. `ProjectStaticHintSource` only
reads `CLAUDE.md` and `docs/arch/memory.md`, skips missing or oversized files,
attributes each snippet by path, and injects them below task memory as
orientation rather than implementation truth.

## Retrieval Policy

For each incoming message:

1. Detect explicit issue number from `MemoryRequest.IssueNumber` or the user
   message.
2. If an issue is explicit, load same-issue Hermes tasks first and ignore
   unrelated active tasks.
3. If no issue is explicit, load the newest active chat task, then recent chat
   task history.
4. Skip task history whose actionable goal exactly matches the current request.
5. Append recent messages as lower-priority continuity only when the request is
   not explicitly issue-scoped. Generic recent messages must not be injected
   into `#N` requests unless Alice can prove they belong to the same issue/task.
6. Sort sections by priority and clamp within `BudgetChars`.

## Runner Coverage

| Runner / Path | Current Memory Status | Target |
| --- | --- | --- |
| Hermes issue/follow-up | Uses `MemoryResolver` for Hermes task, general task memory, recent bridge, static hints, and preview observability | Add richer task summaries |
| Direct Agent | Uses backend session; model switch and resume fallback assemble recent bridge, general task memory, and static hints through `MemoryResolver` | Add richer task summaries |
| File/document analysis | Stop-button document runner resolves prompt memory, including static hints when project context is known, through `MemoryResolver` before calling Direct Agent | Add richer file-specific memory metadata |
| Multimedia analysis | Photo and voice runners resolve prompt memory, including static hints when project context is known, through `MemoryResolver` before calling Direct Agent | Add richer media-specific memory metadata |
| Retry/review/checkup | Reads task/review state through existing services | Use memory sections for prior task/review summaries |

## Continuation Rules

- **Continuation**: user asks to continue, retry, replan, finish, or amend a
  previous task without providing a standalone goal.
- **Follow-up**: user references earlier output or says things like "那個",
  "上一個", "再補", "剛剛", or a direct issue number.
- **Issue-scoped request**: user includes `#N` or full-width `＃Ｎ`, or routing
  already resolved an issue number.

When a request is issue-scoped, same-issue memory outranks active generic chat
memory. Active tasks from different issues must not contaminate the prompt.

## Prompt Budget

Memory sections are sorted by priority before clamping. High-priority issue
memory is kept first; recent bridge is last. Each section must be independently
renderable and useful when truncated. The first implementation uses character
budgets because existing Hermes prompt assembly is character-based.

## Near-Term Work

- Make general persisted memory cards more explicit than the current
  decision-log/unified-task mirror, including touched files and continuation
  hints.
- Add dashboard UI for available memory sections and injected bundle previews.
- Expand static project hints beyond the initial controlled files when there is
  a clear routing and freshness policy.
