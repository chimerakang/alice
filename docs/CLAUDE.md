<claude-mem-context>
# Recent Activity

### Feb 12, 2026

| ID | Time | T | Title | Read |
|----|------|---|-------|------|
| #1058 | 11:49 PM | ✅ | Complete P9 Phase Details and Issue Tracker Added to Master Tasks | ~706 |
| #1057 | " | ✅ | Added P9 Multimedia Input Phase to Master Tasks Documentation | ~770 |
| #1056 | " | ✅ | Phase 9 Multimedia Input Added to Master Tasks Roadmap | ~503 |
| #1053 | 11:48 PM | 🔵 | Master Tasks Documentation Review - Comprehensive Project Status | ~816 |

### Feb 13, 2026

| ID | Time | T | Title | Read |
|----|------|---|-------|------|
| #1776 | 8:17 PM | 🔵 | Alice AI Agent Project: Comprehensive AI Development Audit System | ~411 |
| #1347 | 3:47 PM | 🔵 | Issue #35: Image+Caption Processing Failure Root Cause Identified | ~489 |
| #1342 | 3:30 PM | 🔵 | Alice Configuration Uses ANTHROPIC_API_KEY Not Session Token | ~306 |
| #1092 | 12:09 AM | 🟣 | Parser Successfully Processes Real MASTER_TASKS.md File | ~494 |

### Feb 20, 2026

| ID | Time | T | Title | Read |
|----|------|---|-------|------|
| #4269 | 2:14 PM | 🔵 | Alice 專案企業級系統研究文檔完成 | ~258 |

### Apr 26, 2026

| ID | Time | T | Title | Read |
|----|------|---|-------|------|
| #10512 | 4:43 PM | 🔴 | Hermes 模式跨輪對話上下文注入機制 | ~337 |

### Apr 27, 2026

| ID | Time | T | Title | Read |
|----|------|---|-------|------|
| #10843 | 2:20 PM | 🟣 | Quick-reference decision card created for rapid subtask splitting triage | ~273 |
| #10840 | 2:19 PM | 🟣 | SUBTASK_FRAMEWORK.md created - comprehensive agent task splitting methodology | ~263 |
| #10837 | 2:18 PM | 🔵 | Alice project uses auto-generated phase-based task tracking system | ~458 |
| #10835 | 2:14 PM | 🔵 | Alice project subtask tracking methodology revealed | ~470 |
</claude-mem-context>

## Dynamic Model Routing 行為

#93 後，dynamic model routing 採用 sticky session 優先策略，避免連續對話中途因 auto-triage 切換模型而清掉 Claude/Codex CLI session context。已建立 session 且尚未閒置超時時，後續訊息沿用目前模型與 session，不重新 triage。

Routing 優先順序：

1. 使用者明確指定模型：`/fast`、`/smart`、`/deep`、`/gfast`、`/gsmart`、`/gdeep`、`/plan` 會套用指定模型。若切換會清掉既有 session context，Telegram 會附上「上下文將重置」提示。
2. Sticky session：`sticky_session` 或 `sticky_mode` 啟用，且目前 session 未超過 idle timeout 時，沿用 `lastUsedModel`，routing reason 記為 `sticky_session`。
3. Follow-up detection：沒有可用 sticky session 時，短訊息、接續詞、代名詞指涉、短問句等 follow-up 仍沿用目前模型，routing reason 記為 `follow_up`。
4. Auto-triage：只有新對話、session 已閒置超時、或使用者主動清除/重置後，才重新進入 local score / Haiku 或 GPT triage / static rules 路由。

Follow-up detection 規則包含：

- 接續語氣詞：`但是`、`那`、`繼續`、`還有`、`所以`、`but`、`and`、`also`、`continue`。
- 代名詞或指涉：`這個`、`那個`、`它`、`this`、`that`、`them`。
- 短追問：以 `為什麼`、`怎麼`、`why`、`how` 開頭且長度較短的問句。
- 短訊息：極短訊息預設視為 follow-up，以保留前一輪工具與檔案變更脈絡。

Session 控制：

- `model_routing.sticky_session` 與 `model_routing.sticky_mode` 都可啟用 sticky 行為。
- `model_routing.session_idle_timeout_min` 與 `model_routing.session_idle_timeout` 都可設定閒置超時，預設 5 分鐘。
- Session 閒置超過 timeout 後，下一則訊息會清除舊 session 與 recent bridge context，重新進入 triage。
- `/clear` 會主動清除目前 session、sticky model、plan mode 與 recent bridge context，回覆 `session_cleared`。
- `/reset` 仍可用於清除對話狀態，回覆既有 `conversation_cleared` 訊息。
