package main

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// 這個文件複製了主專案中需要的結構體，用於獨立測試
// 避免與主專案的全域變數產生衝突

// TokenStats 結構體（複製自主專案）
type TokenStats struct {
	TotalInputTokens  int64
	TotalOutputTokens int64
	TotalCostUSD      float64
	APICallCount      int
}

// ToolExecution 結構體（複製自主專案）
type ToolExecution struct {
	Timestamp time.Time
	ToolName  string
	Input     map[string]interface{}
	Status    string
	Duration  time.Duration
	ChatID    int64
	ThreadID  int
	Error     string
}

// ExecutionOutcome 結構體（複製自主專案）
type ExecutionOutcome struct {
	Success      bool
	ErrorMessage string
	TaskType     string
	FilesChanged []string
	Summary      string
}

// DecisionLog 結構體（複製自主專案）
type DecisionLog struct {
	Timestamp     time.Time
	SessionID     string
	ProjectPath   string
	ChatID        int64
	ThreadID      int
	UserPrompt    string
	AgentResponse string
	ToolCalls     []ToolExecution
	Context       map[string]interface{}
	Outcome       ExecutionOutcome
	DurationMs    int
	TokensUsed    TokenStats
}

// PerformanceMetrics 結構體（複製自主專案）
type PerformanceMetrics struct {
	Timestamp         time.Time
	APICallLatency    time.Duration
	APICallSuccess    bool
	ToolExecutionTime time.Duration
	ToolExecutionType string
	TokensUsed        int
	EstimatedCost     float64
	MemoryUsage       uint64
	ErrorType         string
	ChatID            int64
	AgentType         string
}

// SecurityEvent 結構體（複製自主專案）
type SecurityEvent struct {
	ID          string
	Timestamp   time.Time
	EventType   string
	Severity    string
	Description string
	UserID      int64
	IP          string
	UserAgent   string
	Details     map[string]interface{}
	Mitigated   bool
}

