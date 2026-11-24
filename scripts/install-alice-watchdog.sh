#!/bin/bash
set -eu
ALICE_SOURCE_DIR="$(cd "$(dirname "$0")" && pwd)"
ALICE_SERVICE="gui/$(id -u)/local.chimera.alice"
# Requires the existing bot LaunchAgent; this installer does not change credentials.
launchctl print "$ALICE_SERVICE" >/dev/null
ALICE_SUPPORT_DIR="$HOME/Library/Application Support/Alice"
ALICE_LOG_DIR="$HOME/Library/Logs/Alice"
ALICE_WATCHDOG_PLIST="$HOME/Library/LaunchAgents/local.chimera.alice-watchdog.plist"
mkdir -p "$ALICE_SUPPORT_DIR" "$ALICE_LOG_DIR"
install -m 755 "$ALICE_SOURCE_DIR/alice-watchdog.sh" "$ALICE_SUPPORT_DIR/watchdog.sh"
python3 - "$ALICE_WATCHDOG_PLIST" "$ALICE_SUPPORT_DIR/watchdog.sh" "$ALICE_LOG_DIR" <<'PY'
import plistlib,sys
from pathlib import Path
p,script,logs=map(Path,sys.argv[1:])
p.write_bytes(plistlib.dumps({'Label':'local.chimera.alice-watchdog','ProgramArguments':['/bin/bash',str(script)],'RunAtLoad':True,'StartInterval':30,'ProcessType':'Background','EnvironmentVariables':{'PATH':'/usr/bin:/bin:/usr/sbin:/sbin'},'StandardOutPath':str(logs/'watchdog.log'),'StandardErrorPath':str(logs/'watchdog-error.log')}))
PY
plutil -lint "$ALICE_WATCHDOG_PLIST"
if launchctl print "gui/$(id -u)/local.chimera.alice-watchdog" >/dev/null 2>&1; then
    launchctl bootout "gui/$(id -u)" "$ALICE_WATCHDOG_PLIST"
fi
launchctl bootstrap "gui/$(id -u)" "$ALICE_WATCHDOG_PLIST"
echo "Alice watchdog installed: three failed checks trigger restart; five-minute cooldown."
