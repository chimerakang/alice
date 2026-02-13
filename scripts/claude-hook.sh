#!/usr/bin/env bash
# Claude Code Hook Script for Alice
# Reads hook event JSON from stdin, parses transcript, sends to Alice API.
#
# Install: Run ./scripts/install-hooks.sh or manually add to ~/.claude/settings.json
# Events handled: Stop, SessionEnd, UserPromptSubmit
#
# Environment variables:
#   ALICE_HOOK_URL  - Alice API endpoint (default: http://localhost:8082/api/hooks/claude-code)
#   ALICE_SKIP_HOOKS - Set to "1" to skip hook processing (used by Alice itself)

set -euo pipefail

# Skip if Alice itself is the caller (avoids duplicate logging for Telegram sessions)
if [ "${ALICE_SKIP_HOOKS:-}" = "1" ]; then
    exit 0
fi

ALICE_API="${ALICE_HOOK_URL:-http://localhost:8082/api/hooks/claude-code}"

# Read hook event from stdin
HOOK_JSON=$(cat)

# Extract fields
SESSION_ID=$(echo "$HOOK_JSON" | jq -r '.session_id // empty')
HOOK_EVENT=$(echo "$HOOK_JSON" | jq -r '.hook_event_name // empty')
TRANSCRIPT_PATH=$(echo "$HOOK_JSON" | jq -r '.transcript_path // empty')
CWD=$(echo "$HOOK_JSON" | jq -r '.cwd // empty')
# Claude Code hook JSON uses "prompt" (not "user_prompt") for UserPromptSubmit events
USER_PROMPT=$(echo "$HOOK_JSON" | jq -r '.prompt // empty')

# Exit early if missing session_id
if [ -z "$SESSION_ID" ]; then
    exit 0
fi

# Detect source: VS Code vs terminal
# Claude Code hooks don't inherit TERM_PROGRAM, so we use multiple heuristics:
# 1. Check if the user prompt contains <ide_ tags (VS Code injects these)
# 2. Check transcript JSONL for type:"user" (VS Code) vs type:"human" (terminal)
# 3. Fall back to TERM_PROGRAM if available
detect_source() {
    local prompt="${1:-}"
    local transcript="${2:-}"

    # VS Code injects <ide_ tags into prompts
    if echo "$prompt" | grep -q '<ide_'; then
        echo "vscode"
        return
    fi

    # Check TERM_PROGRAM (works when hooks inherit shell env)
    if [ "${TERM_PROGRAM:-}" = "vscode" ]; then
        echo "vscode"
        return
    fi

    # Peek at transcript for VS Code format (type:"user" instead of type:"human")
    if [ -n "$transcript" ] && [ -f "$transcript" ]; then
        if head -30 "$transcript" | grep -q '"type"[[:space:]]*:[[:space:]]*"user"'; then
            echo "vscode"
            return
        fi
    fi

    echo "terminal"
}

# Strip <ide_opened_file>...</ide_opened_file> and <ide_selection>...</ide_selection> tags
# These are VS Code metadata injected into prompts, not actual user input
clean_prompt() {
    local prompt="$1"
    echo "$prompt" | sed -E \
        -e 's/<ide_opened_file>[^<]*<\/ide_opened_file>[[:space:]]*//' \
        -e 's/<ide_selection>[^<]*<\/ide_selection>[[:space:]]*//' \
        -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//'
}

SOURCE=$(detect_source "$USER_PROMPT" "$TRANSCRIPT_PATH")
CLEAN_PROMPT=$(clean_prompt "$USER_PROMPT")

# Handle UserPromptSubmit: lightweight "session active" ping
if [ "$HOOK_EVENT" = "UserPromptSubmit" ]; then
    curl -s -X POST "$ALICE_API" \
        -H "Content-Type: application/json" \
        -d "$(jq -n \
            --arg sid "$SESSION_ID" \
            --arg src "$SOURCE" \
            --arg pd "$CWD" \
            --arg up "$CLEAN_PROMPT" \
            '{
                session_id: $sid,
                event: "session_active",
                source: $src,
                project_dir: $pd,
                user_prompt: $up
            }')" \
        > /dev/null 2>&1 || true
    exit 0
fi

# Handle Stop / SessionEnd: parse transcript and send complete session
if [ "$HOOK_EVENT" != "Stop" ] && [ "$HOOK_EVENT" != "SessionEnd" ]; then
    exit 0
fi

# Check transcript file exists
if [ -z "$TRANSCRIPT_PATH" ] || [ ! -f "$TRANSCRIPT_PATH" ]; then
    exit 0