func main() {
	fmt.Println("🧪 Testing SQLite Persistence Layer...")
	fmt.Println("=" + strings.Repeat("=", 50))

	// 使用臨時資料庫文件
	dbPath := "./test_alice_storage.db"
	defer func() {
		if err := os.Remove(dbPath); err != nil && !os.IsNotExist(err) {
			log.Printf("Warning: failed to cleanup test database: %v", err)
		}
	}()

	// 初始化存儲
	fmt.Println("\n1️⃣ Initializing SQLite storage...")
	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		log.Fatalf("❌ Failed to initialize storage: %v", err)
	}
	defer storage.Close()

	// 檢查健康狀態
	if err := storage.Health(); err != nil {
		log.Fatalf("❌ Storage health check failed: %v", err)
	}
	fmt.Println("✅ Storage initialized and healthy")

	// 測試 Tool Executions
	fmt.Println("\n2️⃣ Testing Tool Execution persistence...")
	testToolExecution := ToolExecution{
		Timestamp: time.Now(),
		ToolName:  "test_tool",
		Input:     map[string]interface{}{"file": "test.txt", "content": "Hello World"},
		Status:    "success",
		Duration:  500 * time.Millisecond,
		ChatID:    12345,
		ThreadID:  1,
	}

	if err := storage.InsertToolExecution(testToolExecution); err != nil {
		log.Printf("❌ Failed to insert tool execution: %v", err)
	} else {
		fmt.Println("✅ Tool execution inserted successfully")
	}

	// 查詢 Tool Executions
	executions, err := storage.GetToolExecutions(10, 0)
	if err != nil {
		log.Printf("❌ Failed to query tool executions: %v", err)
	} else {
		fmt.Printf("✅ Retrieved %d tool executions from database\n", len(executions))
		if len(executions) > 0 {
			fmt.Printf("   Latest: %s - %s (%s)\n", executions[0].ToolName, executions[0].Status, executions[0].Timestamp.Format("15:04:05"))
		}
	}

	// 測試 Decision Logs
	fmt.Println("\n3️⃣ Testing Decision Log persistence...")
	testDecision := DecisionLog{
		Timestamp:     time.Now(),
		SessionID:     "test_session_001",
		ProjectPath:   "/test/project",
		ChatID:        12345,
		ThreadID:      1,
		UserPrompt:    "Test the storage system",
		AgentResponse: "I will test the SQLite persistence layer",
		ToolCalls:     []ToolExecution{testToolExecution},
		Context:       map[string]interface{}{"test": true},
		Outcome: ExecutionOutcome{
			Success:  true,
			TaskType: "testing",
			Summary:  "Successfully tested persistence",
		},
		DurationMs: 1500,
		TokensUsed: TokenStats{
			TotalInputTokens:  100,
			TotalOutputTokens: 200,
			TotalCostUSD:      0.01,
			APICallCount:      1,
		},
	}

	if err := storage.InsertDecisionLog(testDecision); err != nil {
		log.Printf("❌ Failed to insert decision log: %v", err)
	} else {
		fmt.Println("✅ Decision log inserted successfully")
	}

	// 查詢 Decision Logs
	decisions, err := storage.GetDecisionLogs(10, 0)
	if err != nil {
		log.Printf("❌ Failed to query decision logs: %v", err)
	} else {
		fmt.Printf("✅ Retrieved %d decision logs from database\n", len(decisions))
		if len(decisions) > 0 {
			fmt.Printf("   Latest: %s tokens, outcome: %v (%s)\n",
				fmt.Sprintf("%d/%d", decisions[0].TokensUsed.TotalInputTokens, decisions[0].TokensUsed.TotalOutputTokens),
				decisions[0].Outcome.Success, decisions[0].Timestamp.Format("15:04:05"))
		}
	}

	// 測試 Performance Metrics
	fmt.Println("\n4️⃣ Testing Performance Metrics persistence...")
	testMetric := PerformanceMetrics{
		Timestamp:         time.Now(),
		APICallLatency:    250 * time.Millisecond,
		APICallSuccess:    true,
		ToolExecutionTime: 100 * time.Millisecond,
		ToolExecutionType: "read",
		TokensUsed:        150,
		EstimatedCost:     0.005,
		MemoryUsage:       1024 * 1024, // 1MB
		ChatID:            12345,
		AgentType:         "test",
	}

	if err := storage.InsertPerformanceMetric(testMetric); err != nil {
		log.Printf("❌ Failed to insert performance metric: %v", err)
	} else {
		fmt.Println("✅ Performance metric inserted successfully")
	}

	// 查詢 Performance Metrics
	metrics, err := storage.GetPerformanceMetrics(10, 0)
	if err != nil {
		log.Printf("❌ Failed to query performance metrics: %v", err)
	} else {
		fmt.Printf("✅ Retrieved %d performance metrics from database\n", len(metrics))
	}

	// 測試 Security Events
	fmt.Println("\n5️⃣ Testing Security Events persistence...")
	testEvent := SecurityEvent{
		ID:          "test_event_001",
		Timestamp:   time.Now(),
		EventType:   "test_event",
		Severity:    "low",
		Description: "Test security event for persistence testing",
		UserID:      12345,
		IP:          "127.0.0.1",
		UserAgent:   "test-agent/1.0",
		Details:     map[string]interface{}{"test": true, "action": "storage_test"},
		Mitigated:   false,
	}

	if err := storage.InsertSecurityEvent(testEvent); err != nil {
		log.Printf("❌ Failed to insert security event: %v", err)
	} else {
		fmt.Println("✅ Security event inserted successfully")
	}

	// 查詢 Security Events
	events, err := storage.GetSecurityEvents(10, 0)
	if err != nil {
		log.Printf("❌ Failed to query security events: %v", err)
	} else {
		fmt.Printf("✅ Retrieved %d security events from database\n", len(events))
		if len(events) > 0 {
			fmt.Printf("   Latest: %s (%s) - %s\n", events[0].EventType, events[0].Severity, events[0].Timestamp.Format("15:04:05"))
		}
	}

	// 測試時間範圍查詢
	fmt.Println("\n6️⃣ Testing time range queries...")
	now := time.Now()
	start := now.Add(-1 * time.Hour)
	end := now.Add(1 * time.Hour)

	execsByTime, err := storage.GetToolExecutionsByTimeRange(start, end, 10)
	if err != nil {
		log.Printf("❌ Failed to query tool executions by time range: %v", err)
	} else {
		fmt.Printf("✅ Retrieved %d tool executions in time range\n", len(execsByTime))
	}

	// 測試統計功能
	fmt.Println("\n7️⃣ Testing database statistics...")
	if sqliteStorage, ok := storage.(*SQLiteStorage); ok {
		stats, err := sqliteStorage.GetDatabaseStats()
		if err != nil {
			log.Printf("❌ Failed to get database stats: %v", err)
		} else {
			fmt.Println("✅ Database statistics:")
			for key, value := range stats {
				fmt.Printf("   %s: %v\n", key, value)
			}
		}
	}

	// 測試清理功能
	fmt.Println("\n8️⃣ Testing data cleanup...")
	if err := storage.CleanupOldData(0); err != nil { // 清理所有資料
		log.Printf("❌ Failed to cleanup data: %v", err)
	} else {
		fmt.Println("✅ Data cleanup completed")
	}

	// 確認清理後的狀態
	execsAfterCleanup, err := storage.GetToolExecutions(10, 0)
	if err != nil {
		log.Printf("❌ Failed to query after cleanup: %v", err)
	} else {
		fmt.Printf("✅ After cleanup: %d tool executions remain\n", len(execsAfterCleanup))
	}

	fmt.Println("\n🎉 Storage persistence testing completed successfully!")
	fmt.Println("=" + strings.Repeat("=", 50))
}

// NewSQLiteStorage - 為了測試複製的簡化版本
func NewSQLiteStorage(dbPath string) (*SQLiteStorage, error) {
	// 這裡會引用主專案的 storage.go 中的實作
	// 在真實測試中，我們需要確保能正確導入
	fmt.Printf("Note: This test requires the main storage.go implementation\n")
	fmt.Printf("Database path: %s\n", dbPath)

	// 返回 nil 來表示這是一個模擬測試
	// 在實際執行中，應該調用主專案的 NewSQLiteStorage 函數
	return nil, fmt.Errorf("this is a test template - implement actual NewSQLiteStorage call")
}

type SQLiteStorage struct {
	// 模擬結構 - 實際應該導入主專案的定義
}