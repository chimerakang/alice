# Alice Bot 多國語系開發指南

本指南說明如何在 Alice Bot 中添加新語言支持。

## 概述

Alice Bot 使用基於 JSON 的 i18n（國際化）系統，支持動態語言切換和持久化存儲。當前支持：
- **繁體中文** (zh-TW) - 預設語言
- **英文** (en)

## 架構

### 核心組件

```
locales/
├── zh-TW.json        # 繁體中文資源包
├── en.json           # 英文資源包
└── [new-lang].json   # 新語言（按此格式添加）

internal/app/
├── i18n.go           # I18nManager - 語言管理核心
├── telegram.go       # Bot 集成層
└── storage.go        # SQLite 持久化層
```

### 數據流

```
User: /lang ja
  ↓
handleLangCommand() 驗證語言支持
  ↓
setChatlanguage(chatID, "ja")
  ├─ 更新內存 langPreferences
  └─ 保存到 SQLite chat_language 表

後續消息顯示:
getLocalizedMessage(chatID, "key", vars)
  ├─ getChatLanguage() 查詢語言偏好
  ├─ i18n.GetMessage(lang, key, vars)
  └─ 返回本地化消息
```

## 添加新語言步驟

### 1. 創建語言資源文件

在 `locales/` 目錄創建新文件，格式為 `locales/[lang-code].json`

**示例：添加日文 (ja)**

```bash
# 從現有語言複製作為模板
cp locales/en.json locales/ja.json
```

編輯 `locales/ja.json`：

```json
{
  "lang": "ja",
  "name": "日本語",
  "messages": {
    "permission_denied": "❌ このボットを使用する権限がありません。",
    "no_storage": "❌ ストレージシステムが利用できません",
    "error_get_cost": "❌ コストデータの取得に失敗: {error}",
    "no_routing_data": "📊 週間ルーティング統計\n\nまだルーティングデータがありません",

    "actual_cost": "実際のコスト: ${cost}",
    "assumed_sonnet_cost": "Sonnetのみ使用と仮定: ${cost}",
    "savings": "節約額: *${savings}* ({percent}%)",

    "pii_detected": "⚠️ 機密情報を検出し、自動フィルタリングしました。プライバシーを保護してください。",
    "using_agent": "🤖 {agent} エージェントを使用してこのタスクを処理します",
    "error_occurred": "⚠️ エラーが発生しました: {error}",
    "error_prefix": "❌ エラー: {error}",

    "project_usage": "使用方法: /project <パスまたはプロジェクト名>",
    "project_not_found": "❌ {error}",
    "project_set": "✅ プロジェクトを設定しました: {name}",
    "project_path": "📂 パス: `{path}`",
    "project_type": "🔧 タイプ: {type}",

    "conversation_cleared": "🔄 会話をクリアしました\nこのセッションの使用量: {tokens_in}K in / {tokens_out}K out ({calls} 回の呼び出し)",
    "conversation_history_cleared": "🔄 会話履歴をクリアしました",

    "session_id": "`{session_id}`",
    "model_fast": "`{model}` (⚡ 高速モード)",
    "model_deep": "`{model}` (🧠 深度モード)",
    "model_default": "`{model}`",

    "checkpoints_usage": "使用方法: /checkpoints [list|stats]",
    "multiagent_enabled": "✅ マルチエージェント調整が有効になります",
    "multiagent_disabled": "❌ マルチエージェント調整が無効になります",
    "multiagent_usage": "使用方法: /multiagent [enable|disable|status|stats]",

    "voice_disabled": "🎤 音声テキスト変換機能は現在無効です。管理者に `enable_voice_support` を有効にしてもらってください。",
    "voice_no_api_key": "🎤 音声テキスト変換には OpenAI API キーが必要です。管理者に `openai_api_key` を設定してもらってください。",
    "voice_file_too_large": "🎤 音声ファイルが大きすぎます ({size})。制限は {limit}MB です。",
    "voice_too_long": "🎤 音声メッセージが長すぎます ({duration} 秒)。制限は 25 分です。",
    "voice_downloading": "🎤 音声ファイルをダウンロード中...",
    "voice_download_failed": "🎤 音声ファイルのダウンロードに失敗しました。後でもう一度お試しください。",
    "voice_transcribing": "🎤 音声をトランスクリプション中...",
    "voice_transcribe_failed": "🎤 音声トランスクリプションに失敗しました。後でもう一度お試しください。",
    "voice_not_recognized": "🎤 音声コンテンツを認識できませんでした。もう一度録音してください。",
    "voice_transcription_result": "🎤 *音声トランスクリプション結果*:\n\n「{text}」\n\nAI 分析に送信中...",
    "voice_caption_pii_detected": "⚠️ 音声キャプションで機密情報が検出され、自動フィルタリングされました。",

    "help_title": "📚 Alice Bot ヘルプ",
    "help_commands": "利用可能なコマンド:",
    "help_project": "/project - プロジェクトを設定または切り替え",
    "help_clear": "/clear - 会話履歴をクリア",
    "help_status": "/status - 現在の状態を表示",
    "help_usage": "/usage - トークン使用状況を表示",
    "help_task_savings": "/task-savings - スマートルーティングの節約額を表示",
    "help_checkpoint": "/checkpoint - チェックポイント管理",
    "help_fast": "/fast - 高速モデル (Haiku) を強制使用",
    "help_deep": "/deep - 深度モデル (Opus) を強制使用",
    "help_auto": "/auto - 自動モデルルーティング",
    "help_lang": "/lang - ボット言語を切り替え",
    "help_tasks": "/tasks - タスク一覧を表示",
    "help_multiagent": "/multiagent - マルチエージェント調整設定",

    "lang_switched": "✅ 言語を変更しました: {lang}",
    "lang_current": "現在の言語: {lang}",
    "lang_usage": "使用方法: /lang [言語コード]\n\nサポートされている言語:\n• zh-TW - 繁體中文\n• en - English\n• ja - 日本語",
    "lang_not_found": "❌ サポートされていない言語: {lang}",

    "tasks_command_title": "📋 Alice タスク一覧",
    "tasks_no_issues": "利用可能なタスクはありません",

    "mode_switched_fast": "✅ 高速モードに切り替わりました\nモデル: `{model}`",
    "mode_switched_deep": "✅ 深度モードに切り替わりました\nモデル: `{model}`",
    "mode_switched_auto": "✅ 自動ルーティングモードに切り替わりました",
    "routing_disabled": "⚠️ 動的モデルルーティングが有効になっていません",

    "task_aborted": "🛑 タスク実行を停止しました",
    "task_finished": "⚠️ タスクは完了しているため、停止は不要です",
    "no_running_task": "ℹ️ 現在実行中のタスクはありません",

    "checkpoint_disabled": "❌ チェックポイントシステムが有効になっていません",
    "checkpoint_list_failed": "❌ チェックポイント一覧の取得に失敗: {error}",

    "unknown_command": "不明なコマンド。 /help で利用可能なコマンドを表示してください"
  }
}
```

