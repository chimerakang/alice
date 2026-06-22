# OpenAI Agents SDK Comparison

This note compares Alice's current Hermes / ExecutionEngine architecture with
the design primitives in `openai/openai-agents-python`.

The goal is not to port Alice to Python. Alice should keep its Go Telegram bot,
Claude/Codex CLI backends, SQLite persistence, and dashboard. The useful part is
the SDK's runtime vocabulary: clear boundaries for agents, runners, sessions,
guardrails, handoffs, and tracing.

## Source References

- OpenAI Agents SDK repository:
  <https://github.com/openai/openai-agents-python>
- Agents and multi-agent patterns:
  <https://openai.github.io/openai-agents-python/agents/>
- Runner lifecycle and run configuration:
  <https://openai.github.io/openai-agents-python/running_agents/>
- Sessions:
  <https://openai.github.io/openai-agents-python/sessions/>
- Guardrails:
  <https://openai.github.io/openai-agents-python/guardrails/>
- Tracing:
  <https://github.com/openai/openai-agents-python/blob/main/docs/tracing.md>

## Current Alice Mapping

| OpenAI Agents SDK concept | Alice today | Notes |
| --- | --- | --- |
| `Agent` | Planner, Executor, Reviewer roles | Alice has role-specific prompts and model choices, but no single role descriptor type. |
| `Runner` | `PlanExecuteEngine`, `DirectEngine` | Alice has execution engines, but Hermes lifecycle logic is still spread across planner, executor, reviewer, retry, and GitHub callbacks. |
| `RunConfig` | Hermes config, model routing config, GitHub config, `ChatContext` | Alice passes these through several structs. A single run-scoped config would make behavior easier to reason about. |
| `RunContext` | `ChatContext`, project dir, issue metadata, memory resolver inputs | Alice has the pieces, but no explicit dependency injection object for every role/tool/reviewer call. |
| Sessions | Claude/Codex native resume IDs, walking-agent state, persisted memory | Alice currently mixes native sessions and prompt-injected memory. This is the source of several #143/#146/#149 risks. |
| Guardrails | preflight, clean worktree check, path guard, post validators, strict review | Alice already has guardrails, but they are named and surfaced differently depending on where they run. |
| Handoffs | Hermes role transitions | Alice should not adopt decentralized handoff semantics yet; Hermes needs central control over repo state and GitHub workflow. |
| Tracing | performance metrics, unified tasks, `PhaseUsage` | `PhaseUsage` is a good first step. A trace/span model would make token spikes and retry loops easier to diagnose. |
| Sandbox agents | Project workspace + process runner + path guard | Alice already works in real local repos. It can borrow manifest/capability ideas without moving to the SDK sandbox runtime. |

## What Alice Should Borrow

### 1. Runner Loop

The Agents SDK runner loop is simple:

1. Call the model for the current agent.
2. If the result is final output, finish.
3. If the result requests a tool call, run the tool and continue.
4. If the result requests a handoff, switch agent and continue.
5. Stop when `max_turns` is exceeded.

Alice's Hermes loop is conceptually similar, but the lifecycle is less explicit:

1. Preflight
2. Plan
3. Execute sub-tasks
4. Review
5. Retry or partial re-plan
6. Sync GitHub
7. Close or leave open

Recommended Alice adaptation:

```text
HermesRunner.Run(config, context, input)
  -> RunState
  -> events/spans
  -> final result
```

This belongs in #144 and #120. The win is not fewer lines of code; the win is
that each transition becomes inspectable and testable.

### 2. RunConfig

OpenAI's `RunConfig` centralizes run-level behavior such as model/provider,
session settings, guardrails, handoff filters, tracing, and tool behavior.

Alice should introduce an Alice-native equivalent:

```go
type RunConfig struct {
    Chat          ChatContext
    ProjectDir    string
    Backend       BackendKind
    Models        RoleModels
    SessionPolicy SessionPolicy
    MemoryPolicy  MemoryPolicy
    Guardrails    GuardrailPolicy
    Trace         TraceConfig
    GitHub        GitHubIssueContext
}
```

