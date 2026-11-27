<claude-mem-context>

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
