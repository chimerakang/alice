# Codex CLI Spike — Issue #106

調查日期：2026-04-26  
版本：`@openai/codex@0.125.0`（透過 npx 安裝）

---

## 1. 安裝方式

```bash
# 全域安裝（推薦）
npm install -g @openai/codex

# 或透過 npx 臨時執行
npx @openai/codex exec ...
```

---

## 2. 非互動執行旗標

核心子命令：`codex exec`

| 旗標 | 說明 |
|------|------|
| `--json` | 以 JSONL 格式輸出事件流到 stdout（**Alice 整合必用**） |
| `--ephemeral` | 不持久化 session（不可 resume） |
| `--dangerously-bypass-approvals-and-sandbox` | 跳過所有確認、無沙箱限制（CI/自動化用） |
| `-m, --model <MODEL>` | 指定模型（e.g., `o3`, `o4-mini`） |
| `-C, --cd <DIR>` | 設定 agent 工作目錄 |
| `-c <key=value>` | 覆蓋 config.toml 設定值（TOML 格式） |
| `--skip-git-repo-check` | 允許在非 git 目錄執行 |
| `-s, --sandbox <MODE>` | 沙箱策略：`read-only` / `workspace-write` / `danger-full-access` |
| `-o, --output-last-message <FILE>` | 將最後一條訊息寫到檔案 |
| `-p, --profile <PROFILE>` | 使用 config.toml 中指定的 profile |

**Prompt 輸入**：
- 位置參數：`codex exec "prompt text"`
- stdin：`echo "prompt" | codex exec -`（`-` 代表從 stdin 讀取）
- 兩者並用時，stdin 內容作為 `<stdin>` block 附加到 prompt 後面

---

## 3. 輸出事件 Schema（JSONL）

每行一個 JSON 物件，`type` 欄位區分事件種類。

### 3.1 基本對話（無工具呼叫）

```jsonl
{"type":"thread.started","thread_id":"019dc613-8dad-7ef1-9a60-424e5f483bd6"}
{"type":"turn.started"}
{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"pong"}}
{"type":"turn.completed","usage":{"input_tokens":14012,"cached_input_tokens":6528,"output_tokens":17,"reasoning_output_tokens":10}}
```

### 3.2 工具呼叫（Shell 命令執行）

```jsonl
{"type":"thread.started","thread_id":"019dc613-d184-77f1-be6d-dd3348724ffc"}
{"type":"turn.started"}
{"type":"item.started","item":{"id":"item_0","type":"command_execution","command":"/bin/zsh -lc ls","aggregated_output":"","exit_code":null,"status":"in_progress"}}
{"type":"item.completed","item":{"id":"item_0","type":"command_execution","command":"/bin/zsh -lc ls","aggregated_output":"<stdout>","exit_code":0,"status":"completed"}}
{"type":"item.completed","item":{"id":"item_1","type":"agent_message","text":"Files in `/tmp` are listed above."}}
{"type":"turn.completed","usage":{"input_tokens":29407,"cached_input_tokens":20224,"output_tokens":51,"reasoning_output_tokens":0}}
```

### 3.3 事件類型彙整

| 事件 `type` | 說明 |
|-------------|------|
| `thread.started` | Session 開始，包含 `thread_id`（UUID v7） |
| `turn.started` | 一輪對話開始 |
| `item.started` | 工具呼叫開始（streaming 時出現） |
| `item.completed` | 工具呼叫完成或 agent 回應完成 |
| `turn.completed` | 一輪對話結束，包含 token 使用量 |

### 3.4 Item 類型

| `item.type` | 欄位 | 說明 |
|-------------|------|------|
| `agent_message` | `text` | Agent 的文字回應 |
| `command_execution` | `command`, `aggregated_output`, `exit_code`, `status` | Shell 命令執行結果 |

---

## 4. Claude stream-json vs Codex --json 對映表

