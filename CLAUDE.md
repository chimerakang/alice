# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Alice is a Go-based Telegram bot that wraps Claude Code CLI as an AI agent for code assistance. It runs as a native process (bot + API) with a Docker-hosted React dashboard for monitoring AI decisions, tool executions, and project activity.

## Build & Run

```bash
# Build bot binary
go build -o alice ./cmd/alice

# Start bot (background)
nohup ./alice >> alice.log 2>&1 &

# Start dashboard (Docker)
docker compose up -d dashboard

# Rebuild dashboard after frontend changes
cd frontend && npm run build && cp -r dist/* ../web/
docker compose up -d --build dashboard
```

A Makefile is available — run `make help` for all targets.

## Configuration

**Note**: Alice uses Claude Code CLI as a subprocess, so you need to have `claude` CLI installed and authenticated. Alice does not use the Anthropic API directly.

Prerequisites:
1. Install Claude Code: https://code.claude.com/cli
2. Authenticate with your Claude account: `claude auth`

Config is loaded from `config.json` (see `config.example.json`), overridden by env vars:

| Env Var | Config Key | Required | Default |
|---------|-----------|----------|---------|
| `TELEGRAM_BOT_TOKEN` | `telegram_token` | Yes | — |
| `CLAUDE_MODEL` | `model` | No | `claude-sonnet-4-20250514` |
| `PROJECT_DIR` | `default_project_dir` | No | `.` |
| `ALLOWED_USER_IDS` | `allowed_user_ids` | No | `[]` (allow all) |
| `ALICE_ENABLE_PERSISTENCE` | `enable_persistence` | No | `true` |
| `ALICE_DATABASE_PATH` | `database_path` | No | `./data/alice.db` |
| `ALICE_DATA_RETENTION_DAYS` | `data_retention_days` | No | `30` |
| `OPENAI_API_KEY` | `openai_api_key` | No* | — |

*Required only if using multimedia features (voice/image processing)

## Deployment Architecture

```
┌─── User Access ────────────────────────────────────────────┐
│                                                            │
│  Telegram App ──→ Telegram API ──→ Alice Bot (native)      │
│                                     ↕                      │
│  Browser :3939 ──→ Docker nginx ──→ Alice Bot API :8082    │
│                    (React SPA)      (REST + WebSocket)     │
│                                                            │
└────────────────────────────────────────────────────────────┘
```

### Port Allocation (DO NOT CHANGE)

| Port | Service | Type | Description |
|------|---------|------|-------------|
| `8082` | Alice Bot | Native Go | REST API + WebSocket + static fallback |
| `3939` | Dashboard | Docker nginx | React SPA + reverse proxy → :8082 |

- Bot `web_port` in config.json **MUST be `8082`** — nginx.conf hardcodes proxy to `host.docker.internal:8082`
- Dashboard Docker maps `127.0.0.1:3939 → container:8082` (nginx internal port)
- Users access dashboard at `http://localhost:3939`

### Startup Sequence

```bash
# 1. Build bot
go build -o alice ./cmd/alice

# 2. Start bot (native, background)
nohup ./alice >> alice.log 2>&1 &

# 3. Start dashboard (Docker)
docker compose up -d dashboard
```

### Key Files

| File | Purpose |
|------|---------|
| `config.json` | Bot runtime config (tokens, ports) — **NOT in git** |
| `nginx.conf` | Dashboard reverse proxy config |
| `docker-compose.yml` | Dashboard + optional monitoring stack |
| `Dockerfile` | Dashboard image (nginx:alpine + web/ assets) |
| `web/` | Pre-built React SPA (vite build output) |
| `frontend/` | React source code (build with `cd frontend && npm run build`) |

### Project Structure

```
cmd/alice/main.go        — Entry point (thin wrapper calling app.Main())
internal/app/             — All application code (package app)
  main.go                 — Config, LoadConfig(), Main() initialization
  agent.go                — Agent core loop, tool/decision logging
  api.go                  — CLIClient, Claude Code CLI integration
  telegram.go             — TelegramBot, command handling, per-chat agents
  web.go                  — WebInterface, REST API, dashboard
  websocket.go            — WebSocket hub for real-time events
  storage.go              — SQLiteStorage persistence layer
  performance.go          — PerformanceMonitor, metrics collection
  security.go             — SecurityManager, rate limiting, PII detection
  checkpoint.go           — CheckpointManager, state snapshots
  multiagent.go           — AgentCoordinator, specialized agents
  git_integration.go      — GitManager, repository state tracking
  proto_converters.go     — Proto type conversion helpers
  tools.go                — ToolExecutor, BuildTools() (6 tools)
gen/                      — Generated protobuf/gRPC code
proto/                    — Proto definitions
web/                      — Static web assets (pre-built React SPA)
frontend/                 — React source (Vite + TypeScript + Tailwind)
docs/                     — Documentation
```

## Key Design Decisions

- **Per-chat isolation**: Each Telegram chat ID gets its own `Agent` instance with independent conversation history
- **Tool results go back as user messages**: `ToolResult` messages use `role: "user"` per the Anthropic API spec
- **file_patch requires unique match**: Exactly one occurrence of `old_text` must exist, otherwise it errors — prevents ambiguous edits
- **Agent communicates in Traditional Chinese** (繁體中文) by default via system prompt

