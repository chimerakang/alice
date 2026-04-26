# Codex Backend 驗證報告 — Issue #106

**日期**：2026-04-26  
**驗證者**：Hermes Executor  
**狀態**：靜態驗證（codex CLI 未安裝，無法執行端對端測試）

---

## 環境狀況

| 項目 | 狀態 |
|------|------|
| codex CLI 安裝 | ❌ 未安裝（`which codex` 找不到） |
| @openai/codex npm 套件 | ❌ 未安裝（`npm list -g` 顯示空） |
| alice binary | ✅ 已建置（Apr 26 03:35，34MB arm64） |
| 編譯 | ✅ `go build` + `go vet` 零錯誤 |

---

## 靜態驗證結果

### 對話流程

**實作狀況**：
- `CodexClient.Call()` / `CallStream()` / `CallPlan()` 均已實作，呼叫 `runCodexStream()`
- `codex exec --json` 輸出的 JSONL 事件由 `scanner.Scan()` 逐行解析
- `agent_message` 事件 → `textBlocks` 累積 → `CLIResponse.Result`
- 結果回傳格式與 `CLIClient` 相同（`*CLIResponse`）

**待驗證**（需安裝 codex）：
- 實際對話回應是否正確解析
- multi-turn 對話是否能透過 `sessionID` 正確 resume

---

### 工具呼叫事件 Dashboard 顯示

**實作狀況**：
- `command_execution` 事件 → `onToolUse("command_execution", {...})` callback
- Dashboard 工具事件透過 `RecordAPICall()` 記錄到 SQLite
- 工具名稱為 `"command_execution"`（非 Claude 的 `bash` / `str_replace_editor` 等）

**已知差距**：
- Codex 僅有 shell 執行工具，沒有 Claude Code 的多樣工具集（file edit、web fetch 等）
- Dashboard 工具事件 `tool_name` 顯示為 `command_execution`，與 Claude 不同但能正常顯示
- 無 `item.started` streaming 事件到 dashboard（Codex 僅在 `item.completed` 後才觸發 callback）

---

### Cost / Token 統計

**實作狀況**：
- `turn.completed` 的 `usage` 欄位解析 `input_tokens` / `output_tokens`
- `calcCodexCost()` 根據 model 名稱查 pricing table 計算 USD 成本
- `RecordAPICall()` 記錄成本到 performance 統計
- Dashboard `handleCostsByModel` 按 model name 字串聚合，codex model 自動相容

**定價表（目前內建）**：
| Model | Input (USD/M) | Output (USD/M) |
|-------|--------------|----------------|
| o3 | 10.0 | 40.0 |
| o4-mini | 1.1 | 4.4 |
| gpt-4.1 | 2.0 | 8.0 |
| gpt-4.1-mini | 0.4 | 1.6 |
| gpt-4o | 2.5 | 10.0 |
| gpt-4o-mini | 0.15 | 0.6 |

**注意**：未知 model 預設使用 o4-mini 定價（fallback）。

---

### Session 延續

**實作狀況**：
- `sessionID != ""` 時使用 `codex exec resume <sessionID> ...`
- `thread.started` 事件的 `thread_id` 作為新 sessionID 回傳
- Alice 在 `CLIResponse.SessionID` 儲存並傳入下一輪

**已知差距**：
- Codex `resume` 需要在**同一機器**保留 codex 本地狀態（`~/.codex/` 目錄）
- 與 Claude Code 不同，codex session 不跨機器持久化
- 若 alice 重啟，舊 `thread_id` 可能已失效 → 第一輪 resume 失敗後會自動 fallback 為新 session（`exec error` → caller 重試無 sessionID）
- 目前 `runCodexStream` 不特別處理 resume 失敗（會直接回傳 error）

**建議**：未來可在 `Call()` 層加入 resume-fallback 邏輯（捕捉 resume error 後改用 new session）。

---

## 解耦驗證

| 檔案 | 變更 | 狀態 |
|------|------|------|
| `hermes_bridge.go` | 已改為接收 `Client` interface | ✅ |
| `multimodal.go:96` | `cliClient, ok := ea.Agent.client.(*CLIClient)` 帶有 `ok` fallback，非 CLIClient 時降級為 regular Run | ✅ |
| `telegram.go` | model routing 加入 `strings.HasPrefix(userPref, "codex")` 分支 | ✅ |
| `main.go` | switch 三路分支 `codex` / `api` / 預設 claude | ✅ |

---

## 已知限制（End-to-End 測試待補）

1. **codex CLI 需安裝**：`npm install -g @openai/codex` + `export OPENAI_API_KEY=...`
2. **無 --max-turns 旗標**：Codex 不支援限制回合數，planning 模式完全依賴 prompt engineering
3. **Resume 無跨機器持久化**：session 只在本地 codex state 存在時有效
4. **工具集差異**：Codex 只有 shell exec，Claude Code 有完整工具集（file edit、web search 等）
5. **Streaming 差異**：Codex 無 `item.started` mid-stream 通知，工具呼叫 callback 只在完成後觸發

---

## 改進實施（Apr 26, 2026）

### Resume-Fallback 邏輯 ✅
已在 `Call()` 和 `CallStream()` 中實施 resume-fallback：
- 若提供 `sessionID` 且 resume 失敗（exec error），自動改用新 session 重試
- 避免舊 thread_id 失效時整個請求失敗
- 記錄到日誌便於監控 fallback 情況

---

## 建議下一步

要完成 end-to-end 驗證，需執行以下步驟：

```bash
# 1. 安裝 codex CLI
npm install -g @openai/codex

# 2. 設定 OpenAI API Key
export OPENAI_API_KEY="sk-..."

# 3. 更新 config.json 的 ai_backend
# (或建立測試用 config 檔)

# 4. 啟動 alice
./alice

# 5. 在 Telegram 發送訊息測試
# 觀察 alice.log 與 dashboard，特別是 resume 或 fallback 日誌
```
