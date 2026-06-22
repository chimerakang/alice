---
name: alice-i18n
description: Alice 專案 i18n 專用 skill。當任務要新增、修改或除錯任何使用者可見文字時觸發，包括 Telegram 訊息、Dashboard 文案、錯誤訊息、狀態提示、message key、模板變數、或 zh-TW/en 雙語同步；不適用於純後端邏輯或非 UI 字串。
---

# Alice i18n Guidelines

## 觸發時機

遇到下列任一情境就載入本 skill：

- 新增或修改任何 user-facing 文字
- 修正翻譯缺漏、語系不同步、或 key 命名
- 調整模板變數、占位符、或訊息格式化邏輯
- 檢查是否有人在 Go / React / API 內硬編碼字串

Alice 採用集中式 i18n：SQLite 儲存 per-chat 語言偏好 + 記憶體快取。所有使用者可見文字**必須**透過 message key 取得，禁止硬編碼。

## 架構流程

```
User Message
    ↓
TelegramBot.handleCommand()
    ↓
t.getLocalizedMessage(chatID, messageKey, templateVars)
    ↓
I18nManager.GetMessage(langCode, messageKey, templateVars)
    ↓
Memory Cache Hit? → Return cached value
Memory Cache Miss? → Query SQLite chat_language → Apply template → Cache → Return
```

## 支援語言

| Language | Code | File |
|----------|------|------|
| Traditional Chinese | `zh-TW` | `locales/zh-TW.json` |
| English | `en` | `locales/en.json` |

## 正確與錯誤範例

**❌ DO NOT 硬編碼：**
```go
t.send(key, "Token 使用量統計")
```

**✅ 正確做法：**
```go
msgKey := "token_usage_format"
msg := t.getLocalizedMessage(key.chatID, msgKey, map[string]string{
    "{input}":  fmt.Sprintf("%d", stats.TotalInputTokens),
    "{output}": fmt.Sprintf("%d", stats.TotalOutputTokens),
})
t.send(key, msg)
```

## 新增使用者文字流程

1. **識別所有 user-facing strings**（Telegram 回應、Dashboard、錯誤、狀態）
2. **同步新增兩個語系檔**：
   ```
   locales/
   ├── zh-TW.json    # "new_feature_message": "新功能訊息..."
   └── en.json       # "new_feature_message": "New feature message..."
   ```
3. **程式碼使用 key**：
   ```go
   msg := t.getLocalizedMessage(chatID, "new_feature_message", nil)
   ```
4. **動態內容用模板變數**（統一 `{variable}` 格式）：
   ```json
   { "cost_report": "您的成本為 ${cost}，節省 ${savings}%" }
   ```

## Message Key 命名規範

snake_case，依功能分組：

```
token_usage_format          → Token 用量顯示
error_get_cost              → 取得成本失敗
model_distribution_title    → 模型分佈圖標題
mode_switched_fast          → 模式切換通知
usage_stats_by_model        → 按模型統計標題
```

## 實作參考

**Backend (Go)**:
- `internal/app/i18n.go` — `I18nManager` + `GetMessage()`
- sync.RWMutex 保護的 map 快取，O(1) lookup
- SQLite `chat_language` 表儲存 per-chat 偏好，預設 fallback 至 `zh-TW`

**Frontend (React)**:
- `frontend/src/store/languageStore.ts` — Zustand store + localStorage
- `LanguageSwitcher` 元件（sidebar）
- `/api/language` 端點同步後端

## Code Review Checklist

PR 合併前確認：

- [ ] Go 程式碼無硬編碼字串（`telegram.go`、`web.go` 等）
- [ ] 所有訊息使用 `t.getLocalizedMessage(chatID, key, vars)`
- [ ] Message key 同步加入 `zh-TW.json` 與 `en.json`
- [ ] 模板變數命名一致（`{variable}` 格式）
- [ ] Key 遵循 snake_case 命名規範
- [ ] 訊息檔中無格式化邏輯（邏輯保留在 Go）

## 常見錯誤

| 錯誤 | 問題 | 解法 |
|------|------|------|
| `fmt.Sprintf("Total: %d", count)` | 硬編碼英文 | 用 key + 模板 |
| 中英混雜單一字串 | UX 不一致 | 拆成多個 key |
| `strings.ReplaceAll(msg, ...)` | 無法翻譯 | 用模板變數 |
| 只加英文翻譯 | 支援不完整 | 雙語同步 |
| 訊息檔含 count 邏輯 | 邏輯污染資料層 | 邏輯留在 Go，傳入模板 |

## 詳細文件

完整指南見 [docs/i18n_guide.md](../../../docs/i18n_guide.md)。
