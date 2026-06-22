---
name: alice-add-tool
description: Alice 專案 tool integration 專用 skill。當任務要新增、修改或除錯 Claude/Codex tool、Hermes tool hints、tool safety hooks、tool execution logging、Dashboard telemetry、或把舊 BuildTools/ToolExecutor 指引改成現行 CLI-backed 架構時觸發；一般業務邏輯或非工具相關改動不適用。
---

# Alice Tool Integration

## 觸發時機

遇到下列任一情境就載入本 skill：

- 新增或修改 Claude / Codex 可見的 tool 能力
- 調整 Hermes planner 的 tool hints、hook guard、或結果驗證
- 修正 tool execution logging、checkpoint、decision log、或 telemetry
- 清理舊文件中 `BuildTools()` / `ToolExecutor` 之類的過時路徑

Alice 目前不是自行註冊一組 `BuildTools()` 給模型呼叫；主要工具能力來自 Claude Code CLI / Codex CLI。應先判斷要改的是哪一層工具整合，再動手。

## Current Architecture

主要路徑：

1. `internal/app/api.go` 的 `CLIClient.CallStream()` 解析 Claude Code CLI streaming events。
2. `internal/app/codex_client.go` 將 Codex shell execution 映射成 synthetic `command_execution` tool event。
3. `internal/app/agent.go` 在 `onToolUse` callback 記錄 tool start/completion、checkpoint 與 decision log。
4. `internal/app/hermes/hooks.go` 提供 Hermes tool pre/post hooks，用來保護敏感路徑與做結果驗證。
5. `internal/app/web.go`、`internal/app/websocket.go`、`internal/app/proto_converters.go` 將 tool telemetry 暴露給 Dashboard/API。

如果看到舊指引提到 `internal/app/tools.go`、`BuildTools()`、`ToolExecutor.Execute()`，先用 `rg` 確認是否仍存在；目前這些不是有效擴充點。

## Change Types

### Add a new Claude/Codex-visible capability

優先確認是否真的需要 repo 內新增 tool。多數情況應透過：

- Claude Code 既有工具與 prompt/hook 約束
- Codex CLI command execution
- Hermes planner 的 `tool_hints`
- MCP/connector 或外部 CLI

若需求是「模型能呼叫新的外部能力」，需先定義 runtime owner：Claude Code CLI、Codex CLI、Hermes hook、或 Alice API endpoint。不要只新增 Dashboard 顯示而沒有 executor 路徑。

### Add or modify Hermes tool hints

相關檔案：

- `internal/app/hermes/planner.go`
- `internal/app/hermes/prompt_builder.go`
- `internal/app/hermes/prompts/planner_rules*.md`
- `internal/app/hermes/prompts/executor_rules*.md`

保持 `tool_hints` 是建議，不是硬性限制。Executor 仍可依實際狀態選擇更合適工具。

### Add safety checks around tools

相關檔案：

- `internal/app/hermes/hooks.go`
- `internal/app/hermes/hooks_test.go`
- `internal/app/checkpoint.go`

敏感路徑規則必須保留：

- 不修改 `config.json`
- 不清除 secrets/tokens/credentials
- 不破壞 `.git` 或環境檔

### Add tool telemetry or dashboard behavior

相關檔案：

- `internal/app/agent.go`
- `internal/app/storage.go`
- `internal/app/unified_task_store.go`
- `internal/app/web.go`
- `internal/app/websocket.go`
- `frontend/src/**`

新增欄位時要檢查 SQLite schema、API JSON、protobuf converters、WebSocket event 與前端型別是否同步。

## Checklist

- [ ] 先用 `rg` 找現有 owner 與資料流，不假設 `BuildTools()` 存在。
- [ ] 明確界定 executor path、logging path、UI/API path。
- [ ] 若改 Hermes hooks，補或更新 `internal/app/hermes/hooks_test.go`。
- [ ] 若改 storage/API，補 migration/schema 與相關 handler 測試。
- [ ] 若新增 user-facing 文字，同步觸發 `alice-i18n` skill。
- [ ] 跑最小相關測試，例如 `go test ./internal/app/...` 或更窄 package。
