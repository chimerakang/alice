# SQLite 持久化層 - 完整實現文檔

Alice AI Agent 現已完全支援 SQLite 持久化，所有運行時數據都可以持久保存，程式重啟後歷史資料完整保留。

## 🎯 解決的核心問題

**之前的問題**：所有數據都存在記憶體中的 ring buffer，程式重啟後全部消失
**現在的解決方案**：完整的 SQLite 資料庫持久化，支援歷史查詢、趨勢分析、審計追蹤

## 📊 持久化的數據類型

| 數據類型 | 之前 | 現在 | 支援功能 |
|---------|------|------|---------|
| **Tool Executions** | 記憶體 100 筆 | SQLite 無限制 | 時間範圍查詢、按聊天ID查詢、分頁 |
| **Decision Logs** | 記憶體 50 筆 | SQLite 無限制 | 按專案查詢、時間範圍、完整審計跟蹤 |
| **Performance Metrics** | 記憶體 10,000 筆 | SQLite 無限制 | 性能分析、趨勢圖表、統計報告 |
| **Security Events** | 記憶體 1,000 筆 | SQLite 無限制 | 按嚴重性查詢、安全審計、事件關聯 |

## 🏗️ 資料庫架構

### Table: tool_executions
```sql
CREATE TABLE tool_executions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp DATETIME NOT NULL,
    tool_name TEXT NOT NULL,
    input_json TEXT,
    status TEXT NOT NULL,           -- "running", "success", "error"
    duration_ms INTEGER,
    chat_id INTEGER,
    thread_id INTEGER,
    error TEXT,
    git_commit_hash TEXT,           -- Git 整合支援
    git_branch TEXT,                -- Git 整合支援
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

**索引**：timestamp, chat_id, tool_name

### Table: decision_logs
```sql
CREATE TABLE decision_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp DATETIME NOT NULL,
    session_id TEXT,
    project_path TEXT,
    chat_id INTEGER,
    thread_id INTEGER,
    user_prompt TEXT,
    agent_response TEXT,
    tool_calls_json TEXT,           -- JSON array of ToolExecution
    context_json TEXT,              -- Additional context data
    outcome_json TEXT,              -- ExecutionOutcome with success/failure
    duration_ms INTEGER,
    tokens_input INTEGER,
    tokens_output INTEGER,
    tokens_total INTEGER,
    cost_usd REAL,
    git_commit_hash TEXT,
    git_branch TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

**索引**：timestamp, session_id, project_path, chat_id

### Table: performance_metrics
```sql
CREATE TABLE performance_metrics (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp DATETIME NOT NULL,
    api_call_latency_ms INTEGER,
    api_call_success BOOLEAN,
    tool_execution_time_ms INTEGER,
    tool_execution_type TEXT,
    tokens_used INTEGER,
    estimated_cost REAL,
    memory_usage BIGINT,
    error_type TEXT,
    chat_id INTEGER,
    agent_type TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

**索引**：timestamp, chat_id, tool_execution_type

### Table: security_events
```sql
CREATE TABLE security_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    event_id TEXT UNIQUE,
    timestamp DATETIME NOT NULL,
    event_type TEXT NOT NULL,
    severity TEXT NOT NULL,          -- "low", "medium", "high", "critical"
    description TEXT,
    user_id INTEGER,
    ip_address TEXT,
    user_agent TEXT,
    details_json TEXT,               -- Additional event details as JSON
    mitigated BOOLEAN DEFAULT FALSE,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

**索引**：timestamp, event_type, severity, user_id

## ⚙️ 配置選項

在 `config.json` 中新增的配置項：

```json
{
  "enable_persistence": true,              // 啟用持久化（預設：true）
  "database_path": "./data/alice.db",     // 資料庫檔案路徑
  "data_retention_days": 30,              // 資料保留天數
  "enable_data_cleanup": true             // 啟用自動清理
}
```

環境變數支援：
```bash
ALICE_ENABLE_PERSISTENCE=true
ALICE_DATABASE_PATH=/path/to/alice.db
ALICE_DATA_RETENTION_DAYS=30
ALICE_ENABLE_DATA_CLEANUP=true
```

## 🔄 雙寫機制

所有 logger 都已升級為**雙寫機制**：

1. **即時記憶體寫入**：保持現有的快速查詢性能
2. **異步資料庫寫入**：使用 goroutine 不阻塞主流程

```go
// 範例：ToolLogger.LogToolComplete()
if globalStorage != nil && completedExecution != nil {
    go func() {
        if err := globalStorage.InsertToolExecution(*completedExecution); err != nil {
            log.Printf("Warning: failed to persist tool execution: %v", err)
        }
    }()
}
```

**錯誤處理策略**：資料庫寫入失敗不影響主要功能，只記錄警告日誌

