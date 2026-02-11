# WebSocket 實時儀表板連接 - 完整實現文檔

Alice AI Agent 現已完全支援 WebSocket 實時事件推送，儀表板可以即時顯示真實的工具執行、決策完成、性能指標和安全事件。

## 🎯 解決的核心問題

**之前的問題**：儀表板使用假資料模擬（每 3-8 秒產生隨機數據），完全沒有連接到後端真實 API
**現在的解決方案**：完整的 WebSocket 實時事件推送系統，提供真實的即時數據流

## 🌐 WebSocket 架構

### 系統架構
```
Alice Backend ←→ WebSocketHub ←→ Multiple Dashboard Clients
     ↓               ↓                    ↓
   Events      Event Broadcasting    Real-time Updates
```

### 事件流向
```
Logger/Monitor → WebSocketHub.BroadcastEvent() → All Connected Clients
     ↓                      ↓                           ↓
Real-time data        JSON Serialization         Dashboard Updates
```

## 📡 支援的實時事件類型

| 事件類型 | 觸發時機 | 包含數據 |
|---------|---------|---------|
| **tool_execution_start** | 工具開始執行 | tool_name, chat_id, thread_id, timestamp |
| **tool_execution** | 工具執行完成 | tool_name, status, duration_ms, chat_id, error |
| **decision_complete** | AI 決策完成 | success, task_type, duration_ms, tokens, project_path |
| **performance_metric** | 性能指標記錄 | api_latency_ms, memory_usage, tool_type, tokens_used |
| **security_alert** | 安全事件發生 | severity, event_type, description, user_id, ip |
| **agent_status** | 代理狀態變更 | chat_id, status, details, timestamp |

## 🔧 技術實現

### 後端 WebSocket Hub (`websocket.go`)

**核心組件**：
- `WebSocketHub`: 管理所有 WebSocket 連接
- `WebSocketClient`: 代表單個客戶端連接
- `WebSocketEvent`: 標準化的事件結構

**主要功能**：
```go
type WebSocketHub struct {
    clients     map[*WebSocketClient]bool  // 活躍連接
    broadcast   chan []byte                // 廣播頻道
    register    chan *WebSocketClient      // 註冊新連接
    unregister  chan *WebSocketClient      // 註銷連接
    eventBuffer []WebSocketEvent           // 事件緩存（最近100個）
}
```

**事件廣播方法**：
- `BroadcastToolEvent(eventType, execution)` - 廣播工具事件
- `BroadcastDecisionEvent(decision)` - 廣播決策事件
- `BroadcastPerformanceEvent(metric)` - 廣播性能事件
- `BroadcastSecurityEvent(event)` - 廣播安全事件
- `BroadcastAgentStatusEvent(chatID, status, details)` - 廣播代理狀態

### 前端 WebSocket 客戶端 (`websocket-client.js`)

**核心功能**：
- **自動重連機制**: 指數退避算法，最多重試 10 次
- **心跳檢測**: 每 30 秒發送 ping，維持連接活躍
- **事件緩存**: 保留最近 100 個事件供查詢
- **連接狀態管理**: 實時顯示連接狀態

**重連策略**：
```javascript
reconnectInterval = Math.min(
    1000 * Math.pow(2, attempts - 1),  // 指數退避
    30000                               // 最大30秒間隔
);
```

**事件監聽**：
```javascript
wsClient.on('tool_execution', (event) => {
    updateToolsFeed(event.data);
});

wsClient.on('decision_complete', (event) => {
    updateDecisionMetrics(event.data);
});
```

## 🎨 儀表板整合

### 實時數據顯示

**工具執行動態**：
- 即時顯示工具開始/完成狀態
- 執行時間和結果狀態
- 按聊天 ID 分組顯示

**決策記錄更新**：
- 成功率即時計算
- 任務類型統計
- Token 使用量追蹤

**性能指標圖表**：
- API 延遲實時更新
- 記憶體使用量監控
- 工具使用統計動態更新

**安全事件警報**：
- 高嚴重性事件即時彈出通知
- 安全事件計數器實時更新
- 事件類型分類顯示

### UI 狀態指示器

**連接狀態顯示**：
```html
<!-- 實時連接狀態 -->
<span class="websocket-status">🟢 Live</span>  <!-- 連接中 -->
<span class="websocket-status">🔴 Disconnected</span>  <!-- 斷開 -->
```

**實時計數器**：
- 運行中的工具數量
- 活躍代理數量
- 決策成功率
- 安全警報數量

## 🚀 API 端點

### WebSocket 端點
- `WS /ws` - 主要 WebSocket 連接端點

### REST API 端點
- `GET /api/websocket/stats` - 獲取 WebSocket 統計資訊

**範例響應**：
```json
{
  "enabled": true,
  "connected_clients": 3,
  "event_buffer_size": 45,
  "max_buffer_size": 100
}
```

## 📊 事件範例

### 工具執行事件
```json
{
  "type": "tool_execution_start",
  "timestamp": "2024-01-15T10:30:00Z",
  "data": {
    "tool_name": "Read",
    "chat_id": 12345,
    "thread_id": 1,
    "timestamp": "2024-01-15T10:30:00Z"
  }
}

{
  "type": "tool_execution",
  "timestamp": "2024-01-15T10:30:01Z",
  "data": {
    "tool_name": "Read",
    "status": "success",
    "duration_ms": 150,
    "chat_id": 12345,
    "thread_id": 1,
    "error": ""
  }
}
```

