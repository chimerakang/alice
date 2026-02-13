# Claude Code Hooks 設置指南

> 讓 Alice AI Agent 攔截所有 Claude Code 互動（Terminal/VSCode/Telegram）

## 📋 概述

Claude Code Hooks 功能讓 Alice 能夠統一追蹤來自不同環境的 Claude AI 開發活動：

- **Terminal**: `claude` CLI 命令執行
- **VSCode**: Claude 擴展中的互動
- **Telegram**: Alice Bot 中的對話（已自動整合）

## 🎯 功能特色

- ✅ **多源互動追蹤**：統一記錄所有 Claude 會話
- ✅ **實時 WebSocket 推送**：即時更新到 Dashboard
- ✅ **來源標記區分**：Terminal/VSCode/Telegram 清楚標示
- ✅ **自動去重**：避免重複記錄同一會話
- ✅ **完整會話記錄**：包含 prompt、response、thinking、tool calls

## ⚡ 快速設置

### 1. 確保 Alice 正在運行

```bash
# 檢查 Alice 狀態
curl http://localhost:8082/api/status

# 如果沒有運行，啟動 Alice
cd /path/to/alice
./alice
```

### 2. 配置 Claude Code Hooks

在你的 Claude Code 設定檔中添加 hook 配置：

**全域設置** (`~/.claude/settings.json`)：
```json
{
  "hooks": {
    "enabled": true,
    "session_complete": "curl -X POST http://localhost:8082/api/hooks/claude-code -H 'Content-Type: application/json' -d @-",
    "session_active": "curl -X POST http://localhost:8082/api/hooks/claude-code -H 'Content-Type: application/json' -d @-"
  }
}
```

**專案設置** (`.claude/settings.json`)：
```json
{
  "hooks": {
    "enabled": true,
    "session_complete": "/path/to/alice/scripts/claude-hook.sh",
    "session_active": "/path/to/alice/scripts/claude-hook.sh"
  }
}
```

### 3. 使用預製的 Hook Script（推薦）

Alice 提供了預製的 hook script，更穩定且包含錯誤處理：

```bash
# 複製 hook script 到你的路徑
cp /path/to/alice/scripts/claude-hook.sh ~/.local/bin/
chmod +x ~/.local/bin/claude-hook.sh

# 編輯 Claude Code 設定
vim ~/.claude/settings.json
```

**使用 Hook Script 的設定**：
```json
{
  "hooks": {
    "enabled": true,
    "session_complete": "~/.local/bin/claude-hook.sh",
    "session_active": "~/.local/bin/claude-hook.sh"
  }
}
```

## 🔧 Hook Script 範例

Alice 的 `scripts/claude-hook.sh` 包含：

```bash
#!/bin/bash
# Alice Claude Code Hook Script
# 自動發送 Claude Code 會話到 Alice Dashboard

ALICE_URL="http://localhost:8082/api/hooks/claude-code"
LOG_FILE="$HOME/.alice/hook.log"

# 讀取 stdin JSON payload
PAYLOAD=$(cat)

# 發送到 Alice API
if curl -s -X POST "$ALICE_URL" \
   -H "Content-Type: application/json" \
   -d "$PAYLOAD" \
   >> "$LOG_FILE" 2>&1; then
    echo "Hook sent successfully" >> "$LOG_FILE"
else
    echo "Hook failed: $PAYLOAD" >> "$LOG_FILE"
fi
```

## 📊 驗證設置

### 1. 測試 Hook 連接

```bash
# 測試 Alice API 是否可達
curl -X POST http://localhost:8082/api/hooks/claude-code \
  -H "Content-Type: application/json" \
  -d '{
    "session_id": "test-123",
    "event": "session_active",
    "source": "terminal",
    "project_dir": "/test",
    "user_prompt": "test hook connection"
  }'

# 預期回應
{"status":"ok","session":"test-123"}
```

### 2. 執行實際測試

```bash
# 在 Terminal 中運行 Claude Code
claude "hello world"

# 檢查 Alice Dashboard
open http://localhost:8082

# 在 Timeline 頁面應該看到：
# 🖥️ CLI 標籤的新決策記錄
```

### 3. 檢查日誌

