# CLAUDE.md

Guidance for Claude Code working in this repo. Keep this file minimal — load detail from linked docs on demand.

## Project Overview

Alice 是 Go 語言的 Telegram bot，將 Claude Code CLI 包裝成 AI coding agent。原生 bot + API process，搭配 Docker-hosted React dashboard 監控 AI 決策、工具執行與專案活動。

Agent 預設以**繁體中文**與使用者溝通（system prompt 設定）。

## Build & Run

```bash
go build -o alice ./cmd/alice           # Build bot
nohup ./alice >> alice.log 2>&1 &       # Start bot (native, background)
docker compose up -d dashboard          # Start dashboard

# Rebuild dashboard after frontend changes
cd frontend && npm run build && cp -r dist/* ../web/
docker compose up -d --build dashboard
```

`make help` 列出所有 Makefile targets。

## Port Allocation (DO NOT CHANGE)

| Port | Service | Notes |
|------|---------|-------|
| `8082` | Alice Bot (native Go) | REST + WebSocket — `web_port` in config.json **MUST be 8082**；nginx.conf 硬編碼 proxy 至 `host.docker.internal:8082` |
| `3939` | Dashboard (Docker nginx) | 使用者存取 `http://localhost:3939` |

## Critical Safety Rules

保護 runtime secrets 與共用狀態：

1. **NEVER modify `config.json`** — 含 runtime secrets (tokens、API keys)。編輯會破壞 bot。
2. **NEVER commit or push to git** without explicit user instruction in the current message.
3. **NEVER remove or clear API keys, tokens, or credentials** — 這不是 "security fix"，會破壞系統。

Build & restart 操作（`go build`、`pkill alice`、process restart）在使用者明確要求時允許執行。

## Key Design Decisions

- **Per-chat isolation**: 每個 Telegram chat ID 有獨立 `Agent` instance 與對話歷史
- **Tool results 回 user role**: `ToolResult` messages 使用 `role: "user"`（符合 Anthropic API 規範）
- **file_patch 需唯一匹配**: `old_text` 必須在檔案中剛好出現一次，否則報錯（防止歧義編輯）

## Project Structure

```
cmd/alice/main.go        Entry point (calls app.Main())
internal/app/            All application code (package app)
  telegram.go            TelegramBot, command handling, per-chat agents
  agent.go               Agent core loop, tool/decision logging
  api.go                 CLIClient, Claude Code CLI integration
  web.go / websocket.go  REST API + WebSocket hub
  storage.go             SQLiteStorage persistence
  tools.go               ToolExecutor, BuildTools()
  i18n_manager.go        I18nManager
  multiagent.go          AgentCoordinator
  security.go / checkpoint.go / performance.go / git_integration.go
gen/ proto/              Generated protobuf/gRPC + proto definitions
web/                     Pre-built React SPA (vite output)
frontend/                React source (Vite + TS + Tailwind)
docs/                    Documentation
```

## Configuration

Alice 以 subprocess 呼叫 Claude Code CLI，不直接使用 Anthropic API。需先 `claude auth` 完成認證。

Config 從 `config.json` 載入（範本見 `config.example.json`），可用 env vars 覆蓋。主要變數：`TELEGRAM_BOT_TOKEN`（必填）、`CLAUDE_MODEL`、`PROJECT_DIR`、`ALLOWED_USER_IDS`、`ALICE_ENABLE_PERSISTENCE`、`ALICE_DATABASE_PATH`、`OPENAI_API_KEY`（多媒體功能用）。完整清單見 [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md)。

## Adding New Tools

1. 在 `BuildTools()` ([internal/app/tools.go](internal/app/tools.go)) 加 `ToolDef` entry
2. 在 `ToolExecutor.Execute()` switch 加 case
3. 在 `*ToolExecutor` 實作 handler method

## 延伸閱讀（按需載入）

| 主題 | 文件 |
|------|------|
| **Subtask 拆分框架** (Triage、驗收標準、品質檢查) | [docs/SUBTASK_FRAMEWORK.md](docs/SUBTASK_FRAMEWORK.md) + [docs/SUBTASK_DECISION_CARD.md](docs/SUBTASK_DECISION_CARD.md) |
| Dynamic Model Routing（sticky session、triage、commands、Web API） | [docs/DYNAMIC_MODEL_ROUTING.md](docs/DYNAMIC_MODEL_ROUTING.md) |
| Deployment architecture、startup sequence、監控 stack | [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md) |
| Hermes GPT tier / Codex backend（`ai_backend: "multi"`、`/ghermes`、已知限制） | [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md) |
| Hermes prompt rules（Codex planner / executor JSON 與工具限制） | [internal/app/hermes/prompts/planner_rules_codex.md](internal/app/hermes/prompts/planner_rules_codex.md)、[internal/app/hermes/prompts/executor_rules_codex.md](internal/app/hermes/prompts/executor_rules_codex.md) |
| i18n 多國語系規範（新增 user-facing 文字時觸發） | skill `alice-i18n` + [docs/i18n_guide.md](docs/i18n_guide.md) |
| Task & Issue Management（milestone、MASTER_TASKS.md） | skills `/task-sync`、`/tasks`、`/task-add`、`/task-status` |
| Dashboard、WebSocket、Storage、Git integration | [docs/DASHBOARD.md](docs/DASHBOARD.md)、[docs/WEBSOCKET_REALTIME.md](docs/WEBSOCKET_REALTIME.md)、[docs/STORAGE_PERSISTENCE.md](docs/STORAGE_PERSISTENCE.md)、[docs/GIT_INTEGRATION.md](docs/GIT_INTEGRATION.md) |

## Dependencies

telegram-bot-api · gorilla/websocket · modernc.org/sqlite · google.golang.org/grpc · google.golang.org/protobuf
