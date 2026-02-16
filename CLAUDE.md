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

### Expected Cost Savings

- ~40-50% reduction in token costs
- Simple tasks routed to Haiku (7-10x cheaper than Opus)
- Complex tasks routed to Opus for quality

## Adding New Tools

1. Add a `ToolDef` entry in `BuildTools()` in `internal/app/tools.go`
2. Add a case in `ToolExecutor.Execute()` switch
3. Implement the handler method on `*ToolExecutor`

## Critical Safety Rules

**These rules protect runtime secrets and shared state:**

1. **NEVER modify `config.json`** — It contains runtime secrets (tokens, API keys). Editing it will break the bot.
2. **NEVER commit or push to git** without explicit user instruction in the current message.
3. **NEVER remove or clear API keys, tokens, or credentials** from any file — this is not a "security fix", it breaks the system.

**Build & restart operations** (`go build`, `pkill alice`, process restart) are allowed when explicitly requested by the user.

## Dependencies

Key dependencies: telegram-bot-api, gorilla/websocket, modernc.org/sqlite, google.golang.org/grpc, google.golang.org/protobuf
