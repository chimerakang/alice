# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Alice (claude-tg-agent) is a Go-based Telegram bot that provides Claude AI agent capabilities for code assistance. It uses the Anthropic Messages API with tool use to implement an agent loop — Claude can read/write files, execute shell commands, and search code iteratively until a task is complete.

## Build & Run

```bash
# Run directly
go run ./cmd/alice

# Build binary (via Makefile)
make go-build

# Build binary (manual)
go build -o alice ./cmd/alice

# Docker
docker build -t alice .
docker run -d \
  -e TELEGRAM_BOT_TOKEN="..." \
  -e ALLOWED_USER_IDS="123456789" \
  -v /path/to/project:/project \
  alice
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

## Architecture

```
Telegram ←→ TelegramBot ←→ Agent (loop) ←→ AnthropicClient
                                ↕
                          ToolExecutor → local filesystem / shell
```

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
web/                      — Static web assets (HTML, CSS, JS)
docs/                     — Documentation
```

## Key Design Decisions

- **Per-chat isolation**: Each Telegram chat ID gets its own `Agent` instance with independent conversation history
- **Tool results go back as user messages**: `ToolResult` messages use `role: "user"` per the Anthropic API spec
- **file_patch requires unique match**: Exactly one occurrence of `old_text` must exist, otherwise it errors — prevents ambiguous edits
- **Agent communicates in Traditional Chinese** (繁體中文) by default via system prompt

## Adding New Tools

1. Add a `ToolDef` entry in `BuildTools()` in `internal/app/tools.go`
2. Add a case in `ToolExecutor.Execute()` switch
3. Implement the handler method on `*ToolExecutor`

## Dependencies

Key dependencies: telegram-bot-api, gorilla/websocket, modernc.org/sqlite, google.golang.org/grpc, google.golang.org/protobuf