### 決策完成事件
```json
{
  "type": "decision_complete",
  "timestamp": "2024-01-15T10:30:05Z",
  "data": {
    "session_id": "session_001",
    "project_path": "/path/to/project",
    "chat_id": 12345,
    "success": true,
    "task_type": "code_generation",
    "duration_ms": 2500,
    "tokens_input": 120,
    "tokens_output": 300
  }
}
```

### 性能指標事件
```json
{
  "type": "performance_metric",
  "timestamp": "2024-01-15T10:30:02Z",
  "data": {
    "api_latency_ms": 250,
    "api_success": true,
    "tool_execution_time": 100,
    "tool_execution_type": "read",
    "tokens_used": 150,
    "memory_usage": 1048576,
    "chat_id": 12345
  }
}
```

### 安全警報事件
```json
{
  "type": "security_alert",
  "timestamp": "2024-01-15T10:30:03Z",
  "data": {
    "event_id": "sec_001",
    "event_type": "rate_limit_exceeded",
    "severity": "medium",
    "description": "Client exceeded rate limit",
    "user_id": 12345,
    "ip": "192.168.1.100",
    "mitigated": false
  }
}
```

## ⚡ 性能特性

### 連接管理
- **並發連接**: 支援多個儀表板同時連接
- **內存效率**: 事件緩存限制為 100 個事件
- **優雅關閉**: 正確處理連接關閉和清理

### 事件處理
- **異步廣播**: 不阻塞主業務流程
- **批量發送**: 自動批量處理等待中的訊息
- **錯誤恢復**: 客戶端斷線不影響其他連接

### 安全考量
- **來源檢查**: 可配置的 CORS 來源驗證
- **連接限制**: 防止連接洪水攻擊
- **訊息大小限制**: 限制 WebSocket 訊息大小

## 🔧 配置和部署

### 初始化
```go
// 在 main.go 中初始化 WebSocket 系統
InitWebSocket()
log.Printf("   WebSocket real-time events: enabled")
```

### 前端引入
```html
<!-- 載入 WebSocket 客戶端 -->
<script src="js/websocket-client.js"></script>
<script src="js/dashboard.js"></script>
```

### Dashboard 連接
```javascript
// 在 AliceDashboard 中自動初始化
constructor() {
    this.wsClient = new AliceWebSocketClient();
    this.initializeWebSocket();
}
```

## 🧪 測試工具

### WebSocket 測試客戶端
建立了獨立的測試工具 `cmd/test_websocket/main.go`：

**功能**：
- 連接到 Alice WebSocket 伺服器
- 即時顯示所有事件類型
- 統計事件頻率和分佈
- 自動心跳檢測

**使用方式**：
```bash
# 編譯測試工具
cd cmd/test_websocket && go build -o ../../test-websocket .

# 測試 WebSocket 連接
./test-websocket ws://localhost:8080/ws
```

**測試輸出範例**：
```
🧪 Testing Alice WebSocket Real-time Events
==================================================
Connecting to: ws://localhost:8080/ws

📡 Attempting WebSocket connection...
✅ Connected to Alice WebSocket server!
🎧 Listening for real-time events...

🔧 [10:30:00] Tool started: Read (Chat: 12345)
✅ [10:30:01] Tool completed: Read -> success (Chat: 12345, Duration: 150ms)
✅ [10:30:05] Decision: code_generation -> SUCCESS (Duration: 2500ms)
📊 [10:30:02] Performance: read (API: 250ms, Memory: 1048576 bytes)
```

## 📈 監控和統計

### 連接統計
- 當前連接數
- 事件緩存大小
- 重連次數統計

### 事件統計
- 各類型事件數量
- 事件頻率（每分鐘）
- 錯誤率統計

### 性能監控
- WebSocket 延遲
- 訊息佇列大小
- 記憶體使用量

## 🔄 與 SQLite 持久化整合

WebSocket 與 SQLite 持久化完美結合：

**即時 + 歷史**：
- WebSocket 提供即時事件流
- SQLite 提供歷史數據查詢
- 儀表板同時顯示即時和歷史數據

**數據一致性**：
- 所有事件同時寫入 WebSocket 和 SQLite
- 確保即時顯示與歷史記錄一致
- 斷線重連時從歷史數據補充

## 🎉 完成的 Acceptance Criteria

- [x] **Dashboard 顯示真實的即時 tool 執行資料** - 完全實現
- [x] **Agent 狀態即時更新** - 完全實現
- [x] **效能指標即時刷新** - 完全實現
- [x] **斷線自動重連** - 指數退避重連機制
- [x] **移除所有 mock/simulated data** - charts.js 假資料已移除

## 🚀 技術優勢

### 即時性 (Real-time)
- **零延遲事件推送**: 事件發生立即推送到所有連接的客戶端
- **真實數據流**: 完全基於實際系統事件，無模擬數據

### 可靠性 (Reliability)
- **自動重連**: 智慧重連機制，網路中斷後自動恢復
- **事件緩存**: 新連接自動接收最近事件，避免數據丟失
- **優雅降級**: WebSocket 故障時儀表板仍可使用 REST API

### 擴展性 (Scalability)
- **多客戶端支援**: 支援多個儀表板同時連接
- **事件類型擴展**: 易於添加新的事件類型
- **效能最佳化**: 異步處理，不影響主系統性能

### 用戶體驗 (UX)
- **即時回饋**: 操作結果立即可見
- **視覺狀態指示**: 清楚的連接狀態顯示
- **流暢動畫**: 實時數據更新動畫效果

Alice 現在擁有完整的實時數據推送能力，提供enterprise級的監控體驗！ 🌟

---

*文件版本：v1.0 | 最後更新：2024-01-15*