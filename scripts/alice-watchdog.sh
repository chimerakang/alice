#!/bin/bash
set -eu
# Runs independently of Alice, so a stalled Go runtime cannot stop the check.
ALICE_SERVICE="gui/$(id -u)/local.chimera.alice"
ALICE_STATE_DIR="${ALICE_WATCHDOG_STATE_DIR:-$HOME/Library/Application Support/Alice/watchdog}"
mkdir -p "$ALICE_STATE_DIR"
# launchd does not run overlapping instances of a job.
# Never restart an intentionally unloaded service.
if ! service_status=$(launchctl print "$ALICE_SERVICE" 2>/dev/null); then exit 0; fi
now=$(date +%s)
pid=$(printf '%s\n' "$service_status" | sed -n 's/^[[:space:]]*pid = \([0-9]*\).*/\1/p')
pid=${pid:-starting}
previous_pid=''
if [ -r "$ALICE_STATE_DIR/pid" ]; then read -r previous_pid < "$ALICE_STATE_DIR/pid" || true; fi
if [ "$pid" != "$previous_pid" ]; then
    printf '%s\n' "$pid" > "$ALICE_STATE_DIR/pid"
    printf '%s\n' "$now" > "$ALICE_STATE_DIR/first_seen"
    printf '0\n' > "$ALICE_STATE_DIR/failures"
fi
if curl --silent --fail --max-time 5 http://127.0.0.1:8082/api/health >/dev/null; then
    printf '0\n' > "$ALICE_STATE_DIR/failures"
    exit 0
fi
# Allow startup authentication, command registration and the first 60s poll.
first_seen=$now
if [ -r "$ALICE_STATE_DIR/first_seen" ]; then read -r first_seen < "$ALICE_STATE_DIR/first_seen" || true; fi
case "$first_seen" in ''|*[!0-9]*) first_seen=$now ;; esac
if [ $((now - first_seen)) -lt 120 ]; then exit 0; fi
failures=0
if [ -r "$ALICE_STATE_DIR/failures" ]; then read -r failures < "$ALICE_STATE_DIR/failures" || true; fi
case "$failures" in ''|*[!0-9]*) failures=0 ;; esac
failures=$((failures + 1))
printf '%s\n' "$failures" > "$ALICE_STATE_DIR/failures"
if [ "$failures" -lt 3 ]; then exit 0; fi
last_restart=0
if [ -r "$ALICE_STATE_DIR/last_restart" ]; then read -r last_restart < "$ALICE_STATE_DIR/last_restart" || true; fi
case "$last_restart" in ''|*[!0-9]*) last_restart=0 ;; esac
if [ $((now - last_restart)) -lt 300 ]; then exit 0; fi
printf '%s Alice failed three health checks; requesting launchd restart\n' "$(date '+%Y-%m-%dT%H:%M:%S%z')" >&2
if launchctl kickstart -k "$ALICE_SERVICE"; then
    printf '%s\n' "$now" > "$ALICE_STATE_DIR/last_restart"
    printf '0\n' > "$ALICE_STATE_DIR/failures"
fi
