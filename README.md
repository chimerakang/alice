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
- 🗣️ **Forum Topics 支援** - 每個 Topic 獨立專案與對話
- ⚡ 零依賴純 HTTP API 實作

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

## Telegram 群組設定

### 🔧 建立和設定群組

#### 1. 建立 Telegram 群組
1. 在 Telegram 中建立新群組（至少需要 2 個成員）
2. 將你的 Bot 加入群組：
   - 在群組中輸入 `@你的bot用戶名`
   - 或者進入 Bot 對話，點選 "Add to Group"

#### 2. 設定 Bot 權限
Bot 需要以下基本權限：
- ✅ **傳送訊息**
- ✅ **讀取訊息歷史**
- ✅ **回覆訊息**

#### 3. 開啟 Forum Topics（群組多專案支援）

**開啟 Topics：**
1. 進入群組設定 → "Group Type"
2. 開啟 "Topics" 功能
3. 群組會轉換為 Forum 模式

**建立專案 Topics：**
```
建議 Topic 命名：
🖥️ Frontend        # 前端專案
⚙️ Backend         # 後端 API
📱 Mobile App      # 行動 App
📚 Documentation   # 文件專案
🧪 Experiments     # 實驗性功能
```

#### 4. 設定各 Topic 的專案目錄

在每個 Topic 中執行：
```
/project /path/to/specific/project
```

例如：
- **Frontend** Topic: `/project ~/projects/web-app`
- **Backend** Topic: `/project ~/projects/api-server`
- **Mobile** Topic: `/project ~/projects/mobile-app`

### 🔒 權限和安全

**白名單設定：**
- 在 `config.json` 中設定 `allowed_user_ids`
- 或使用環境變數 `ALLOWED_USER_IDS="123456789,987654321"`
- 只有白名單內的用戶可以使用 Bot

**最佳實務：**
- 建議建立私人群組（僅邀請團隊成員）
- 為敏感專案使用獨立的 Bot instance
- 定期檢查群組成員

## Telegram 指令

| 指令 | 說明 |
|------|------|
| `/help` | 顯示說明 |
| `/project <路徑>` | 切換專案目錄（支援相對路徑） |
| `/reset` | 清除對話歷史 |
| `/status` | 查看目前狀態 |
| `/usage` | 查看 token 用量 |

直接傳送文字訊息就會啟動 Claude Code 來處理你的需求。

### Forum Topics 支援 🆕

在 Telegram 群組中開啟 Topics 功能，Alice 會為每個 Topic 維護獨立的：
- 對話歷史
- 專案目錄
- CLI session

這讓你可以在同一個群組中同時處理多個專案，每個 Topic 就是一個獨立的工作環境。

## 使用範例

### 基本使用
```
你: 看一下這個專案的結構，然後幫我加一個 health check endpoint

Bot: 🔧 Claude Code 處理中 ...

Bot: 我已經完成了以下修改：
     1. 在 main.go 新增了 /health endpoint
     2. 回傳 JSON {"status": "ok", "timestamp": ...}
     ...
```

### Forum Topics 多專案範例

**步驟 1: 建立 Topics 並設定專案**
```
🖥️ Frontend Topic:
你: /project ~/projects/web-app
Bot: ✅ 專案已切換到: /Users/username/projects/web-app

⚙️ Backend Topic:
你: /project ~/projects/api-server
Bot: ✅ 專案已切換到: /Users/username/projects/api-server
```

**步驟 2: 在各 Topic 中獨立工作**
```
🖥️ Frontend Topic:
你: 幫我添加一個新的 React 組件
Bot: 我來為你建立一個新的 React 組件...

⚙️ Backend Topic:
你: 檢查 API 端點的效能問題
Bot: 我來分析 API 端點的效能...
```

每個 Topic 中的對話完全獨立，Alice 會記住各自的：
- 專案目錄和檔案狀態
- 對話上下文和歷史
- CLI session 和工作進度

## 群組使用指南

### 👥 團隊協作最佳實務

**方案 1: 單一群組多 Topics**
- 適合：小團隊（2-5人）處理相關專案
- 設定：一個群組，多個 Topics 分別對應不同專案
- 優點：集中管理、容易切換專案討論

**方案 2: 多群組分離**
- 適合：大團隊或機敏專案
- 設定：每個主要專案建立獨立群組
- 優點：更好的權限控制、避免干擾

### 📋 常見使用情境

**日常開發流程：**
```
1. 進入對應 Topic
2. /status 查看目前專案狀態
3. 直接描述需求：「幫我修復登入 bug」
4. Alice 自動分析程式碼並提供解決方案
5. /usage 查看本次開發的 token 用量
```

**專案交接：**
```
1. 新成員加入群組
2. 在相關 Topic 中使用 /help 了解指令
3. 使用 /project 確認專案目錄設定
4. 開始協作開發
```

**故障排除：**
- 如果 Bot 沒有回應，檢查是否在白名單中
- 如果專案路徑錯誤，使用 `/project` 重新設定
- 如果對話混亂，使用 `/reset` 清除歷史

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