This would replace the current pattern where Hermes config, GitHub config,
strict review config, memory inputs, and model routing choices are assembled in
different layers.

Recommended issue: #144.

### 3. SessionPolicy

The Agents SDK explicitly warns against mixing client-managed session history
with server-managed conversation continuation for the same run, because the same
context can be duplicated.

Alice has the same class of problem with different ingredients:

- Claude/Codex native session IDs
- persisted Hermes task memory
- recent message bridge
- direct prompt context
- walking-agent slim prompts

Recommended Alice policy:

```text
NativeOnly
  Use Claude/Codex resume. Do not inject persisted task memory.

MemoryOnly
  Do not resume a native session. Build prompt memory through MemoryResolver.

HybridIssueOnly
  Allow native session + scoped issue memory only when a GitHub issue number is explicit.

Fresh
  No native resume and no persisted memory.
```

This policy should be decided before each model call, not inferred ad hoc inside
the caller. This is directly relevant to #143, #146, and #149.

### 4. Guardrails as Runtime Primitives

OpenAI separates guardrails by where they run:

- Input guardrails
- Output guardrails
- Tool guardrails

Alice already has these concepts, but not under one vocabulary:

| Alice guardrail | Proposed category |
| --- | --- |
| issue already completed check | Input guardrail |
| dirty worktree check | Input guardrail |
| Planner checklist/granularity rules | Planner guardrail |
| path guard | Tool guardrail |
| post validators | Tool/output guardrail |
| strict reviewer tags | Output guardrail |
| issue close readiness | Output guardrail |

Recommended shape:

```go
type GuardrailResult struct {
    Name      string
    Phase     string
    Decision  GuardrailDecision
    Reason    string
    Evidence  []string
    Cost      Usage
}
```

Guardrails should become first-class events in summaries and dashboard traces.
This would make it easier to explain why Hermes skipped, retried, blocked, or
left an issue open.

Recommended issues: #144, #148.

### 5. Trace/Span Observability

OpenAI Agents SDK tracing records a workflow as traces and spans: model
generations, tool calls, handoffs, guardrails, and custom events.

Alice has:

- `performance_metrics`
- unified task rows
- review rows
- model usage
- phase usage

The missing layer is a parent/child trace model:

```text
trace: hermes issue #148
  span: preflight
  span: planner
  span: executor subtask 1
    span: cli call
    span: tool event
  span: reviewer
  span: github sync
  span: issue close
```

The existing `PhaseUsage` can remain the aggregate view. Trace spans should
answer the debugging questions:

- Which phase consumed the most tokens?
- Which model call caused a cache miss?
- Which guardrail blocked or skipped execution?
- Which retry caused repeated planner/reviewer cost?
- Why did GitHub issue closure not happen?

Recommended issue: #148.

### 6. Agents as Tools, Not Handoffs

The SDK documents two multi-agent patterns:

- Manager invokes specialized agents as tools.
- Peer agents hand off control to one another.

Alice should prefer the manager pattern. Hermes must keep central control over:

- Git baseline
- worktree safety
- path guards
- test evidence
- review gates
- GitHub checklist sync
- issue close policy

Handoffs are a useful vocabulary, but decentralized handoffs would make Alice's
coding workflow harder to audit. Planner, Executor, Reviewer, Preflight, and
GitHub sync should remain centrally orchestrated roles.

Recommended issue: #120.

### 7. Structured Outputs for Planner/Reviewer

The Agents SDK treats typed output as a first-class feature. Alice cannot rely on
OpenAI structured outputs for Claude CLI, but it can borrow the discipline:

- Define Go structs as the source of truth.
- Keep JSON schema examples generated or mirrored from those structs.
- Treat parse failure as a structured model behavior error.
- Record planner JSON success/failure as metrics.

Alice already has planner JSON retry and review JSON parsing. The next step is
to make the schema contract more explicit in docs, tests, and telemetry.

Recommended issues: #148, #144.