## Dynamic Model Routing (Issue #72)

Alice supports dynamic model routing to optimize token costs by intelligently routing tasks to different Claude models based on complexity:

### Three-Tier Routing Priority

1. **User Command Override** (0ms latency)
   - `/fast` — Forces fast model (Haiku) for simple tasks
   - `/deep` — Forces deep model (Opus) for complex tasks
   - `/auto` — Returns to automatic routing mode

2. **AI Triage** (~300ms latency)
   - Uses OpenAI GPT-4o-mini to classify task complexity
   - Classifies as "fast" (simple tasks) or "deep" (complex tasks)
   - Requires: `openai_api_key` in config and `use_gpt4o_mini_for_triage: true`

3. **Default Model** (Static fallback)
   - Falls back to configured default model if routing disabled or AI triage fails

### Configuration

```json
{
  "model_routing": {
    "enable_dynamic_routing": false,
    "fast_model": "claude-haiku-4-5-20251001",
    "deep_model": "claude-opus-4-6",
    "use_gpt4o_mini_for_triage": false
  }
}
```

### Environment Variables

| Env Var | Purpose |
|---------|---------|
| `CLAUDE_MODEL` | Default model (fallback) |
| `OPENAI_API_KEY` | Required for GPT-4o-mini triage |

### Commands

```
/fast     — Fast mode (⚡ Haiku) - simple, one-off questions
/deep     — Deep mode (🧠 Opus) - complex analysis, multi-file refactoring
/auto     — Auto mode 🤖 - AI decides based on task complexity
/status   — Shows current model mode
```

### Implementation Details

- **Session Continuity**: When model changes, a new session is created (forces fresh context)
- **Last Used Model Tracking**: Agent remembers which model was last used to detect changes
- **Logging**: Model selection decisions are logged with tag `[telegram] model routing:`

### Web API Endpoints

#### GET `/api/model-routing/status`
Returns current model routing configuration and preferences for all active chats.

**Response**:
```json
{
  "enabled": true,
  "fast_model": "claude-haiku-4-5-20251001",
  "deep_model": "claude-opus-4-6",
  "default_model": "claude-sonnet-4-5-20250929",
  "use_gpt4o_mini_for_triage": true,
  "total_chats": 3,
  "preferences": {
    "chat_123456789_thread_1": {
      "mode": "fast",
      "model": "claude-haiku-4-5-20251001"
    },
    "chat_987654321_thread_0": {
      "mode": "auto",
      "model": "claude-sonnet-4-5-20250929"
    }
  },
  "timestamp": "2026-02-16T12:34:56Z"
}
```

#### POST `/api/model-routing/set`
Sets the model routing preference for a specific chat.

**Request**:
```json
{
  "chat_id": 123456789,
  "thread_id": 1,
  "mode": "fast"  // or "deep" or "auto"
}
```

**Response**:
```json
{
  "success": true,
  "chat_id": 123456789,
  "thread_id": 1,
  "mode": "fast",
  "model": "claude-haiku-4-5-20251001",
  "message": "Model routing mode set to fast (claude-haiku-4-5-20251001)",
  "timestamp": "2026-02-16T12:34:56Z"
}
```

**Authentication**: If `web_api_token` is configured, requires `Authorization: Bearer <token>` header.

### Expected Cost Savings

- ~40-50% reduction in token costs
- Simple tasks routed to Haiku (7-10x cheaper than Opus)
- Complex tasks routed to Opus for quality

## Task & Issue Management

### MASTER_TASKS.md Generation (`/task-sync`)

The `/task-sync` skill regenerates `docs/MASTER_TASKS.md` from GitHub Issues and Milestones automatically.

**Usage:**
```bash
/task-sync              # Regenerate and write to file
/task-sync --dry-run    # Preview without writing
```

**Mechanics:**
- Milestones represent Phases (P1, P2, ..., P14)
- Each milestone can have multiple issues
- Issues are organized by phase with completion status
- Supports sub-tasks parsed from issue body (`- [x]` / `- [ ]`)

### Issue Checklist Rules

**Principle: Sub-tasks are tracked independently**

- **Issue Status**: Determined by GitHub issue state (open/closed)
- **Sub-task Status**: Determined by checklist in issue body (`- [x]` / `- [ ]`)
- **Independence**: A closed issue can have incomplete sub-tasks, and vice versa
  - Example: `Issue #1` is ✅ closed, but body has `☐ documentation pending`
  - This means the core work is done, but cleanup tasks remain
- **Responsibility**: When closing an issue, update its body checklist if needed
  - But it's optional — unfinished sub-tasks are acceptable if issue is complete

**Checklist Format in Issue Body:**
```markdown
- [x] Completed task
- [ ] Pending task
- [x] Another done task
```

### Applicable to All Projects

These rules apply universally to any GitHub repository with milestones, enabling consistent task tracking across projects.

## Adding New Tools