## 🌐 Web API 增強

### 現有 API 端點增強
所有現有 API 端點都已支援歷史查詢參數：

#### GET /api/tools/executions
**新增查詢參數**：
- `limit` - 限制結果數量（預設：100）
- `offset` - 分頁偏移（預設：0）
- `chat_id` - 按聊天ID過濾
- `start_time` - 開始時間（RFC3339格式）
- `end_time` - 結束時間（RFC3339格式）
- `source` - 資料來源（"memory" 或 "database"）

**範例**：
```bash
# 查詢最近100個工具執行
GET /api/tools/executions?limit=100

# 查詢特定聊天的工具執行
GET /api/tools/executions?chat_id=12345&limit=50

# 查詢時間範圍內的執行記錄
GET /api/tools/executions?start_time=2024-01-01T00:00:00Z&end_time=2024-01-31T23:59:59Z

# 只查詢記憶體數據（向後兼容）
GET /api/tools/executions?source=memory
```

#### GET /api/decisions
**新增查詢參數**：
- `limit`, `offset` - 分頁控制
- `project_path` - 按專案路徑過濾
- `start_time`, `end_time` - 時間範圍
- `source` - 資料來源選擇

#### GET /api/performance/metrics
**新增查詢參數**：
- `limit`, `offset` - 分頁控制
- `start_time`, `end_time` - 時間範圍
- `source` - 資料來源選擇

#### GET /api/security/events
**新增查詢參數**：
- `limit`, `offset` - 分頁控制
- `severity` - 按嚴重性過濾（"low", "medium", "high", "critical"）
- `start_time`, `end_time` - 時間範圍
- `source` - 資料來源選擇

### 新增 Storage Management API 端點

#### GET /api/storage/health
檢查儲存系統健康狀態
```json
{
  "healthy": true,
  "persistence": true,
  "timestamp": "2024-01-15T10:30:00Z"
}
```

#### GET /api/storage/stats
獲取資料庫統計資訊
```json
{
  "persistence": true,
  "database_stats": {
    "tool_executions_count": 1250,
    "decision_logs_count": 89,
    "performance_metrics_count": 5430,
    "security_events_count": 12,
    "database_size_bytes": 2048576,
    "earliest_record": "2024-01-01T00:00:00Z",
    "latest_record": "2024-01-15T10:30:00Z"
  },
  "timestamp": "2024-01-15T10:30:00Z"
}
```

#### POST /api/storage/cleanup
手動觸發資料清理
```json
// Request
{
  "retention_days": 30
}

// Response
{
  "success": true,
  "retention_days": 30,
  "duration_ms": 145,
  "timestamp": "2024-01-15T10:30:00Z"
}
```

## 🗄️ Storage Interface

完整的 Storage 介面支援所有持久化操作：

```go
type Storage interface {
    // Tool Executions
    InsertToolExecution(exec ToolExecution) error
    GetToolExecutions(limit int, offset int) ([]ToolExecution, error)
    GetToolExecutionsByTimeRange(start, end time.Time, limit int) ([]ToolExecution, error)
    GetToolExecutionsByChat(chatID int64, limit int) ([]ToolExecution, error)

    // Decision Logs
    InsertDecisionLog(log DecisionLog) error
    GetDecisionLogs(limit int, offset int) ([]DecisionLog, error)
    GetDecisionLogsByTimeRange(start, end time.Time, limit int) ([]DecisionLog, error)
    GetDecisionLogsByProject(projectPath string, limit int) ([]DecisionLog, error)

    // Performance Metrics
    InsertPerformanceMetric(metric PerformanceMetrics) error
    GetPerformanceMetrics(limit int, offset int) ([]PerformanceMetrics, error)
    GetPerformanceMetricsByTimeRange(start, end time.Time, limit int) ([]PerformanceMetrics, error)
    GetPerformanceAnalytics(hours int) (PerformanceAnalytics, error)

    // Security Events
    InsertSecurityEvent(event SecurityEvent) error
    GetSecurityEvents(limit int, offset int) ([]SecurityEvent, error)
    GetSecurityEventsByTimeRange(start, end time.Time, limit int) ([]SecurityEvent, error)
    GetSecurityEventsBySeverity(severity string, limit int) ([]SecurityEvent, error)

    // Data Management
    CleanupOldData(retentionDays int) error
    Close() error
    Health() error
}
```

## 🧹 自動資料清理

### 定期清理機制
- **頻率**：每日執行一次
- **清理策略**：刪除超過保留期限的記錄
- **VACUUM 操作**：清理後自動回收資料庫空間

