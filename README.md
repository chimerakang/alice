# Claude TG Agent

透過 Telegram 操控的 AI 程式開發助手。用 Anthropic API + Tool Use 實現，等同於你自己的 Claude Code。

## 架構

```
Telegram ←→ Go Bot ←→ Anthropic API (tool_use loop)
                ↕
        本機檔案系統 / Shell
```

核心是一個 agent loop：Claude 可以讀寫檔案、執行指令、搜尋程式碼，反覆迭代直到完成任務。

## 功能

- 📁 讀取 / 寫入 / 修改檔案
- 🔍 搜尋程式碼（支援 ripgrep）
- 💻 執行 shell 指令
- 📂 瀏覽專案結構
- 🔄 對話上下文保持
- 🔒 使用者白名單
- 📊 多專案切換

## 快速開始

### 1. 取得 API Keys

- **Anthropic API Key**: https://console.anthropic.com/
- **Telegram Bot Token**: 跟 [@BotFather](https://t.me/BotFather) 建立一個 bot
- **你的 Telegram User ID**: 跟 [@userinfobot](https://t.me/userinfobot) 取得

### 2. 設定

```bash
cp config.example.json config.json
# 編輯 config.json 填入你的 keys
```

或用環境變數：

```bash
export ANTHROPIC_API_KEY="sk-ant-xxxxx"
export TELEGRAM_BOT_TOKEN="123456:ABC-DEF"
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

### Docker 部署

```bash
docker build -t claude-tg-agent .

docker run -d \
  -e ANTHROPIC_API_KEY="sk-ant-xxxxx" \
  -e TELEGRAM_BOT_TOKEN="123456:ABC-DEF" \
  -e ALLOWED_USER_IDS="123456789" \
  -v /path/to/your/project:/project \
  claude-tg-agent
```

## Telegram 指令

| 指令 | 說明 |
|------|------|
| `/help` | 顯示說明 |
| `/project <路徑>` | 切換專案目錄 |
| `/reset` | 清除對話歷史 |
| `/status` | 查看目前狀態 |

直接傳送文字訊息就會啟動 agent 來處理你的需求。

## 使用範例

```
你: 看一下這個專案的結構，然後幫我加一個 health check endpoint

Bot: 🔧 list_files ...
Bot: 🔧 file_read ...
Bot: 🔧 file_write ...

Bot: 我已經完成了以下修改：
     1. 在 main.go 新增了 /health endpoint
     2. 回傳 JSON {"status": "ok", "timestamp": ...}
     ...
```

## 擴展

這是一個基礎框架，你可以加入：

- **自訂 tools**：在 `tools.go` 的 `BuildTools()` 和 `Execute()` 中新增
- **Web UI**：agent 核心和 Telegram 是分離的，可以接任何前端
- **多使用者 / 多專案**：已支援，每個 chat 獨立的 agent instance
- **成本追蹤**：API response 已包含 token usage，可以累計
- **Git 操作工具**：新增 git_commit, git_push 等專用 tools
- **CI/CD 觸發**：新增 tool 呼叫 GitHub Actions / GitLab CI

## 模型選擇

在 `config.json` 或 `CLAUDE_MODEL` 環境變數中設定：

- `claude-sonnet-4-20250514` — 預設，性價比最好
- `claude-opus-4-20250514` — 最強，複雜任務
- `claude-haiku-4-5-20251001` — 最快最便宜，簡單任務

## License

MIT
