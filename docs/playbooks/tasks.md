# Task And Issue Management

GitHub Issues 是專案工作的唯一真相來源。`docs/MASTER_TASKS.md` 是從 GitHub 狀態生成的快照，視為唯讀輸出。

## 使用時機

- 使用者問 `tasks`、`progress`、`todo`、`next step`、`任務`、`進度`、`待辦`。
- 使用者執行 `/tasks` 或 `/tasks <milestone>`。
- 使用者要新增任務、更新狀態，或同步 `MASTER_TASKS.md`。

## 工作模型

```text
GitHub Milestones = phases
GitHub Issues     = tasks
Issue checklists  = subtasks
Labels            = priority/status refinements
docs/MASTER_TASKS.md = generated snapshot
```

常用標籤：

- `P0`, `P1`, `P2`：優先級。
- `planning`：已範疇化但尚未開始。
- `testing`：需要驗證或測試。
- `paused`：阻塞或暫停。

## 讀寫規則

1. 需要即時狀態時，直接用 `gh` 查 GitHub。
2. 不要把 `docs/MASTER_TASKS.md` 當成 open / closed 狀態的權威來源。
3. 若要變更任務狀態，先更新 GitHub，再考慮同步快照。
4. 只有在使用者要求 sync，或明確工作流程需要時，才重生 `docs/MASTER_TASKS.md`。
5. 生成檔不要自行提交，除非使用者明確要求。

## `/tasks` 互動流程

先用 `gh repo view --json nameWithOwner -q .nameWithOwner` 自動辨識 repo，再查 milestones 與 issues。

### 沒有參數

- 讀取所有 milestones。
- 以 `closed / (open + closed) * 100` 算進度。
- 顯示 phase 概覽、描述、進度與狀態。

### 指定 phase

- 以 milestone 名稱或前綴過濾，例如 `P13`、`P8.5`。
- 讀取該 milestone 的 issues。
- 顯示每個 issue 的狀態、標籤與更新時間。
- 解析 issue body 內的 checklist (`- [x]` / `- [ ]`) 作為 subtasks。
- 若存在高優先級標籤，優先提示。

## `/task-sync` 生成流程

### 前置條件

- 需要已安裝且登入的 `gh` CLI。
- Repo 已建立 GitHub Milestones。

### 生成步驟

1. `gh repo view --json nameWithOwner -q .nameWithOwner`
2. 讀取所有 milestones。
3. 對每個 milestone 讀取所有 issues。
4. 解析 issue body 內的 checklist。
5. 依 issue state 與 labels 轉成狀態標記。
6. 寫入 `docs/MASTER_TASKS.md`，或在 `--dry-run` 時只輸出預覽。

### 狀態對應

| 條件 | Emoji | 說明 |
|------|-------|------|
| Issue closed | ✅ | 已完成 |
| Label 含 `testing` | 🧪 | 測試中 |
| Label 含 `paused` | ⏸️ | 暫停 |
| Label 含 `planning` | 📋 | 規劃中 |
| Issue open | 🔄 | 開發中 |

Phase 狀態：

- 全部 issues closed → `✅`
- 有部分 issues closed → `🔄`
- 尚無 closed issues → `📋`

### 任務 ID 規則

- milestone `P1 - Core Backend` → phase prefix `P1`，task IDs 依序為 `1.1`、`1.2`。
- milestone `P8.5 - TG Enhancement` → task IDs 依序為 `8.5.1`、`8.5.2`。
- milestone `P13 - Future` → task IDs 依序為 `13.1`、`13.2`。

### `MASTER_TASKS.md` 格式契約

```markdown
# {Project Name} - Master Tasks

> {Project description}
> Last updated: {current date}
> Auto-generated from GitHub Issues — do not edit manually.
> Run `/task-sync` to regenerate.

## Status Legend

| Status | Label |
|--------|-------|
| 📋 | 規劃中 |
| 🔄 | 開發中 |
| 🧪 | 測試中 |
| ✅ | 已完成 |
| ⏸️ | 暫停 |
| ❌ | 已取消 |

## Phase Overview

| Phase | Description | Progress | Status |
|-------|-------------|----------|--------|
| {milestone.title} | {milestone.description} | {progress}% | {status_emoji} |

---

## {milestone.title} ({status_emoji} {progress}%)

| # | Task | Issue | Status |
|---|------|-------|--------|
| {phase_num}.{seq} | **{issue.title}** | [#{number}]({html_url}) | {emoji} |
| | — {sub_task_text} | | {sub_emoji} |

---

## Issue Tracker

| Issue | Title | Phase | Status |
|-------|-------|-------|--------|
| [#{number}]({url}) | {title} | {milestone} | {state_emoji} |
```

### 輸出要求

- 非 dry-run 時，將結果寫入 `docs/MASTER_TASKS.md`。
- 回報 milestone 數、issue 總數、open / closed 數。
- 提醒使用者可用 `git add docs/MASTER_TASKS.md && git commit -m "📋 Sync MASTER_TASKS.md from GitHub Issues"`。
- 保持輸出穩定，讓 diff 容易審查。

## 關聯指令

- `/tasks`：顯示進度與 phase 詳情。
- `/task-add`：建立 issue 並指派 milestone。
- `/task-status`：更新 issue 狀態、標籤或 milestone。
- `/task-sync`：從 GitHub Issues 生成 `docs/MASTER_TASKS.md`。
