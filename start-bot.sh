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
    echo "🔄 透過 launchd 重啟新版 Alice..."
    launchctl kickstart -k "$ALICE_SERVICE"
else
    echo "🔌 停止現有 bot 進程..."
    pkill -f '^\./alice($| )' || true
    sleep 1
    nohup ./alice >> alice.log 2>&1 &
fi
# A live PID alone is not readiness. The health endpoint includes Telegram polling.
for attempt in $(seq 1 30); do
    if curl --silent --fail --max-time 2 http://127.0.0.1:8082/api/health >/dev/null; then
        echo "✅ Alice 與 Telegram polling 已就緒"
        exit 0
    fi
    sleep 2
done
echo "❌ Alice 未通過健康檢查，請檢查服務日誌" >&2
exit 1