1. Add a `ToolDef` entry in `BuildTools()` in `internal/app/tools.go`
2. Add a case in `ToolExecutor.Execute()` switch
3. Implement the handler method on `*ToolExecutor`

## Multi-Language Support (i18n) Guidelines

### Architecture Overview

Alice implements a **centralized i18n system** with SQLite persistence and in-memory caching to support multiple languages:

```
User Message
    ↓
TelegramBot.handleCommand()
    ↓
t.getLocalizedMessage(chatID, messageKey, templateVars)
    ↓
I18nManager.GetMessage(langCode, messageKey, templateVars)
    ↓
Memory Cache Hit? → Return cached value
Memory Cache Miss? → Query SQLite chat_language → Apply template → Cache → Return
```

### Supported Languages

| Language | Code | File | Messages |
|----------|------|------|----------|
| Traditional Chinese | `zh-TW` | `internal/app/i18n/zh-TW.json` | 271+ keys |
| English | `en` | `internal/app/i18n/en.json` | 271+ keys |

### Adding New User-Facing Text

**❌ DO NOT hardcode strings like this:**
```go
// WRONG - hardcoded string
t.send(key, "Token 使用量統計")
```

**✅ CORRECT - use message keys:**
```go
// CORRECT - localized message
msgKey := "token_usage_format"
msg := t.getLocalizedMessage(key.chatID, msgKey, map[string]string{
    "{input}": fmt.Sprintf("%d", stats.TotalInputTokens),
    "{output}": fmt.Sprintf("%d", stats.TotalOutputTokens),
})
t.send(key, msg)
```

### Workflow for New Features

1. **Identify all user-facing strings** in your new feature
   - Command responses in Telegram
   - Dashboard messages
   - Error messages
   - Status indicators

2. **Add message keys to language files:**
   ```
   internal/app/i18n/
   ├── zh-TW.json    # Add: "new_feature_message": "新功能訊息..."
   └── en.json       # Add: "new_feature_message": "New feature message..."
   ```

3. **Use message keys in code:**
   ```go
   msgKey := "new_feature_message"
   templatedMsg := t.getLocalizedMessage(key.chatID, msgKey, nil)
   t.send(key, templatedMsg)
   ```

4. **Support template variables** for dynamic content:
   ```json
   {
     "cost_report": "您的成本為 ${cost}，節省 ${savings}%"
   }
   ```
   ```go
   msg := t.getLocalizedMessage(chatID, "cost_report", map[string]string{
       "${cost}": fmt.Sprintf("%.2f", actualCost),
       "${savings}": fmt.Sprintf("%.1f", savingsPercent),
   })
   ```

### Message Key Naming Convention

Use **snake_case** for message keys, organized by feature:

```
token_usage_format          → Token usage display
error_get_cost              → Error when fetching cost data
model_distribution_title    → Model distribution chart title
task_savings_amount         → Savings amount display
mode_switched_fast          → Mode switched notification
usage_stats_by_model        → Usage statistics by model header
```

### Implementation Details

**Backend (Go):**
- File: `internal/app/i18n_manager.go` (184 lines)
- Interface: `I18nManager` with `GetMessage()` method
- Cache: sync.RWMutex-protected map for O(1) lookups
- SQLite: `chat_language` table stores per-chat preferences
- Fallback: Default to `zh-TW` if language not set

**Frontend (React):**
- File: `frontend/src/store/languageStore.ts`
- State management: Zustand store with localStorage persistence
- Component: `LanguageSwitcher` in sidebar for user selection
- API: `/api/language` endpoint for sync with backend

### Audit Checklist for Code Review

Before merging any PR that adds user-facing text:

- [ ] No hardcoded strings in Go code (`telegram.go`, `web.go`, etc.)
- [ ] All messages use `t.getLocalizedMessage(chatID, messageKey, vars)`
- [ ] Message keys added to both `zh-TW.json` and `en.json`
- [ ] Template variables use consistent naming (`{variable}` format)
- [ ] New keys follow naming convention (snake_case, descriptive)
- [ ] No formatting logic in message files (keep in Go code)

### Common Mistakes to Avoid

| Mistake | Problem | Solution |
|---------|---------|----------|
| `fmt.Sprintf("Total: %d", count)` | Hardcoded English | Use message key with template |
| Mixing Chinese/English in one string | Inconsistent UX | Separate into message keys |
| `strings.ReplaceAll(msg, "old", "new")` | Not translatable | Use template variables |
| Adding only English translation | Incomplete support | Add to both language files |
| Dynamic count logic in message file | Logic in data layer | Put logic in Go, pass to template |

## Critical Safety Rules

**These rules protect runtime secrets and shared state:**

1. **NEVER modify `config.json`** — It contains runtime secrets (tokens, API keys). Editing it will break the bot.
2. **NEVER commit or push to git** without explicit user instruction in the current message.
3. **NEVER remove or clear API keys, tokens, or credentials** from any file — this is not a "security fix", it breaks the system.

**Build & restart operations** (`go build`, `pkill alice`, process restart) are allowed when explicitly requested by the user.

## Dependencies

Key dependencies: telegram-bot-api, gorilla/websocket, modernc.org/sqlite, google.golang.org/grpc, google.golang.org/protobuf