### 2. 更新語言資源缺失檢查

查看 `locales/zh-TW.json` 和 `locales/en.json`，確保新語言包含所有必要的 key。

**message keys 清單** (67 個):

**核心訊息** (10):
- permission_denied
- no_storage
- error_get_cost
- no_routing_data
- actual_cost
- assumed_sonnet_cost
- savings
- pii_detected
- using_agent
- error_occurred

**項目管理** (5):
- project_usage
- project_not_found
- project_set
- project_path
- project_type

**對話管理** (3):
- conversation_cleared
- conversation_history_cleared
- session_id

**模型相關** (3):
- model_fast
- model_deep
- model_default

**檢查點** (2):
- checkpoints_usage
- (其他在 checkpoint 相關方法中)

**多代理** (2):
- multiagent_enabled
- multiagent_disabled
- multiagent_usage

**語音功能** (10):
- voice_disabled
- voice_no_api_key
- voice_file_too_long
- voice_downloading
- voice_download_failed
- voice_transcribing
- voice_transcribe_failed
- voice_not_recognized
- voice_transcription_result
- voice_caption_pii_detected

**幫助文本** (11):
- help_title
- help_commands
- help_project
- help_clear
- help_status
- help_usage
- help_task_savings
- help_checkpoint
- help_fast
- help_deep
- help_auto
- help_lang
- help_tasks
- help_multiagent

**語言切換** (4):
- lang_switched
- lang_current
- lang_usage
- lang_not_found

**任務清單** (2):
- tasks_command_title
- tasks_no_issues

**模式切換** (4):
- mode_switched_fast
- mode_switched_deep
- mode_switched_auto
- routing_disabled

**任務控制** (3):
- task_aborted
- task_finished
- no_running_task

**檢查點** (3):
- checkpoint_disabled
- checkpoint_list_failed
- checkpoints_usage

**其他** (1):
- unknown_command

### 3. 測試新語言

```bash
# 編譯 bot
go build -o alice ./cmd/alice

# 啟動 bot
./alice

# 在 Telegram 中測試
/lang ja       # 切換至日文
/help          # 查看幫助（應以日文顯示）
```

### 4. 驗證檢查清單