| 概念 | Claude stream-json | Codex --json |
|------|-------------------|--------------|
| Session ID | `session_id`（在回應 metadata 中） | `thread_id`（`thread.started` 事件） |
| 文字輸出 | `content_block_delta` + `type:"text_delta"` | `item.completed` + `type:"agent_message"` |
| 工具呼叫 | `content_block_start` + `type:"tool_use"` | `item.started/completed` + `type:"command_execution"` |
| 思考過程 | `content_block_start` + `type:"thinking"` | `reasoning_output_tokens` in usage（無獨立事件） |
| Token 使用量 | `message_delta` + `usage` | `turn.completed` + `usage` |
| Cost | `total_cost_usd` in 最終回應 | **不提供**（需自行計算） |

**關鍵差異**：
- Codex 工具系統完全不同：僅有 `command_execution`（Shell），無 MCP tool_use events
- Codex 不回傳 `total_cost_usd`，需自行建立 pricing table
- Codex 無 thinking block 事件，reasoning tokens 僅在 usage 計數

---

## 5. Session Resume 能力

### 結論：**支援 Session Resume**，但有限制

```bash
# Resume 最近一次 session
codex exec resume --last "繼續任務"

# Resume 指定 thread_id
codex exec resume <THREAD_UUID> "繼續任務"
```

**重要限制**：
1. `--ephemeral` 的 session **無法** resume（錯誤：`no rollout found for thread id`）
2. 不加 `--ephemeral` 的 session 會自動持久化，可透過 `--last` 或 UUID 恢復
3. Thread ID 格式為 UUID v7（含時間戳，可排序）
4. 有一個已知錯誤：`failed to record rollout items: thread X not found`（stderr 輸出），但不影響功能

### Alice 整合策略

由於 Alice 管理對話歷史，建議：
- **短對話**：使用 `--ephemeral`，Alice 自行傳遞完整對話歷史（同 Claude CLIClient 模式）
- **長任務**：不加 `--ephemeral`，從 `thread.started` 取得 `thread_id` 並儲存，後續 `codex exec resume <id>` 延續

---

## 6. 環境變數考量

Codex 有自己的巢狀執行偵測機制（類比 Claude 的 `CLAUDE_CODE_ENTRYPOINT` 等）。  
需要調查的變數（建議在 `cleanEnvForCLI` 類比函數中處理）：
- `CODEX_*` 前綴變數
- `OPENAI_API_KEY`（Codex 使用 OpenAI API，非 Anthropic）
- 可能有 `CODEX_DISABLE_*` 類的偵測變數

---

## 7. Cost 計算

Codex 不直接回傳 cost，`turn.completed` 僅有：
```json
{
  "usage": {
    "input_tokens": 14012,
    "cached_input_tokens": 6528,
    "output_tokens": 17,
    "reasoning_output_tokens": 10
  }
}
```

需要在 Alice 端建立 pricing table（依模型不同），例如：
```go
var codexPricing = map[string]struct{ Input, Output float64 }{
    "o3":          {Input: 10.0 / 1e6, Output: 40.0 / 1e6},
    "o4-mini":     {Input: 1.1 / 1e6, Output: 4.4 / 1e6},
    "gpt-4.1":     {Input: 2.0 / 1e6, Output: 8.0 / 1e6},
    "gpt-4.1-mini":{Input: 0.4 / 1e6, Output: 1.6 / 1e6},
}
```

---

## 8. 整合摘要

**可行性**：✅ 高  
Codex CLI 的 `--json` 模式輸出結構清晰，事件數量少（4-5種），比 Claude stream-json 更簡單。

**主要適配工作**：
1. `item.completed[agent_message]` → `CLIResponse.Content`
2. `item.started/completed[command_execution]` → `ToolUseEvent`（需降級對映，因 Codex 工具系統不同）
3. `turn.completed[usage]` → token counting + cost estimation（需 pricing table）
4. `thread.started[thread_id]` → session ID 儲存（用於 resume）

**風險**：
- Codex 工具系統（僅 Shell）與 Claude MCP 工具系統差異大，Dashboard 工具事件顯示需要對映層或降級
- `failed to record rollout items` 錯誤持續出現在 stderr（可能是 bug，需觀察）
