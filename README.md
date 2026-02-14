<p align="center">
  <h1 align="center">👁️ Alice Monitor</h1>
  <p align="center"><strong>The Local-First AI Agent Observability & Security Platform.</strong></p>
  <p align="center">Stop debugging your LLM agents with <code>print()</code>. Visualize reasoning, track API costs, and catch security leaks in real-time.</p>
</p>

<p align="center">
  <a href="#-why-alice-monitor">Why?</a> &bull;
  <a href="#-key-features">Features</a> &bull;
  <a href="#-quick-start">Quick Start</a> &bull;
  <a href="#-tech-stack">Tech Stack</a> &bull;
  <a href="#-support--enterprise">Enterprise</a> &bull;
  <a href="#-license">License</a>
</p>

---

## 🚀 Why Alice Monitor?

As AI agents move from experimental scripts to production systems, terminal logs are no longer enough. Alice Monitor acts as the **flight data recorder** for your AI agents.

Built entirely in **Go**, it ships as a single, lightweight binary with a gorgeous **OLED dark-mode dashboard**. It's designed specifically for developers who care about **data privacy**, **cost control**, and **deep observability** — without sending sensitive data to third-party cloud platforms.

### ✨ See it in action

![Dashboard Overview](docs/screenshots/dashboard.png)

---

## 🔥 Key Features

### 🔒 Enterprise-Grade Security & PII Redaction

Don't leak your users' data to LLMs. Alice Monitor intercepts and **automatically masks Personally Identifiable Information** (PII) like emails, credit cards, and SSNs before they hit the LLM API, complete with a full security audit log.

![Security & PII Masking](docs/screenshots/security.png)

### 💰 Real-Time Cost & Token Tracking

Prevent "infinite loop" API bankruptcies. Track token usage, estimate costs in real-time, and visualize performance bottlenecks across multiple agents and projects simultaneously.

![Cost & Token Tracking](docs/screenshots/performance.png)

### ⏪ Time-Travel Debugging (Timeline)

Understand exactly *why* your agent made a decision. Alice tracks the entire Chain-of-Thought, showing precise tool execution times, pre-danger safety checkpoints, and Git commit associations.

![Timeline Debugging](docs/screenshots/timeline.png)

### ⚡ Zero-Dependency & Local-First

No complex microservices. Run it locally alongside your codebase. Your logs, your prompts, and your data **stay on your machine**.

---

## 🛠️ Quick Start

Getting started is as simple as running a single Go command.

### Prerequisites

- **Go 1.24+** installed
- **Claude Code CLI** installed and authenticated:
  ```bash
  npm install -g @anthropic-ai/claude-code
  claude auth
  ```
