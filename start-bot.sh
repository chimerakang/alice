#!/bin/bash
set -eu
PROJECT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$PROJECT_DIR"
ALICE_SERVICE="gui/$(id -u)/local.chimera.alice"
# Build before stopping the working service. Atomic replacement preserves rollback
# on compilation failure and prevents starting a partially written executable.
ALICE_BUILD="$(mktemp "$PROJECT_DIR/.alice-build.XXXXXX")"
trap 'rm -f "$ALICE_BUILD"' EXIT
echo "🏗️ 編譯 bot..."
go build -o "$ALICE_BUILD" ./cmd/alice
mv -f "$ALICE_BUILD" "$PROJECT_DIR/alice"
if launchctl print "$ALICE_SERVICE" >/dev/null 2>&1; then
    # launchd runs a copy outside the project volume. Install it from this shell:
    # a launchd-spawned cp is denied read access to a removable volume by macOS privacy
    # controls, so the service cannot refresh its own runtime.
    ALICE_SUPPORT_DIR="${ALICE_SUPPORT_DIR:-$HOME/Library/Application Support/Alice}"
    ALICE_RUNTIME="$ALICE_SUPPORT_DIR/alice-runtime"
    # A stable signing identity keeps the volume-access grant across rebuilds. Ad-hoc
    # builds get a new code identity each time, and the service then blocks at startup
    # on a consent dialog. Set ALICE_CODESIGN_IDENTITY in the environment or in .env.
    ALICE_CODESIGN_IDENTITY="${ALICE_CODESIGN_IDENTITY:-}"
    if [ -z "$ALICE_CODESIGN_IDENTITY" ] && [ -r "$PROJECT_DIR/.env" ]; then
        ALICE_CODESIGN_IDENTITY="$(sed -n 's/^ALICE_CODESIGN_IDENTITY=//p' "$PROJECT_DIR/.env" | tail -n 1)"
        ALICE_CODESIGN_IDENTITY="${ALICE_CODESIGN_IDENTITY%\"}"
        ALICE_CODESIGN_IDENTITY="${ALICE_CODESIGN_IDENTITY#\"}"
    fi
    ALICE_CODESIGN_IDENTIFIER="${ALICE_CODESIGN_IDENTIFIER:-${ALICE_SERVICE##*/}}"
    mkdir -p "$ALICE_SUPPORT_DIR"
    ALICE_RUNTIME_NEW="$(mktemp "$ALICE_SUPPORT_DIR/.alice-runtime.XXXXXX")"
    trap 'rm -f "$ALICE_BUILD" "$ALICE_RUNTIME_NEW"' EXIT
    cp "$PROJECT_DIR/alice" "$ALICE_RUNTIME_NEW"
    chmod 755 "$ALICE_RUNTIME_NEW"
    if [ -n "$ALICE_CODESIGN_IDENTITY" ]; then
        echo "🔏 簽署 runtime ($ALICE_CODESIGN_IDENTIFIER)..."
        codesign --force --timestamp=none --sign "$ALICE_CODESIGN_IDENTITY" \
            --identifier "$ALICE_CODESIGN_IDENTIFIER" "$ALICE_RUNTIME_NEW"
    else
        echo "⚠️ 未設定 ALICE_CODESIGN_IDENTITY：runtime 為 ad-hoc 簽章，macOS 可能再次要求卷宗存取授權" >&2
    fi
    mv -f "$ALICE_RUNTIME_NEW" "$ALICE_RUNTIME"
    echo "🔄 透過 launchd 重啟新版 Alice..."
    launchctl kickstart -k "$ALICE_SERVICE"
else
    echo "🔌 停止現有 bot 進程..."
    pkill -f '^\./alice($| )' || true
    sleep 1
    nohup ./alice >> alice.log 2>&1 &
fi
# A live PID alone is not readiness. The health endpoint includes Telegram polling,
# which reports ready only after the first long poll returns: up to 60s on a quiet
# bot, on top of startup. Allow for both, as the watchdog does.
ALICE_READY_TIMEOUT="${ALICE_READY_TIMEOUT:-150}"
ALICE_READY_DEADLINE=$(( $(date +%s) + ALICE_READY_TIMEOUT ))
while [ "$(date +%s)" -lt "$ALICE_READY_DEADLINE" ]; do
    if curl --silent --fail --max-time 2 http://127.0.0.1:8082/api/health >/dev/null; then
        echo "✅ Alice 與 Telegram polling 已就緒"
        exit 0
    fi
    sleep 2
done
echo "❌ Alice 未通過健康檢查，請檢查服務日誌" >&2
exit 1
