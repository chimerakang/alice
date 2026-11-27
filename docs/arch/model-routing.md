# Model Routing Architecture

Alice uses dynamic model routing to balance cost, latency, and quality. The current design favors sticky sessions so follow-up turns keep the same backend context unless the user explicitly changes mode or the session expires.

## Four-Tier Routing Priority

1. User command override:
   - `/fast` selects the fast model.
   - `/smart` selects the balanced model.
   - `/deep` selects the deep model.
   - `/gpt-fast`, `/gpt-smart`, and `/gpt-deep` select Codex/GPT-backed modes when `ai_backend` is `multi`.
   - `/plan` selects the planner/executor split when configured.
   - `/auto` returns to automatic routing.
   - `/clear` clears active context and sticky state.
2. Sticky session:
   - When `sticky_session` is enabled and the session is within `session_idle_timeout_min`, Alice keeps the current model/backend session.
   - Short continuation messages can inherit the active model.
3. Hybrid triage:
   - New topics can use local complexity heuristics and, when configured, a lightweight triage model.
4. Static fallback:
   - Rules in `internal/app/security.go` and selection logic in `internal/app/agent.go` route obvious fast/deep tasks.
   - Unknown tasks default to the smart model.

## Session Lifecycle

Routing is tied to backend session reuse. Alice keeps using the active backend session while the selected model remains compatible, so Claude/Codex can preserve tool history, file edits, and recent context.

The session is cleared or treated as fresh when:

- The user sends `/clear`.
- The user switches to a different explicit mode such as `/fast`, `/deep`, or `/gpt-smart`.
- The sticky session exceeds `session_idle_timeout_min`.
- The routing layer detects a new topic and no explicit override is active.
- The backend owner changes between Claude, Codex, or planner/executor mode.

`/auto` clears the explicit model override but does not delete usage statistics. The next user message goes through sticky/follow-up detection first, then triage/static fallback if needed.

## Configuration

Relevant `config.json` / environment-backed fields live under `model_routing`:

```json
{
  "model_routing": {
    "enable_dynamic_routing": true,
    "sticky_session": true,
    "session_idle_timeout_min": 5,
    "use_gpt4o_mini_for_triage": true,
    "fast_model": "claude-haiku-4-5-20251001",
    "smart_model": "claude-sonnet-4-6",
    "deep_model": "claude-opus-4-6",
    "codex_fast_model": "gpt-5.4-mini",
    "codex_smart_model": "gpt-5.4",
    "codex_deep_model": "gpt-5.5",
    "plan_model": "gpt-5.4",
    "execute_model": "gpt-5.4-mini"
  }
}
```

Do not edit `config.json` directly. Use `config.example.json`, environment variables, or docs when changing examples.

## Implementation Map

- `internal/app/agent.go` handles model selection, sticky session behavior, decision logging, and backend calls.
- `internal/app/telegram.go` handles Telegram commands such as `/fast`, `/smart`, `/deep`, `/auto`, and `/clear`.
- `internal/app/security.go` defines default static model routes.
- `internal/app/config_types.go` defines routing config structs.
- `internal/app/multi_backend_client.go` and `internal/app/codex_client.go` participate when `ai_backend` is `multi`.

## Web API

Dashboard-facing endpoints live in `internal/app/web.go`:

- `GET /api/model-routing/status` returns whether dynamic routing is enabled, configured model names, default model, sticky-session settings, and current per-agent preferences.
- `POST /api/model-routing/set` sets a chat/thread routing mode. Accepted modes include `fast`, `smart`, `deep`, `gpt-fast`, `gpt-smart`, `gpt-deep`, `plan`, `auto`, or an explicit model name.
- `GET /api/costs/by-model`, `GET /api/costs/summary`, and `GET /api/costs/savings` expose model cost and savings telemetry used by dashboard components.
- Decision APIs such as `GET /api/decisions/recent`, `GET /api/decisions/range`, and `GET /api/decisions/search` include routing fields when present.

When changing routing API shape, update `frontend/src/types/alice.ts`, `frontend/src/lib/api.ts`, and any dashboard component that reads model or routing metadata.

## Decision Logs

Routing metadata is stored with decision logs:

- `model`
- `routing_reason`
- `routing_latency_ms`

Use this telemetry to validate cost, latency, and routing quality before changing defaults.

## Detailed Reference

The older full implementation note is preserved at [docs/DYNAMIC_MODEL_ROUTING.md](../DYNAMIC_MODEL_ROUTING.md). Prefer this file for current context loading; use the older note only when historical detail is needed.

## Fleet: tiered models, Scout backend and escalation

Fleet tasks are one-shot, so none of the chat machinery above (sticky sessions,
follow-up detection, `/fast` `/deep`) applies. Fleet reuses only the tier
models in `model_routing` and the local `ClassifyComplexity` heuristic; the
command layer never calls a model to decide.

Tiers, lowest to highest: `fast_model`, `smart_model`, `deep_model`,
`flagship_model`. `flagship_model` is Fleet-only and optional: when empty the
tier does not exist and both selection and escalation stop at `deep`.

### Choosing a model at dispatch (`internal/app/fleet_routing.go`)

1. A task with a model (`--model`, the dispatch form, the API) is used as is.
2. Otherwise, if `fleet.auto_model` is not `false` and `smart_model` is set,
   the commander picks a tier:
   - the highest tier any source-issue label maps to in `fleet.tier_labels`
     (label to tier name, case-insensitive; no label names live in code);
   - else `ClassifyComplexity` on the goal (and issue title/body): a trivial
     match goes to `fast`, a complex match to `deep`;
   - else `smart`. When unsure it never guesses lower.
3. The choice is recorded as a `model.selected` task event
   (`source`, `tier`, `model`, `reason`, `detail`) and shown on the task card,
   the Telegram card and the task page as "chosen by the commander: deep, …"
   or "chosen by you".

Setting `fleet.auto_model` to `false` restores the old behaviour (task model,
else the default model).

### Scout backend

`fleet.scout_backend`: `claude` (default), `codex`, or `auto`. In `auto` the
commander uses Codex only when the task's tier is listed in
`fleet.scout_codex_tiers` and that tier has a `codex_*_model`; otherwise
Claude. A task under any USD limit (its own budget, the project's, or the
fleet's `task_budget_usd` / `daily_budget_usd`) always gets Claude, because
Codex Scout does not report USD usage. The choice is a `scout.backend` event.

### Escalation on retry

Retrying a `failed` or `needs_decision` task whose model the commander chose
moves it up one configured tier (fast → smart → deep → flagship) and records a
`model.selected` event with reason `escalated`. It does not escalate when the
model was set by hand, the task is already on the top tier, or the status
reason contains one of `fleet.no_escalate_reasons` (default: budget limits,
lost agent process, quota / rate / usage limit).

The retry and the new model decision are written in one transaction before
the scheduler can start the next run. The runner consults Scout routing at
execution time to account for budget changes since dispatch. Set
`fleet.scout_codex_binary` to override the Codex CLI executable.
