#!/usr/bin/env bash
# Install Claude Code hooks for Alice integration
# Usage: ./scripts/install-hooks.sh [alice-api-url]
#
# This script adds hooks to ~/.claude/settings.json so that Claude Code
# sends session events to Alice for auditing and tracking.

set -euo pipefail

ALICE_URL="${1:-http://localhost:8082}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
HOOK_SCRIPT="$SCRIPT_DIR/claude-hook.sh"
SETTINGS_FILE="$HOME/.claude/settings.json"

# Ensure hook script is executable
chmod +x "$HOOK_SCRIPT"

echo "Installing Claude Code hooks for Alice..."
echo "  Hook script: $HOOK_SCRIPT"
echo "  Alice API:   $ALICE_URL/api/hooks/claude-code"
echo "  Settings:    $SETTINGS_FILE"
echo ""

# Check dependencies
if ! command -v jq &> /dev/null; then
    echo "Error: jq is required. Install with: brew install jq"
    exit 1
fi

if ! command -v python3 &> /dev/null; then
    echo "Error: python3 is required for transcript parsing."
    exit 1
fi

# Create settings directory if needed
mkdir -p "$(dirname "$SETTINGS_FILE")"

# Create settings file if it doesn't exist
if [ ! -f "$SETTINGS_FILE" ]; then
    echo '{}' > "$SETTINGS_FILE"
fi

# Build the hook command
HOOK_CMD="ALICE_HOOK_URL=${ALICE_URL}/api/hooks/claude-code bash ${HOOK_SCRIPT}"

# Build hooks configuration (correct nested format: event -> matcher groups -> hooks array)
HOOKS_CONFIG=$(jq -n \
    --arg cmd "$HOOK_CMD" \
    '{
        hooks: {
            Stop: [
                {
                    hooks: [
                        {
                            type: "command",
                            command: $cmd
                        }
                    ]
                }
            ],
            SessionEnd: [
                {
                    hooks: [
                        {
                            type: "command",
                            command: $cmd
                        }
                    ]
                }
            ],
            UserPromptSubmit: [
                {
                    hooks: [
                        {
                            type: "command",
                            command: $cmd
                        }
                    ]
                }
            ]
        }
    }')

# Merge with existing settings (preserving existing keys)
UPDATED=$(jq -s '.[0] * .[1]' "$SETTINGS_FILE" <(echo "$HOOKS_CONFIG"))
echo "$UPDATED" > "$SETTINGS_FILE"

echo "Done! Claude Code hooks installed."
echo ""
echo "Hooks configured for events:"
echo "  - Stop          (sends complete session on task completion)"
echo "  - SessionEnd    (sends complete session on session end)"
echo "  - UserPromptSubmit (sends lightweight active ping)"
echo ""
echo "To verify, check your settings:"
echo "  cat $SETTINGS_FILE | jq .hooks"
echo ""
echo "To test manually:"
echo "  echo '{\"session_id\":\"test\",\"hook_event_name\":\"Stop\",\"transcript_path\":\"/tmp/test.jsonl\",\"cwd\":\"/tmp\"}' | bash $HOOK_SCRIPT"
echo ""
echo "To uninstall, remove the 'hooks' key from $SETTINGS_FILE"