- A **Telegram Bot Token** from [@BotFather](https://t.me/BotFather)

### Installation

```bash
# Clone the repository
git clone https://github.com/chimerakang/alice.git
cd alice

# Copy and edit config
cp config.example.json config.json
# Edit config.json with your Telegram token and settings

# Build the bot
go build -o alice ./cmd/alice

# Start the bot
./alice
```

### Launch the Dashboard

The OLED dark-mode dashboard runs as a Docker container:

```bash
# Start the dashboard
docker compose up -d dashboard

# Open your browser
open http://localhost:3939
```

> **Tip:** The bot runs natively on port `8082` (REST API + WebSocket). The dashboard at port `3939` is an nginx reverse proxy serving the React SPA and forwarding API calls to the bot.

---

## 💻 Tech Stack

| Layer | Technology |
|-------|-----------|
| **Backend** | Go (high-concurrency, WebSocket streaming, SQLite storage) |
| **Frontend** | React + TypeScript + Vite (Tailwind CSS, OLED dark-mode) |
| **Integration** | Telegram Bot API for real-time mobile alerts |
| **Monitoring** | Optional Prometheus + Grafana stack |

---

## 🤝 Support & Enterprise

Alice Monitor is open-source and free for personal and community use.

### 🏢 Looking for Enterprise Features?

If you are building commercial multi-tenant SaaS or deploying agents in strict compliance environments (Healthcare, FinTech), we offer a **Commercial License** and **Pro Features** including:

| Tier | Price | Highlights |
|------|-------|-----------|
| **Community** | Free | Core monitoring, basic PII detection, single-user dashboard |
| **Team** | $99/mo | Custom PII rules, Team RBAC, priority support (up to 20 users) |
| **Enterprise** | $299/mo | Full SSO (SAML/OIDC), audit exports, SIEM integration, SLA |
| **Custom** | Contact us | On-premise deployment, custom integrations, dedicated support |

> Annual plans receive a 20% discount.

📩 For enterprise inquiries, see our full [Commercialization Plan](docs/COMMERCIALIZATION.md) or contact us at <!-- TODO: Add contact email -->.

---

## 📄 License

This project is dual-licensed:

- **Open-source** — [AGPL-3.0](LICENSE) for personal, community, and open-source use
- **Commercial** — A proprietary license for closed-source SaaS and commercial products

> If you intend to use Alice Monitor as part of a closed-source commercial product or SaaS, a commercial license is required. See [docs/COMMERCIALIZATION.md](docs/COMMERCIALIZATION.md) for details.

---

<br>

<h1 align="center">📖 繁體中文文件</h1>

---

# Alice — Claude Code Telegram Agent

透過 Telegram 操控的 AI 程式開發助手。底層呼叫 Claude Code CLI，搭配 Claude Max 訂閱使用，無額外 token 費用。

## 架構

### 多接口統一架構
```
        ┌─── Telegram Bot ←─┐
        │                  │
        ├─── Web Dashboard ←┼─→ Alice Core Agent ←→ Claude API
        │                  │           ↕
        └─── REST API ←────┘    Tool Executor
                                     ↕
                            ┌─── File Operations
                            ├─── Shell Commands
                            ├─── Code Search
                            ├─── Git Integration
                            └─── Checkpoint System
                                     ↕
                            ┌─── SQLite Database
                            ├─── WebSocket Hub
                            └─── Performance Monitor
```

### 核心組件
- **Alice Agent** - 主要 AI 代理邏輯和工具協調
- **Checkpoint System** - 狀態快照和回溯功能
- **Web Dashboard** - 實時監控和視覺化介面
- **Multi-Channel Support** - Telegram、Web、REST API 多重接口

## 功能

### 🤖 核心 AI 功能
- 📁 讀取 / 寫入 / 修改檔案（CLI 內建）
- 🔍 搜尋程式碼（CLI 內建 Glob + Grep）
- 💻 執行 shell 指令（CLI 內建 Bash）
- 🔄 對話上下文保持（CLI session resume）
- 🔒 使用者白名單
- 📂 多專案切換
- 📊 Token 用量追蹤
- 🗣️ **Forum Topics 支援** - 每個 Topic 獨立專案與對話

### 📸 **NEW: Checkpoint & State Snapshot System**
- 🔄 **自動快照** - 危險操作前自動建立檢查點
- 💾 **SQLite 持久化** - 完整的檢查點數據存儲
- 🔍 **危險操作檢測** - 智能識別 file_write、rm 等風險命令
- 🌐 **REST API** - 完整的檢查點 CRUD 操作
- ⚡ **實時監控** - WebSocket 事件廣播

### 📊 **Advanced Monitoring & Analytics**
- 🎯 **AI 決策透明度** - 完整的決策過程記錄
- ⚡ **性能監控** - 實時性能指標收集和分析
- 🤝 **多代理協調** - 智能任務分配和協作
- 🔒 **安全增強** - PII 檢測、審計日誌、速率限制
- 🚀 **DevOps 整合** - Docker、Kubernetes、CI/CD 支援

### 🌐 **Web Dashboard Interface**
- 📈 **Timeline 視覺化** - 即時 AI 決策過程時間軸
- 💻 **Terminal 模擬器** - 彩色終端輸出與過濾
- 🎨 **OLED 優化主題** - 深黑背景，高對比度設計
- 📱 **響應式設計** - 支援桌面、平板、手機
- 🔍 **智能過濾** - 事件類型、時間範圍、狀態過濾

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
  "allowed_user_ids": [你的UserID],

  "enable_web_interface": true,
  "web_port": "8082",
  "web_static_dir": "./web",

  "enable_persistence": true,
  "database_path": "./data/alice.db",
  "data_retention_days": 30
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
# Build
go build -o alice ./cmd/alice

# 啟動 bot
./alice

# 啟動 Dashboard (Docker)
docker compose up -d dashboard

# 打開瀏覽器訪問
open http://localhost:3939
```

### 4. 🌐 Web Dashboard 使用

啟動後，Web Dashboard 在 `http://localhost:3939` 運行：

#### 🎨 主要功能頁面

| 頁面 | 功能描述 |
|------|----------|
| **Dashboard** | 系統概覽、指標統計、成本追蹤 |
| **Timeline** | 即時 AI 決策過程時間軸 |
| **Security** | PII 檢測、安全審計日誌 |
| **Performance** | 性能指標、回應時間分析 |

#### 📊 REST API 端點

```bash
# 檢查點管理
POST   /api/checkpoints/create     # 創建檢查點
GET    /api/checkpoints           # 列出檢查點
DELETE /api/checkpoints           # 刪除檢查點
GET    /api/checkpoints/stats     # 檢查點統計

# 系統監控
GET    /api/health               # 系統健康檢查
GET    /ws                       # WebSocket 連接
GET    /metrics                  # Prometheus 指標
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

### Forum Topics 支援

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

## 模型選擇

在 `config.json` 的 `model` 欄位或 `CLAUDE_MODEL` 環境變數中設定：

- `claude-sonnet-4-20250514` — 預設，性價比最好
- `claude-opus-4-20250514` — 最強，複雜任務
- `claude-haiku-4-5-20251001` — 最快，簡單任務