- [ ] JSON 檔案格式正確（無語法錯誤）
- [ ] 所有必需的 message keys 都已翻譯
- [ ] 模板變數 (e.g., {error}, {model}) 保留在翻譯中
- [ ] 沒有硬編碼的語言代碼（僅在 `lang_usage` 中引用）
- [ ] 編譯成功，無錯誤
- [ ] `/lang [code]` 命令可以切換到新語言
- [ ] 新語言下 `/help` 顯示正確的翻譯

## 文件結構

```
locales/
├── zh-TW.json
│   ├── lang: "zh-TW"
│   ├── name: "繁體中文"
│   └── messages: { ... 67 keys ... }
├── en.json
│   └── ...
└── [new-lang].json
    └── ...
```

## 模板變數

消息中可使用以下模板變數（使用 `{key}` 語法）：

| 變數 | 用途 | 示例 |
|-----|------|------|
| `{error}` | 錯誤描述 | "無法取得成本數據: {error}" |
| `{model}` | 模型名稱 | "已切換至快速模式\n模型: `{model}`" |
| `{lang}` | 語言名稱 | "語言已切換為: {lang}" |
| `{agent}` | 代理類型 | "使用 {agent} 代理處理此任務" |
| `{tokens_in}` | 輸入 Token 數 | "{tokens_in}K in" |
| `{tokens_out}` | 輸出 Token 數 | "{tokens_out}K out" |
| `{calls}` | API 調用次數 | "{calls} 次呼叫" |
| `{cost}` | 成本金額 | "實際花費: ${cost}" |
| `{percent}` | 百分比 | "節省金額: {percent}%" |
| `{size}` | 檔案大小 | "語音檔案過大 ({size})" |
| `{limit}` | 大小限制 | "限制為 {limit}MB" |
| `{duration}` | 時間長度 | "過長 ({duration} 秒)" |
| `{text}` | 文字內容 | "語音轉錄結果: {text}" |
| `{name}` | 專案名稱 | "專案已設定為: {name}" |
| `{path}` | 檔案路徑 | "路徑: `{path}`" |
| `{type}` | 專案類型 | "類型: {type}" |

## 實現細節

### I18nManager 類 (internal/app/i18n.go)

```go
// 獲取本地化消息
msg := i18nManager.GetMessage("ja", "error_prefix", map[string]string{
  "error": "連接超時",
})
// 結果: "❌ エラー: 連接超時"

// 檢查語言支持
if i18nManager.IsLanguageSupported("ja") {
  // ...
}

// 獲取可用語言
langs := i18nManager.GetAvailableLanguages()
// 結果: map[string]string{"zh-TW": "繁體中文", "en": "English", "ja": "日本語", ...}
```

### Telegram Bot 集成 (internal/app/telegram.go)

```go
// 獲取用戶語言偏好並取得本地化消息
msg := t.getLocalizedMessage(chatID, "error_prefix", map[string]string{
  "error": err.Error(),
})
t.send(key, msg)

// 切換用戶語言
t.setChatlanguage(chatID, "ja")  // 保存到內存 + SQLite
```

### SQLite 持久化 (internal/app/storage.go)

```go
// 保存用戶語言偏好
storage.SaveChatLanguage(chatID, "ja")

// 查詢用戶語言偏好
lang, err := storage.GetChatLanguage(chatID)
```

## 常見問題

### Q: 如何添加新的消息鍵值？

A:
1. 在所有 `locales/*.json` 檔案中添加新的 key-value 對
2. 在 `telegram.go` 中使用 `t.getLocalizedMessage(chatID, "new_key", vars)`
3. 編譯測試

### Q: 如何修改現有翻譯？

A: 編輯對應的 `locales/*.json` 檔案，重新編譯即可。下次 bot 啟動時自動加載新翻譯。

### Q: 支持 RTL（從右到左）語言嗎？

A: 目前不支持。RTL 需要前端 UI 適配。可後續專案擴展。

### Q: 如何處理複數形式？

A: 目前不支持複數規則。可使用帶變數的消息進行數量標記，例如：
```json
"items_count": "{count} 個項目"
```

### Q: 如何翻譯包含換行符的消息？

A: 使用 `\n` 進行換行，例如：
```json
"multiline": "第一行\n第二行"
```

## 貢獻指南

新增語言時：
1. 提交 PR 包含新語言檔案
2. 確保所有 message keys 完整
3. 驗證編譯無誤
4. 提供簡短的語言文化背景說明

## 相關檔案

- `internal/app/i18n.go` - I18nManager 實現
- `internal/app/telegram.go` - Bot 集成層
- `internal/app/storage.go` - 持久化層
- `docs/i18n_guide.md` - 本指南
- `memory/i18n_implementation.md` - 技術筆記