fi

# Parse JSONL transcript using python3 to extract session summary
SUMMARY=$(python3 -c "
import json, sys, re

transcript_file = sys.argv[1]
user_prompts = []
assistant_responses = []
thinking_blocks = []
tool_calls = []
files_changed = set()
detected_source = 'terminal'

# Regex patterns for IDE-injected metadata tags
IDE_TAG_PATTERNS = [
    re.compile(r'<ide_opened_file>.*?</ide_opened_file>\s*', re.DOTALL),
    re.compile(r'<ide_selection>.*?</ide_selection>\s*', re.DOTALL),
]

def clean_ide_tags(text):
    \"\"\"Strip VS Code IDE metadata tags from prompt text.\"\"\"
    for pat in IDE_TAG_PATTERNS:
        text = pat.sub('', text)
    return text.strip()

with open(transcript_file, 'r') as f:
    for line in f:
        line = line.strip()
        if not line:
            continue
        try:
            entry = json.loads(line)
        except json.JSONDecodeError:
            continue

        msg_type = entry.get('type', '')

        # Detect VS Code format: uses type 'user' instead of 'human'
        if msg_type == 'user':
            detected_source = 'vscode'

        # Extract content - handle both terminal format (content field)
        # and VS Code format (message.content field)
        if msg_type in ('human', 'user'):
            content = entry.get('content', '')
            # VS Code format: content is in message.content
            msg = entry.get('message', {})
            if isinstance(msg, dict) and msg.get('content'):
                content = msg['content']
            if isinstance(content, list):
                for block in content:
                    if isinstance(block, dict) and block.get('type') == 'text':
                        text = clean_ide_tags(block.get('text', ''))
                        if text:
                            user_prompts.append(text)
            elif isinstance(content, str) and content:
                text = clean_ide_tags(content)
                if text:
                    user_prompts.append(text)

        elif msg_type == 'assistant':
            content = entry.get('content', '')
            msg = entry.get('message', {})
            if isinstance(msg, dict) and msg.get('content'):
                content = msg['content']
            if isinstance(content, list):
                for block in content:
                    if isinstance(block, dict):
                        if block.get('type') == 'text':
                            assistant_responses.append(block.get('text', ''))
                        elif block.get('type') == 'thinking':
                            thinking_blocks.append(block.get('text', ''))
                        elif block.get('type') == 'tool_use':
                            tc = {
                                'tool_name': block.get('name', ''),
                                'input': {},
                                'status': 'success',
                                'duration_ms': 0
                            }
                            tool_calls.append(tc)
                            inp = block.get('input', {})
                            if block.get('name') in ('Write', 'Edit') and 'file_path' in inp:
                                files_changed.add(inp['file_path'])
            elif isinstance(content, str) and content:
                assistant_responses.append(content)

summary = {
    'user_prompt': user_prompts[0] if user_prompts else '',
    'agent_response': assistant_responses[-1][:2000] if assistant_responses else '',
    'thinking_content': thinking_blocks[-1][:2000] if thinking_blocks else '',
    'tool_calls': tool_calls[:50],
    'files_changed': list(files_changed),
    'success': True,
    'detected_source': detected_source,
}

print(json.dumps(summary))
" "$TRANSCRIPT_PATH" 2>/dev/null || echo '{}')

# Use transcript-detected source if available (more reliable than env-based detection)
TRANSCRIPT_SOURCE=$(echo "$SUMMARY" | jq -r '.detected_source // empty' 2>/dev/null)
if [ -n "$TRANSCRIPT_SOURCE" ] && [ "$TRANSCRIPT_SOURCE" != "null" ]; then
    SOURCE="$TRANSCRIPT_SOURCE"
fi

# Build and send the complete session payload
curl -s -X POST "$ALICE_API" \
    -H "Content-Type: application/json" \
    -d "$(echo "$SUMMARY" | jq \
        --arg sid "$SESSION_ID" \
        --arg src "$SOURCE" \
        --arg pd "$CWD" \
        --arg tp "$TRANSCRIPT_PATH" \
        --arg ts "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
        '{
            session_id: $sid,
            event: "session_complete",
            source: $src,
            project_dir: $pd,
            transcript_path: $tp,
            timestamp: $ts,
            user_prompt: .user_prompt,
            agent_response: .agent_response,
            thinking_content: .thinking_content,
            tool_calls: .tool_calls,
            files_changed: .files_changed,
            success: .success,
            duration_ms: 0,
            tokens_input: 0,
            tokens_output: 0
        }')" \
    > /dev/null 2>&1 || true
