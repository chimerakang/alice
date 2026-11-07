# Telegram reliability issue drafts

GitHub creation is blocked: the GitHub integration returned HTTP 403 `Resource not accessible by integration` on 2026-11-07. No issue numbers have been assigned. These drafts preserve the requested issues for publication once access is restored.

## 1. Bound Telegram HTTP requests and support cancellation

Priority: high. Startup getMe, command registration, polling and sending use unbounded default HTTP requests. A stalled connection can block polling or the single outgoing queue indefinitely.

Acceptance: separate API and long-poll deadlines cover headers and body; shutdown cancels requests; logs redact request URLs; tests cover stalled headers and bodies and cancellation.

## 2. Report real polling health and recover a hung process externally

Priority: high. `/api/health` reports healthy and Telegram active without checking polling. launchd KeepAlive restarts only after process exit, so it cannot recover a living but unresponsive process.

Acceptance: return HTTP 503 for starting, failed, stale or stopped polling; expose last successful poll; independent launchd watchdog uses bounded local HTTP checks, restarts after three failures, respects intentionally unloaded services and applies a cooldown. Do not claim these defects alone establish the cause of the observed process-wide hang.

## 3. Back off polling failures and honor Telegram retry_after

Priority: high. Poll failures immediately loop; ok=false responses including conflict and rate limiting are silently ignored.

Acceptance: bounded exponential backoff, respect retry_after, reset after success, cancellation while waiting, tests for failure and recovery.

## 4. Bound synchronous send retries

Priority: medium. sendMessageSync's recursive 429 handler has no retry cap and defers closing responses until recursion unwinds.

Acceptance: at most three attempts with one overall deadline; close each response before waiting; cancellation interrupts retry_after; tests confirm attempt and close counts.

## 5. Build before restarting a launchd-managed bot

Priority: medium. start-bot.sh exits through kickstart before building whenever the service is loaded, silently reusing the old executable.

Acceptance: build a candidate before stopping the old service; atomically replace only on success; build failure preserves running service and binary; verify health after restart. Cover command order and build-failure behavior using fake executables rather than restarting production in tests.

## Implementation and deployment

The code fixes include automated failure tests. Runtime config and credentials remain unchanged. The external watchdog is prepared in `scripts/alice-watchdog.sh` with an explicit installer `scripts/install-alice-watchdog.sh`; it is not installed automatically. After deploying the fixed bot and confirming macOS permissions, run the installer to check health every 30 seconds. It requests a restart after three consecutive failures, with a five-minute cooldown and a two-minute grace for each newly observed process. Startup polling may take up to one 60-second long poll before becoming ready.
