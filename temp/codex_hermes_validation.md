# Codex Hermes `/ghermes` 端對端驗證紀錄

日期：2026-04-26  
範圍：Issue #107 子任務 1（E2E 設定、`/ghermes #107` 實跑、planner / executor / cost / GitHub 行為觀察）

## 結論

- 執行環境已符合 GPT tier 前提：`config.json` 為 `ai_backend: "multi"`，`hermes.enabled = true`，且 OpenAI key 可由 `multimedia.openai_api_key` 提供；本機 `codex` CLI 亦已安裝。
- `/ghermes #107` 已在真實 Telegram workflow 觸發並建立 Hermes task，不是僅靜態推論。
- planner 已成功在 Codex tier 產出符合 schema 的 JSON plan 並落庫，但同日也出現過 planner JSON parse failure，依目前樣本看穩定性仍不足。
- executor 已實際啟動 Codex 子程序，但截至本次觀察尚未產生任何完成的 `command_execution` 事件，因此 cost 分流與 checklist sync 仍未完成驗證。

## 執行前提驗證

### Runtime config

以非敏感方式檢查 `config.json` 與環境後，確認：

- `ai_backend = "multi"`
- `hermes.enabled = true`
- `hermes.github_integration.enabled = true`
- `hermes.github_integration.sync_checklist = true`
- `hermes.github_integration.comment_on_events = ["start", "complete", "fail", "budget_exceeded"]`
- `OPENAI_API_KEY` 環境變數存在
- `multimedia.openai_api_key` 亦存在
- `telegram_token` 與 `anthropic_key` 在現行設定中存在
- `codex` CLI 路徑存在：`/opt/homebrew/bin/codex`
- `gh auth status` 顯示已登入 `github.com`，帳號為 `chimerakang`

### 啟動日誌

`alice.log` 顯示 2026-04-26 14:54:59 與 15:03:19 啟動時皆載入：

- `[client-routing] Config ai_backend=multi - using MultiBackendClient dispatcher`
- `MultiBackendClient: codex backend enabled (default model: gpt-5.4, key from multimedia.openai_api_key)`

這代表 GPT/Codex tier 在 runtime 已啟用，且 key 來源優先序實際走到 config 內的 `multimedia.openai_api_key`。

## `/ghermes #107` 真實流程觀察

### Telegram / Hermes workflow 證據

`alice.log` 在 2026-04-26 15:03:33~15:03:36 有以下事件：

- 收到 Telegram 訊息：`/ghermes #107`
- `handleMessage` / `handleCommand` / `processing command: /ghermes`
- Hermes task 啟動：`chat -1003899455107 started task 59958dc7-7ecc-4881-af15-fd979c01ca51`

因此可以確認 `/ghermes #107` 不是透過手動插資料或單元測試模擬，而是經過專案既有 Telegram 指令入口。

### GitHub Issue comment 證據

`gh issue view 107 --json comments` 顯示：

- 2026-04-26 06:31:23Z：失敗 comment，原因為 `codex` 不在 PATH
- 2026-04-26 06:36:01Z：失敗 comment，原因為 planner JSON parse failed after retries
- 2026-04-26 07:04:02Z：開始 comment，內容為
  - Planner: `gpt-5.5`
  - Executor: `gpt-5.4-mini`

這證明 GitHub comment 整合至少在 `fail` 與 `start` 事件上已實際生效。

## Planner 驗證

### 成功案例

SQLite `hermes_task_states` 中，task `59958dc7-7ecc-4881-af15-fd979c01ca51` 內容顯示：

- `status = executing`
- `planner_session = 019dc899-d85f-7183-bf1b-3b9a3e177453`
- `plan_json` 為 7 個 subtasks 的 JSON array
- 第一個 subtask 已進入 `in_progress`
- `model_usages` 已記錄 planner 使用 `gpt-5.5`、`input_tokens = 16877`、`output_tokens = 614`

這表示：

- Codex planner 能輸出可被 Hermes `parsePlannerJSON` 接受的 schema
- Planner session ID 已成功落庫
- Plan 已成功從 planning phase 進入 execution phase

### 失敗案例

同一個 Issue #107 在當天稍早已有一次真實失敗 comment：

- `planner JSON parse failed after retries`

另有一筆 task 狀態：

- `ad574577-40ee-45e0-85b2-e9289ead34ef`
- `created_at = 2026-04-26T14:35:16+08:00`
- `status = failed`