### 清理日誌範例
```
2024/01/15 03:00:00 Cleaned up 150 old records from tool_executions
2024/01/15 03:00:00 Cleaned up 25 old records from decision_logs
2024/01/15 03:00:00 Cleaned up 800 old records from performance_metrics
2024/01/15 03:00:00 Data cleanup completed: removed 975 total records older than 30 days
```

## 🔒 安全考量

### 資料庫安全性
- **權限控制**：資料庫檔案權限設為 `0644`
- **連接安全**：本地檔案連接，不暴露網路端口
- **Pure Go 驅動**：使用 `modernc.org/sqlite`，無 CGO 依賴

### PII 資料處理
- User prompts 和 agent responses 會完整保存
- IP 位址和 User Agent 保存用於安全審計
- 敏感資料在 JSON 欄位中可選擇性保存

## 📈 效能影響

### 寫入效能
- **異步寫入**：不影響主要業務流程
- **批量處理**：可選的批量插入機制
- **WAL 模式**：提升並發寫入性能

### 查詢效能
- **雙重緩存**：記憶體 + 資料庫
- **智慧回退**：資料庫查詢失敗時自動使用記憶體數據
- **索引優化**：針對常見查詢模式建立索引

### 資源使用
- **磁碟空間**：依據數據量線性增長
- **記憶體影響**：極小（只有連接池）
- **CPU 開銷**：異步寫入，最小化主線程影響

## 🚀 使用範例

### 1. 基本設定
```json
// config.json
{
  "enable_persistence": true,
  "database_path": "./data/alice.db",
  "data_retention_days": 30
}
```

### 2. 程式啟動日誌
```
🚀 Starting Claude TG Agent (CLI mode)
   Persistence: enabled (SQLite at ./data/alice.db)
   Data retention: 30 days
   Data cleanup: scheduled daily (retention: 30 days)
```

### 3. API 查詢範例
```bash
# 查詢最近24小時的決策記錄
curl "http://localhost:8080/api/decisions?start_time=2024-01-14T10:00:00Z&end_time=2024-01-15T10:00:00Z&limit=100"

# 查詢特定聊天的工具執行記錄
curl "http://localhost:8080/api/tools/executions?chat_id=123456&limit=50"

# 檢查資料庫健康狀態
curl "http://localhost:8080/api/storage/health"

# 手動觸發資料清理
curl -X POST "http://localhost:8080/api/storage/cleanup" \
  -H "Content-Type: application/json" \
  -d '{"retention_days": 7}'
```

### 4. 程式碼中的使用
```go
// 插入工具執行記錄（自動完成）
globalToolLogger.LogToolComplete("Read", "success", 150*time.Millisecond, nil)

// 查詢歷史記錄
if globalStorage != nil {
    executions, err := globalStorage.GetToolExecutionsByTimeRange(
        time.Now().Add(-24*time.Hour),
        time.Now(),
        100,
    )
    // 處理結果...
}
```

## 🔧 故障排除

### 常見問題

#### 1. 資料庫檔案權限錯誤
```bash
# 檢查檔案權限
ls -la data/alice.db

# 修復權限
chmod 644 data/alice.db
chown $USER:$GROUP data/alice.db
```

#### 2. 磁碟空間不足
```bash
# 檢查資料庫大小
du -h data/alice.db

# 手動清理舊資料
curl -X POST "http://localhost:8080/api/storage/cleanup" \
  -H "Content-Type: application/json" \
  -d '{"retention_days": 7}'
```

#### 3. 查詢效能問題
- 檢查是否有過大的時間範圍查詢
- 使用 `limit` 參數限制結果數量
- 考慮增加特定欄位的索引

#### 4. 資料庫鎖定問題
```bash
# 檢查是否有其他程序使用資料庫
lsof data/alice.db

# 重啟 Alice Agent
kill -TERM <alice-pid>
./claude-tg-agent
```

## 📋 檢查清單

部署前確認：

- [ ] `enable_persistence` 設為 true
- [ ] `database_path` 目錄存在且可寫入
- [ ] `data_retention_days` 設置合理值
- [ ] 確認磁碟空間充足
- [ ] 測試資料庫健康端點：`GET /api/storage/health`
- [ ] 確認自動清理正常運作

## 🎉 成果總結

✅ **完全解決持久化問題**：程式重啟不再丟失歷史資料
✅ **零停機升級**：向後相容，現有功能不受影響
✅ **豐富的查詢功能**：時間範圍、分頁、過濾、統計
✅ **自動資料管理**：定期清理、空間回收、健康監控
✅ **高效能設計**：異步寫入、智慧回退、索引優化
✅ **完整的 API 支援**：RESTful 介面、詳細文檔、範例程式碼

Alice 現在擁有enterprise級的資料持久化能力！ 🚀

---

*文件版本：v1.0 | 最後更新：2024-01-15*