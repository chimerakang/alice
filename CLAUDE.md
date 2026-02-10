# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Alice (claude-tg-agent) is a Go-based Telegram bot that provides Claude AI agent capabilities for code assistance. It uses the Anthropic Messages API with tool use to implement an agent loop — Claude can read/write files, execute shell commands, and search code iteratively until a task is complete.

## Build & Run

```bash
# Run directly
go run .

# Build binary
go build -o claude-tg-agent .

# Docker
docker build -t claude-tg-agent .
docker run -d \
  -e ANTHROPIC_API_KEY="..." \
  -e TELEGRAM_BOT_TOKEN="..." \
  -e ALLOWED_USER_IDS="123456789" \
  -v /path/to/project:/project \
  claude-tg-agent
```

No test files exist yet. No Makefile or task runner — just `go run .` / `go build`.

## Configuration

Config is loaded from `config.json` (see `config.example.json`), overridden by env vars:

| Env Var | Config Key | Required | Default |
|---------|-----------|----------|---------|
| `ANTHROPIC_API_KEY` | `anthropic_key` | Yes | — |
| `TELEGRAM_BOT_TOKEN` | `telegram_token` | Yes | — |
| `CLAUDE_MODEL` | `model` | No | `claude-sonnet-4-20250514` |
| `PROJECT_DIR` | `default_project_dir` | No | `.` |
| `ALLOWED_USER_IDS` | `allowed_user_ids` | No | `[]` (allow all) |

## Architecture

```
Telegram ←→ TelegramBot ←→ Agent (loop) ←→ AnthropicClient
                                ↕
                          ToolExecutor → local filesystem / shell
```

All source is in the root package (`package main`), one file per concern:

- **main.go** — Entry point, `Config` struct, `LoadConfig()` (config.json + env var merge)
- **api.go** — `AnthropicClient`, API request/response types (`Message`, `ContentBlock`, `ToolDef`, `APIResponse`), message constructors
- **agent.go** — `Agent` struct with the core loop (`Run()`). Iterates up to 25 times, calling Claude and executing tools until no more tool calls or `end_turn`. Per-chat conversation `history` management.
- **tools.go** — `ToolExecutor` and `BuildTools()`. Six tools: `file_read`, `file_write`, `bash`, `file_search`, `list_files`, `file_patch`. Safety limits: 100KB file read, 50KB bash output, 30KB search results, dangerous command blocklist.
- **telegram.go** — `TelegramBot` with per-chat agent instances (`map[int64]*Agent`), user whitelist, commands (`/help`, `/project`, `/reset`, `/status`), message splitting for Telegram's 4096-char limit.

## Key Design Decisions

- **Per-chat isolation**: Each Telegram chat ID gets its own `Agent` instance with independent conversation history
- **Tool results go back as user messages**: `ToolResult` messages use `role: "user"` per the Anthropic API spec
- **file_patch requires unique match**: Exactly one occurrence of `old_text` must exist, otherwise it errors — prevents ambiguous edits
- **Agent communicates in Traditional Chinese** (繁體中文) by default via system prompt

## Adding New Tools

1. Add a `ToolDef` entry in `BuildTools()` in tools.go
2. Add a case in `ToolExecutor.Execute()` switch
3. Implement the handler method on `*ToolExecutor`

## Dependencies

Single direct dependency: `github.com/go-telegram-bot-api/telegram-bot-api/v5`
