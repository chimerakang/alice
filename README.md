# Alice — Claude Code Telegram Agent

透過 Telegram 操控的 AI 程式開發助手。底層呼叫 Claude Code CLI，搭配 Claude Max 訂閱使用，無額外 token 費用。

## 架構

```
Telegram ←→ Go Bot ←→ Claude Code CLI (claude -p)
                            ↕
                    內建 agent loop + tools
                    (Read, Write, Edit, Bash, Glob, Grep ...)
```

Bot 只負責轉發訊息，所有工具執行和 agent loop 由 Claude Code CLI 內建處理。

## 功能

- 📁 讀取 / 寫入 / 修改檔案（CLI 內建）
- 🔍 搜尋程式碼（CLI 內建 Glob + Grep）
- 💻 執行 shell 指令（CLI 內建 Bash）
- 🔄 對話上下文保持（CLI session resume）
- 🔒 使用者白名單
- 📂 多專案切換
- 📊 Token 用量追蹤

## 前置條件

- **Claude Max 訂閱**（$200/月，無限使用）
- 機器上已安裝並登入 Claude Code CLI：
  ```bash
  npm install -g @anthropic-ai/claude-code
  claude login
  ```
- 驗證 CLI 正常：`claude -p "hello"`

## 快速開始

### 1. 取得 Telegram 設定

- **Telegram Bot Token**: 跟 [@BotFather](https://t.me/BotFather) 建立一個 bot
- **你的 Telegram User ID**: 跟 [@userinfobot](https://t.me/userinfobot) 取得

### 2. 設定

```bash
cp config.example.json config.json
# 編輯 config.json
```

```json
{
  "telegram_token": "你的 Telegram Bot Token",
  "model": "claude-sonnet-4-20250514",
  "default_project_dir": "/path/to/your/project",
  "allowed_user_ids": [你的UserID]
}
```

或用環境變數：

```bash
export TELEGRAM_BOT_TOKEN="123456:ABC-DEF"
export CLAUDE_MODEL="claude-sonnet-4-20250514"
export PROJECT_DIR="/path/to/your/project"
export ALLOWED_USER_IDS="123456789"
```

### 3. 執行

```bash
# 直接跑
go run .

# 或 build
go build -o claude-tg-agent .
./claude-tg-agent
```

## Telegram 指令

| 指令 | 說明 |
|------|------|
| `/help` | 顯示說明 |
| `/project <路徑>` | 切換專案目錄 |
| `/reset` | 清除對話歷史 |
| `/status` | 查看目前狀態 |
| `/usage` | 查看 token 用量 |

直接傳送文字訊息就會啟動 Claude Code 來處理你的需求。

## 使用範例

```
你: 看一下這個專案的結構，然後幫我加一個 health check endpoint

Bot: 🔧 Claude Code 處理中 ...

Bot: 我已經完成了以下修改：
     1. 在 main.go 新增了 /health endpoint
     2. 回傳 JSON {"status": "ok", "timestamp": ...}
     ...
```

## 模型選擇

在 `config.json` 的 `model` 欄位或 `CLAUDE_MODEL` 環境變數中設定：

- `claude-sonnet-4-20250514` — 預設，性價比最好
- `claude-opus-4-20250514` — 最強，複雜任務
- `claude-haiku-4-5-20251001` — 最快，簡單任務

## Docker 部署（需額外設定）

```bash
docker build -t claude-tg-agent .

docker run -d \
  -v $(pwd)/config.json:/app/config.json:ro \
  -v /path/to/your/project:/project \
  -v ~/.claude:/home/claude/.claude \
  claude-tg-agent
```

注意：Docker 內的 Claude CLI 需要獨立登入認證。若主機認證存在 OS Keychain 中，需在容器內執行 `claude login` 完成認證。

## License

MIT
