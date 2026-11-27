# Fleet quota routing (#207)

Codex App Server documents `account/rateLimits/read` and `account/rateLimits/updated`, which expose account rate-limit snapshots through its public protocol. This is separate from the current `codex exec --json` integration. Use the App Server protocol with the CLI's existing managed login, without reading credential files.

Claude Agent SDK documents `RateLimitEvent` with status and optional utilization, window type and reset timestamp. It reports state during a run. No standalone subscription-quota query for the current Claude CLI integration was verified. Consume supported structured events when available. For windows without measurements, use a configured token allowance and period over recorded Fleet usage. Show the resulting percentage as an estimate, since token usage does not establish subscription capacity or account-wide consumption.

Sources:
- [Codex App Server](https://developers.openai.com/codex/app-server)
- [Claude Agent SDK reference](https://code.claude.com/docs/en/agent-sdk/python)

The Alice-side policy is in `internal/app/fleet_quota.go`. It takes snapshots and eligible backends as input. Unknown data preserves the normal preference when both backends are available. Detected exhaustion without a reset remains blocked until the provider refreshes its state. Error matching accepts caller-supplied expressions and a timestamp layout. Time-only human-readable resets need the provider date and timezone context before they can become absolute timestamps.

The accompanying alice-core changes record backend and model when available, preserve unattributed usage, and aggregate tokens, cost and distinct tasks by backend. Alice exposes these totals at `GET /api/fleet/usage/backends`. Pass RFC3339 `since` and `until` values for a half-open interval. The end defaults to now, and the start defaults to 24 hours before the end. An empty backend represents unattributed usage. The Fleet cost panel shows the breakdown for the selected period. With the currently pinned alice-core revision, the endpoint returns HTTP 501. The usage API is in alice-core commit `e8f76e2`. Publish the alice-core revision before pinning it in Alice so a clean checkout can resolve the module. For local development, a Go workspace containing both modules enables the endpoint.

Set `fleet.scout_quota.window_minutes`, `fleet.scout_quota.claude_tokens`, and `fleet.scout_quota.codex_tokens` to positive values to enable local estimates. In Scout auto mode, tasks already eligible for Codex can prefer Claude when it has a higher estimated remaining share. This setting uses a rolling window and only updates backend preference.

Set `fleet.scout_quota.max_transfers` to a positive value to enable automatic transfers in Scout auto mode when the linked Fleet runner supports them. A rejected Scout can requeue with a fresh session on an eligible alternate backend. Only runs started from the queue with the stored goal are eligible. The transfer count persists for the lifetime of the task.

## Remaining integration
- Publish normalized quota snapshots from supported CLI events and the Codex App Server. Store exhaustion and reset state across restarts. Allow configurable patterns, estimated allowances and periods.
- Give the scheduler an eligibility decision before allocating a run slot, with a wait outcome and optional next wake time. Record one owner notification per exhaustion episode. Ship and USD-limited Scout remain Claude-only.
- Check the alternate backend for recorded exhaustion before transferring and schedule a wake when capacity returns.
- Expose quota state to Alice's Fleet API and dashboard. Provider exhaustion and reset times are still unavailable at runtime.