```bash
# 檢查 Alice 日誌
tail -f /path/to/alice/alice.log | grep hooks

# 檢查 Hook 日誌
tail -f ~/.alice/hook.log

# 預期日誌
[hooks] Session test-123 from terminal logged (0 tools, 0ms)
```

## 🎨 Dashboard 功能

設置成功後，Alice Dashboard 將顯示：

### Timeline 頁面
- **來源過濾器**：可按 Terminal/VSCode/Telegram 篩選
- **來源標籤**：每個決策顯示來源圖示
  - 🖥️ CLI (Terminal)
  - 💜 VS (VSCode)
  - 📱 TG (Telegram)

### Dashboard 頁面
- **來源分布圖表**：顯示各來源使用占比
- **效能對比**：各來源的執行時間和成功率
- **使用趨勢**：過去 7 天的來源活動

## 🔍 故障排除

### Hook 沒有觸發
1. **檢查 Claude Code 版本**：確保支援 hooks
2. **檢查設定路徑**：`~/.claude/settings.json` 是否正確
3. **檢查權限**：hook script 是否有執行權限
4. **檢查網路**：能否連接到 `http://localhost:8082`

### Dashboard 沒有顯示 Hook 資料
1. **檢查 Alice 日誌**：是否有 hook 事件記錄
2. **檢查來源過濾**：Timeline 頁面是否選擇了正確的來源
3. **檢查 WebSocket**：瀏覽器控制台是否有連接錯誤

### 重複記錄問題
1. **Session ID 衝突**：檢查是否有多個 hook 同時運行
2. **快取問題**：重啟 Alice 清除快取
3. **去重邏輯**：Alice 自動處理，通常不需要手動干預

### 常見錯誤

**Error: Session token required**
- 這個錯誤表示使用了錯誤的 API 路徑
- 確保使用 `/api/hooks/claude-code` 而非其他端點

**Error: Connection refused**
- Alice 沒有運行或埠口不正確
- 檢查 Alice 是否在 `:8082` 埠運行

**Hook script permission denied**
- 設定檔中的 script 路徑沒有執行權限
- 執行 `chmod +x /path/to/hook-script.sh`

## 📚 進階配置

### 自訂 Hook URL
如果 Alice 運行在不同的埠或主機：

```json
{
  "hooks": {
    "enabled": true,
    "session_complete": "curl -X POST http://alice-server:8080/api/hooks/claude-code -H 'Content-Type: application/json' -d @-"
  }
}
```

### Hook 事件過濾
只記錄特定類型的事件：

```json
{
  "hooks": {
    "enabled": true,
    "session_complete": "~/.local/bin/claude-hook.sh",
    "session_active": false
  }
}
```

### 多專案配置
不同專案使用不同的 Alice 實例：

**專案 A** (`.claude/settings.json`)：
```json
{
  "hooks": {
    "session_complete": "curl -X POST http://alice-a:8082/api/hooks/claude-code -d @-"
  }
}
```

**專案 B** (`.claude/settings.json`)：
```json
{
  "hooks": {
    "session_complete": "curl -X POST http://alice-b:8082/api/hooks/claude-code -d @-"
  }
}
```

## 🎯 最佳實踐

### 1. 安全性
- 僅在可信網路中使用 HTTP hook
- 對於生產環境，考慮使用 HTTPS
- 定期檢查 hook 日誌避免異常流量

### 2. 效能
- 使用 `session_active` 事件進行輕量級追蹤
- 僅在需要完整記錄時啟用 `session_complete`
- 定期清理舊的 hook 日誌

### 3. 維護
- 定期檢查 Alice 和 Claude Code 的更新
- 備份重要的設定檔
- 監控 Dashboard 的磁碟空間使用

## 🔗 相關資源

- [Alice AI Agent GitHub](https://github.com/chimerakang/alice)
- [Claude Code 官方文件](https://code.claude.com/docs)
- [Issue #32: Claude Code Hooks 整合](https://github.com/chimerakang/alice/issues/32)

## 🎉 完成！

設置完成後，Alice 將能夠：
- 📊 統一追蹤所有 Claude AI 互動
- 🔄 實時更新 Dashboard 資料
- 📈 提供詳細的使用分析和趨勢
- 🎯 幫助你更好地理解 AI 開發工作流

如有問題，請查看故障排除部分或在 GitHub Issues 中回報。

---

*最後更新：2026-02-13*