## What Alice Should Not Borrow Yet

### Do Not Port Alice to Python SDK Runtime

Alice's constraints are different:

- Go Telegram bot
- Claude/Codex CLI subscription usage
- local SQLite and dashboard
- project-specific GitHub issue workflow
- local process runner and path guard

Moving the core runtime to Python Agents SDK would reintroduce the #100 problem:
API billing, new deployment surface, split runtime ownership, and another
session/memory layer to debug.

### Do Not Adopt Decentralized Handoffs for Hermes

Hermes is a coding workflow with side effects. Central orchestration is a
feature, not a limitation. Decentralized peer handoffs can come later for
non-mutating workflows such as research or support-style Q&A.

### Do Not Replace Alice's Workspace Model with Sandbox Agents

OpenAI sandbox agents are relevant conceptually, because they package filesystem
and command capabilities as a controlled workspace. Alice already operates in
real repos and has checkpoint/path-guard/process-runner behavior.

Borrow the manifest/capability vocabulary, not the runtime.

## Proposed P15 Follow-Up Work

### A. RunConfig and SessionPolicy (#144/#146)

Define a run-scoped config object and a session policy enum. Apply it before
every model call.

Expected benefit:

- fewer accidental memory/session mixes
- clearer logs
- easier tests for `issue=0`, direct model switch, walking-agent, and fresh run

### B. Guardrail Registry (#144/#148)

Unify preflight, planner checks, path guard, validators, reviewer gate, and issue
close readiness under a shared guardrail result type.

Expected benefit:

- one dashboard surface for "why this run skipped/retried/blocked"
- lower token waste from explicit blocking guardrails

### C. Trace Spans (#148)

Add `trace_id`, `span_id`, `parent_span_id`, `span_type`, `phase`, `model`,
`tokens`, `cost`, `status`, and `metadata_json` to the persisted execution
record.

Expected benefit:

- phase usage becomes explainable rather than only aggregated
- cache hit issues can be tied to concrete spans
- retries and duplicated planner/reviewer calls become visible

### D. Runner Loop Refactor (#120/#144)

Move Hermes from callback-heavy lifecycle handling toward an explicit runner
loop with typed events.

Expected benefit:

- simpler FSM
- fewer "task done but issue still open" surprises
- clearer continuation/retry semantics

### E. Role Descriptors (#120)

Introduce role descriptors for Planner, Executor, Reviewer, Preflight, and
GitHub sync.

Expected benefit:

- model selection and prompt ownership become explicit
- future Codex/Claude/OpenAI backend changes are less invasive

## 補充：未被點名但具體可借的 primitive（2026-05-02 二次掃描）

第一輪 doc 寫得到「Runner / RunConfig / SessionPolicy / Guardrails / Trace / Manager pattern」這些**抽象概念**，下面是再掃 SDK source（v0.15.0 at HEAD）後抓到、原 doc 沒明確點名、但**直接可寫成 Go 程式碼**的具體 primitive。每個都對應一個 hermes-v2.md 的 phase。

### A. `tool_use_behavior=StopAtTools(...)`（agent.py:298）— 對應 [hermes-v2 §2.7](hermes-v2.md)

**SDK 做法**：在 `Agent` 上設 `tool_use_behavior` 為 `"run_llm_again"` / `"stop_on_first_tool"` / `StopAtTools(["emit_plan"])` / callable，**結構性**控制何時停止 model loop。

**對應 Alice 痛點**：Planner 現在靠 prompt 反覆叮嚀「OUTPUT ONLY JSON, NO PROSE」防止它跑去 grep 檔案。容易被忽略。改成「只要呼叫 `emit_plan` tool 就停止」是結構性控制，不靠 model 紀律。

**Go 對應**：在 `CallPlan` 路徑 / Planner session 加一個 special tool `emit_plan`（schema = sub-task JSON array），CLI 偵測到該 tool call 就終止。比現在的 `--max-turns 3` + JSON 解析 retry 更乾淨。

