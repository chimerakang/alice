#!/usr/bin/env bash
# UserPromptSubmit hook forwarder for Alice Hermes experiment (#104).
#
# Reads Claude Code hook JSON from stdin, augments with a `source` hint derived
# from the parent process name, POSTs to Alice's observation endpoint, and
# emits the response so Claude Code proceeds normally.
#
# Fail-open: if Alice is offline or the request fails, emit `{}` so the hook
# never blocks the user's Claude Code session.

set -u

ALICE_ENDPOINT="${ALICE_HOOK_ENDPOINT:-http://localhost:8082/api/hooks/user-prompt-submit}"
TIMEOUT_SECONDS="${ALICE_HOOK_TIMEOUT:-3}"

# Detect whether this hook is running under VS Code vs terminal by inspecting
# the environment. VS Code's Claude Code extension sets TERM_PROGRAM=vscode.
if [[ "${TERM_PROGRAM:-}" == "vscode" ]]; then
  SOURCE_HINT="vscode"
elif [[ -n "${VSCODE_PID:-}" || -n "${VSCODE_IPC_HOOK:-}" ]]; then
  SOURCE_HINT="vscode"
elif [[ -t 0 || -n "${TERM:-}" ]]; then
  SOURCE_HINT="terminal"
else
  SOURCE_HINT="unknown"
fi

payload="$(cat)"
# Inject source hint as an extra field. jq is optional; fall back to raw forward.
if command -v jq >/dev/null 2>&1; then
  payload="$(printf '%s' "$payload" | jq --arg s "$SOURCE_HINT" '. + {source: $s}')"
fi

response="$(curl -sf -m "$TIMEOUT_SECONDS" -X POST "$ALICE_ENDPOINT" \
  -H 'Content-Type: application/json' \
  --data-binary "$payload" 2>/dev/null || true)"

if [[ -z "$response" ]]; then
  echo '{}'
else
  echo "$response"
fi