### 目前判斷

以今天可見樣本來看：

- 成功：至少 1 次
- 失敗：至少 1 次（JSON parse）

因此目前不能宣稱 Codex planner 已「穩定」產出 schema JSON；就可見樣本來看，失敗率高於 10%。

## Executor 驗證

### 已驗證到的部分

目前 `alice` 主程序 PID `56953` 仍在執行，且其子程序存在：

- `node /opt/homebrew/bin/codex exec --json ... -m gpt-5.4 ...`

這個子程序的 prompt 內容就是 Issue #107 的第一個驗證 subtask，因此可確認：

- Hermes 已從 planner phase 實際切到 executor phase
- 該 subtask 因 `tool_hints` 含 `Edit`，被 heavy executor 路由到 `gpt-5.4`
- GitHub start comment 顯示的 Executor `gpt-5.4-mini` 只是預設 executor model，並未反映 heavy executor 實際模型

### 尚未驗證到的部分

截至本次觀察：

- `tool_executions` table 仍為 `0` 筆
- task `59958dc7-7ecc-4881-af15-fd979c01ca51` 的 `updated_at` 停在 `2026-04-26T15:04:04+08:00`
- `accumulated` 仍為空字串
- GitHub 上沒有新的 subtask progress comment

因此本次只能確認 executor 已啟動，但還不能證明：

- `command_execution` 完成事件有被 coordinator 正確消化
- shell 工具輸出有寫回 task state
- executor 結果摘要已成功回傳

## Cost / Backend 分流驗證

### 已觀察到的部分

- `hermes_task_states.model_usages` 已有 planner 的 `gpt-5.5` token usage
- 這代表 Hermes task 層級已開始按模型累加 usage，而非全部視為 Claude

### 尚未完成驗證

由於 executor 尚未完成，且本次沒有任何 `tool_executions` / 新的 `decision_logs` 對應到 Codex Hermes 子任務完成事件，因此目前無法完整證明：

- dashboard 的成本統計已把 GPT 成本與 Claude 成本正確分帳
- executor 階段的 Codex 成本已進入 `decision_logs` / performance aggregation

結論：`cost backend split` 目前僅完成 planner usage 觀察，尚未完成 end-to-end 驗證。

## GitHub checklist / comment sync 驗證

### 已驗證

- `fail` comment：可用
- `start` comment：可用

### 尚未驗證

- `complete` comment：尚未出現
- checklist sync：Issue body 仍維持原始 unchecked 清單，尚未觀察到任何 `- [x]`

### 額外發現

雖然本次 `/ghermes #107` 的 GitHub start comment 已成功發出，但 SQLite 中對應 task 的 `github_issue_number` 仍為 `0`。  
`hermes_task_states` schema 支援此欄位，問題比較像是 `TaskState` 建立時未把 `CoordinatorConfig.GithubIssueNumber` 寫入 store。

這代表：

- GitHub lifecycle comment 依然可能正常，因為 coordinator runtime config 仍持有 issue number
- 但持久化 state 與查詢層會失去 Issue 綁定，後續統計/恢復/除錯都會不完整

## 已知差異與風險

1. Codex planner 今日已有真實 JSON parse failure，穩定性不足，需考慮 codex-specific prompt rules。
2. 實際執行中的 heavy executor 模型是 `gpt-5.4`，但 GitHub start comment 顯示的是預設 executor `gpt-5.4-mini`，comment 與實際模型不一致。
3. `github_issue_number` 未持久化到 task store，會讓資料庫中的 Hermes task 與 GitHub issue 關聯斷裂。
4. executor 已啟動但長時間未產生完成事件，`command_execution` callback 仍未被本次實跑證明。
5. cost 分流目前只有 planner 模型 usage 證據，尚不足以證明 dashboard 端分帳完全正確。

## 本次子任務判定

### 已完成

- 驗證 runtime config 為 `ai_backend: "multi"`
- 驗證 OpenAI key 可用且未暴露 secret
- 驗證 `/ghermes #107` 曾經且仍正在透過真實 Telegram/Hermes workflow 執行
- 驗證 planner 在 Codex tier 至少成功一次並產出合法 JSON plan
- 驗證 GitHub `start` / `fail` comments 在 GPT tier 可用

### 未完成或待後續子任務補證

- executor `command_execution` 完成事件
- cost 統計 backend 分流的完整閉環
- checklist sync 與 `complete` comment