### B. `Agent.as_tool()` 語意（agent.py:472）— 補充 §6 Manager pattern

**SDK 細節**：用 `as_tool()` 包子 agent 跟 handoff **不同**——子 agent 只拿到 `parent` 給的 generated input，**不繼承完整對話 transcript**。回 `RunResult`，supports `custom_output_extractor`。

**這對 Alice 的意義**：Reviewer 現在跨 backend（Claude→Codex 互查），靠的是「**新開 session**」隔離。但同 backend 的 reviewer 沒有獨立性保證——目前是用 prompt 寫「只能 Read，不能 Edit/Write」。`as_tool` 模式是把 reviewer 包成「只看 executor 結果文字 + plan，不繼承 executor session」的 sub-agent，**結構性**確保獨立性，不靠 prompt 紀律。

**對應 hermes-v2 §2.2**：reviewer in-session 可行性研究的一個分支——同 backend reviewer 用 `as_tool` 模式而非 fresh session。

### C. `is_enabled` per tool runtime gating（tool.py）— 對應 [hermes-v2 §2.2](hermes-v2.md)

**SDK 做法**：`Tool` 上掛 `is_enabled: bool | Callable(ctx, agent)`，runtime 決定該 tool 是否暴露給 model。

**對應 Alice 痛點**：Executor 拿到全工具集（Read/Edit/Write/Bash）。Reviewer 同 backend 也是。差異靠 prompt 表達，model 仍可叫 Edit。

**Go 對應**：spawning `claude` 子程序時，根據 role 動態組 `--allowed-tools` / `--disallowed-tools` flag。Reviewer 永遠加 `--disallowed-tools Edit,Write,Bash`。比 prompt 強。

### D. Parallel input guardrail（guardrail.py:72）— 對應 [hermes-v2 §2.6.A](hermes-v2.md) preflight

**SDK 做法**：input guardrails 預設 `run_in_parallel=True`。Guardrail 跟 main agent 並行啟動，guardrail tripwire 命中就 cancel main agent。

**對應 Alice 痛點**：Phase 2.6.A 的 preflight 設計是**先跑 Haiku、再決定**。並行設計是 Haiku **跟 Planner 同步啟動**，Haiku 判定「issue 已完成」就 cancel Planner。**延遲零**，only false-negative 造成的 Haiku 額外開銷。

**Go 對應**：`startHermesTaskWithIssueTier` 在 spawn Planner CLI 之前 fork 一個 goroutine 跑 Haiku check，main goroutine 繼續啟 Planner。Haiku 結果回來 trip 時呼叫 ctx.Cancel() 砍 Planner。比 sequential preflight 快 10-30 秒。

### E. `RunState.to_state()` 序列化（run_state.py:183）— 對應 [hermes-v2 §2.5](hermes-v2.md) partial retry

**SDK 做法**：`RunState` 完全可序列化成 JSON。`result.to_state()` → 存檔 → `Runner.run(agent, state)` 從任意 sub-task 邊界 resume。用於 human-in-the-loop approval。

**對應 Alice 痛點**：task_retry 砍掉重練，accumulated state 整個被 `UpdateAccumulated(taskID, "")` 清空。Phase 2.5 的設計三選項（per-sub-task tagging / operator pause / plan diff）其實都比這個簡單——**直接採用 SDK 的 state-snapshot pattern**：每個 sub-task 結束 snapshot 整個 RunState，retry 時用 `Runner.run(agent, state)` 從 snapshot 重啟，跳過已 done sub-task。

**Go 對應**：Hermes `TaskState` 已大致可序列化（DB 持久化）。需要的是 `LoadFromState(taskID, fromSubTaskIdx int) error` 接續邏輯。

### F. `RequestUsage[]` per-call list（usage.py）— 對應 [hermes-v2 §2.4](hermes-v2.md) cache hit monitoring

**SDK 做法**：`Usage.request_usage_entries: list[RequestUsage]`——**每個 API request 一筆**，含 `input_tokens_details.cached_tokens` 細項。不只是 phase rollup。

