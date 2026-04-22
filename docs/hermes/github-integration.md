# Hermes GitHub Issue 整合

本文件說明 Hermes 與 GitHub Issue 工作流的整合機制。

## 指令

```
/hermes #101         — 從 Issue #101 讀取目標並立即啟動 Hermes
/hermes              — 啟動 Hermes 模式（等待下一則訊息作為目標）
/hermes status       — 查看目前任務狀態
/hermes stop         — 停止目前任務並退出 Hermes 模式
```

## /hermes #N 行為

1. 呼叫 `gh issue view N --json title,body,labels` 取得 Issue 資料
2. 解析 Issue body 中的 `- [ ]` checklist 項目
3. 若有未勾選 checklist → Planner 優先用 checklist 作為 SubTask 雛形
4. 若無 checklist → Planner 自由拆解，完成後寫回 checklist 到 Issue body
5. 根據 `complexity:*` label 套用 TokenBudget 上限（若有設定）
6. 啟動 Coordinator，Issue 編號繫結到 TaskState

## 生命週期事件 → GitHub 動作

| 事件 | 動作 |
|------|------|
| Planner 完成規劃 | Post "Hermes 開始執行" comment（若 `comment_on_events` 含 `"start"`）|
| Planner 完成規劃 | 將計劃寫回 Issue checklist（若無原有 checklist，且 `sync_checklist: true`）|
| 每個 SubTask 完成 | 同步更新 Issue checklist `- [ ]` → `- [x]`（若 `sync_checklist: true`）|
| Budget 耗盡 | Post budget 警告 comment（若 `comment_on_events` 含 `"budget_exceeded"`）|
| 全部完成 | Post 完成 comment（含 token 用量、artifacts、耗時）|
| 全部完成 + `hermes-auto-close` label | `gh issue close N` 自動關閉 |
| 任務失敗 | Post 失敗 comment + 加上 `hermes-failed` label |

## 設定範例

```json
{
  "hermes": {
    "enabled": true,
    "github_integration": {
      "enabled": true,
      "comment_on_events": ["start", "complete", "fail", "budget_exceeded"],
      "sync_checklist": true,
      "auto_close_on_label": "hermes-auto-close",
      "mark_failure_label": "hermes-failed",
      "trigger_task_sync_on_complete": true,
      "complexity_budget_map": {
        "complexity:small":  { "max_total_tokens": 100000,  "max_wallclock_seconds": 300 },
        "complexity:medium": { "max_total_tokens": 500000,  "max_wallclock_seconds": 600 },
        "complexity:large":  { "max_total_tokens": 1000000, "max_wallclock_seconds": 1800 }
      }
    }
  }
}
```

## Checklist 同步邏輯

`SyncChecklist` 使用描述文字比對（大小寫不敏感）：
- 完全匹配 SubTask.Description == checklist item text（優先）
- 前綴匹配（前 16 字元）作為 fallback

若 Planner 從 Issue checklist 生成 SubTask，兩者描述通常完全一致，確保 1:1 對齊。

## task-sync 自動觸發

`trigger_task_sync_on_complete: true` 時，Hermes 完成後（成功或失敗）會以 goroutine 執行
`claude --print --dangerously-skip-permissions /task-sync`，更新 `docs/MASTER_TASKS.md`。

觸發邏輯透過 `CoordinatorConfig.PostCompletionHook` callback 注入，由 `telegram.go` 負責
建立（包含正確的環境變數清理，避免 Claude Code 嵌套執行偵測）。

## 認證

使用系統已安裝的 `gh` CLI（`gh auth login` 認證），無需額外 PAT。
與 `/task-sync`、`/task-status` 等 skills 共用相同的認證狀態。

## 實作位置

| 檔案 | 職責 |
|------|------|
| `internal/app/hermes/github.go` | FetchIssue, SyncChecklist, PostComment, CloseIssue 等 |
| `internal/app/hermes/github_config.go` | GithubCfg struct（coordinator 內部使用）|
| `internal/app/main.go` | GithubIntegrationConfig JSON 設定 |
| `internal/app/hermes/coordinator.go` | 生命週期事件觸發 GitHub 動作 |
| `internal/app/telegram.go` | /hermes #N 指令解析 + startHermesFromIssue |
| `internal/app/hermes/state.go` | TaskState.GithubIssueNumber 欄位 |
