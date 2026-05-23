# CLAUDE.md

Guidance for Claude Code in this repo. Keep this file minimal; load detail from linked docs or skills on demand.

## Core

Alice 是 Go 語言的 Telegram bot，將 Claude Code CLI 包裝成 AI coding agent。原生 bot + API process，搭配 Docker-hosted React dashboard 監控 AI 決策、工具執行與專案活動。

Agent 預設以**繁體中文**與使用者溝通（system prompt 設定）。
使用 Claude Code CLI 前先 `claude auth`。

## Build & Run

```bash
go build -o alice ./cmd/alice
nohup ./alice >> alice.log 2>&1 &
docker compose up -d dashboard

cd frontend && npm run build && cp -r dist/* ../web/
docker compose up -d --build dashboard
```

`make help` 列出所有 Makefile targets。

## Ports

- `8082`: Alice Bot (native Go)；`web_port` in `config.json` 必須維持 8082，nginx.conf 也硬編碼 proxy 到 `host.docker.internal:8082`
- `3939`: Dashboard (Docker nginx)；使用者存取 `http://localhost:3939`

## Response Markers

Alice 的 bot layer 會解析以下 marker，並在回應中自動觸發對應動作：

| Marker | 格式 | 說明 |
|--------|------|------|
| `[SEND_FILE:path]` | 相對路徑 | 發送工作目錄內的圖片/影片/文件給使用者 |
| `[GENERATE_IMAGE:prompt\|size\|quality]` | size/quality 可省略 | 呼叫 OpenAI Image API 生成圖片並發送，預設 1024x1024, auto |

範例：`[GENERATE_IMAGE:火龍，水彩風格，奇幻場景]`

## Safety

1. **NEVER modify `config.json`**，它含 runtime secrets（tokens、API keys）。
2. **NEVER commit or push to git** without explicit user instruction in the current message.
3. **NEVER remove or clear API keys, tokens, or credentials**。

Build / restart 操作（`go build`、`pkill alice`、process restart）只有在使用者明確要求時才執行。

## Roadmap

- user-facing 文字、翻譯、模板變數：載入 [`alice-i18n`](.claude/skills/alice-i18n/SKILL.md) 與 [`docs/i18n_guide.md`](docs/i18n_guide.md)
- 新增或修改 Claude / Codex tool integration：載入 [`alice-add-tool`](.claude/skills/alice-add-tool/SKILL.md)
- Dynamic Model Routing：讀 [`docs/arch/model-routing.md`](docs/arch/model-routing.md)
- Deployment Architecture、Startup Sequence、Project Structure：讀 [`docs/arch/deployment.md`](docs/arch/deployment.md) 與 [`docs/DEPLOYMENT.md`](docs/DEPLOYMENT.md)
- Task & Issue Management、`MASTER_TASKS.md` 生成：讀 [`docs/playbooks/tasks.md`](docs/playbooks/tasks.md)
- Subtask 拆分框架與驗收標準：讀 [`docs/SUBTASK_FRAMEWORK.md`](docs/SUBTASK_FRAMEWORK.md) 與 [`docs/SUBTASK_DECISION_CARD.md`](docs/SUBTASK_DECISION_CARD.md)
- Hermes prompt 規則與工具限制：讀 [`internal/app/hermes/prompts/planner_rules_codex.md`](internal/app/hermes/prompts/planner_rules_codex.md) 與 [`internal/app/hermes/prompts/executor_rules_codex.md`](internal/app/hermes/prompts/executor_rules_codex.md)
- Dashboard、WebSocket、Storage、Git integration：讀 [`docs/DASHBOARD.md`](docs/DASHBOARD.md)、[`docs/WEBSOCKET_REALTIME.md`](docs/WEBSOCKET_REALTIME.md)、[`docs/STORAGE_PERSISTENCE.md`](docs/STORAGE_PERSISTENCE.md)、[`docs/GIT_INTEGRATION.md`](docs/GIT_INTEGRATION.md)