**對應 Alice 痛點**：目前 `PhaseUsages` 只到 phase 級別加總。無法回答「walking-agent 第 7 個 sub-task 的真實 cache hit %」。

**Go 對應**：`PhaseUsage` 之外新增 `RequestUsage` slice，每次 `[agent] done` log 寫一筆 row（已有 log，DB schema 加表即可）。對於 issue #305「6M token 怎麼花」這種 postmortem 直接見效。

### G. Deterministic prompt cache key resolver（prompt_cache_key.py）— 補充 §3 SessionPolicy

**SDK 做法**：用 deterministic function 從 grouping + session 產生 `prompt_cache_key`，存在 `RunState._generated_prompt_cache_key`，resumed runs 重用。**取代 cache 行為的 heuristic**。

**對應 Alice 痛點**：walking-agent 的 watermark heuristic 一直在跟 transcript size 鬥（120K threshold 太緊、Codex 數字不同義…）。這不是 Alice 獨有問題——SDK 已經把它解成「session 自己負責 cache key 生成」。

**對應**：Walking-agent Phase 2 的設計重構——從「watermark guard」變成「`Session` 物件持有 cache key + transcript，Engine 不關心」。原 doc §3 SessionPolicy 是對的方向，但沒提到 cache key resolver 是落實這方向的具體機制。

## 跟 hermes-v2.md 的對照矩陣

| SDK primitive | hermes-v2 phase | 收益估計 |
|---|---|---|
| StopAtTools | §2.7 granularity | Planner JSON retry 機率↓，省幾乎全部 retry token |
| as_tool() | §2.2 reviewer in-session | 解 reviewer-vs-executor 工具隔離問題 |
| is_enabled | §2.2 reviewer | 同上，更小改動 |
| Parallel guardrail | §2.6.A preflight | 砍 30-60s latency on preflight skip |
| Serializable RunState | §2.5 partial retry | 直接解 task_retry 砍掉重練問題 |
| RequestUsage[] | §2.4 cache hit monitoring | issue postmortem 用，可見性提升 |
| Cache key resolver | walking-agent (Phase 2) | 重構 watermark heuristic 為 Session 抽象 |

## 補充修正：原 §6 的微調

原文：「Hermes 必須保留中央控制」「decentralized handoffs 太大」——同意。但這個結論不該蓋掉「**Manager pattern + as_tool**」這個更細的子結構。原 §6 把「manager 模式」跟「decentralized handoff」二分，實際 SDK 還有第三種：**manager 用 as_tool 包 specialized 子 agent**，這個跟 Hermes Brain-Executor 完全 isomorphic，且**比 Hermes 現況更乾淨**——因為子 agent 拿到的 input 是 parent 顯式 generated 的（如 Planner 給 Executor 的 sub-task description），不繼承完整 chat。Alice 已經在做這件事，只是沒有這個 vocabulary。

## 對 P15 工作項的具體影響

原 doc 的 P15 follow-up A-E（RunConfig、Guardrail Registry、Trace Spans、Runner Loop、Role Descriptors）都對。建議**插入 F**：

### F. Serializable RunState + 從 snapshot resume（#149 / hermes-v2 §2.5）

把 `TaskState` 從「DB persisted record」升級成「**runtime resumable state**」。新增 `Runner.RunFromState(state, fromIdx)` 入口，task_retry 改成 snapshot → diff plan → restart from idx 而非從零。

預期收益：直接解用戶反映的「issue 跑 3-4 次、6M+ token」問題的核心。



The OpenAI Agents SDK is useful to Alice as an architectural reference, not as a
runtime dependency.

The best ideas to borrow are:

1. `Runner` as an explicit loop.
2. `RunConfig` and typed run context.
3. One session strategy per run.
4. Guardrails as first-class runtime events.
5. Trace/span observability.
6. Manager-controlled agents-as-tools instead of decentralized handoffs.

These ideas fit the current P15 milestone and should help Alice make Hermes
cheaper, easier to debug, and less surprising for GitHub issue workflows.
