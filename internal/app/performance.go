package app

import (
	"encoding/json"
	"fmt"
	"log"
	"runtime"
	"sync"
	"time"
)

// PerformanceMetrics 效能指標結構
type PerformanceMetrics struct {
	Timestamp         time.Time      `json:"timestamp"`
	APICallLatency    time.Duration  `json:"api_call_latency"`
	APICallSuccess    bool           `json:"api_call_success"`
	ToolExecutionTime time.Duration  `json:"tool_execution_time"`
	ToolExecutionType string         `json:"tool_execution_type"`
	TokensUsed        int            `json:"tokens_used"`
	EstimatedCost     float64        `json:"estimated_cost"`
	MemoryUsage       uint64         `json:"memory_usage"`
	ErrorType         string         `json:"error_type,omitempty"`
	ChatID            int64          `json:"chat_id"`
	AgentType         string         `json:"agent_type,omitempty"`
}

// PerformanceAnalytics 效能分析數據
type PerformanceAnalytics struct {
	TotalRequests      int64         `json:"total_requests"`
	SuccessRate        float64       `json:"success_rate"`
	AvgAPILatency      time.Duration `json:"avg_api_latency"`
	AvgToolExecution   time.Duration `json:"avg_tool_execution"`
	TotalTokens        int64         `json:"total_tokens"`
	TotalCost          float64       `json:"total_cost"`
	ErrorsByType       map[string]int64 `json:"errors_by_type"`
	ToolUsageStats     map[string]int64 `json:"tool_usage_stats"`
	PeakMemoryUsage    uint64        `json:"peak_memory_usage"`
	CurrentMemoryUsage uint64        `json:"current_memory_usage"`
	UptimeSeconds      int64         `json:"uptime_seconds"`
	RequestsPerHour    float64       `json:"requests_per_hour"`
	CostPerRequest     float64       `json:"cost_per_request"`
}

// PerformanceRecommendation 效能最佳化建議
type PerformanceRecommendation struct {
	Type        string    `json:"type"`
	Priority    string    `json:"priority"` // high, medium, low
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Impact      string    `json:"impact"`
	Timestamp   time.Time `json:"timestamp"`
}

// PerformanceMonitor 效能監控器
type PerformanceMonitor struct {
	mu                sync.RWMutex
	metrics           []PerformanceMetrics
	maxMetricsHistory int
	startTime         time.Time

	// 聚合統計
	totalRequests     int64
	successfulCalls   int64
	totalAPILatency   time.Duration
	totalToolTime     time.Duration
	totalTokens       int64
	totalCost         float64
	errorCounts       map[string]int64
	toolUsage         map[string]int64
	peakMemory        uint64
}

// NewPerformanceMonitor 建立新的效能監控器
func NewPerformanceMonitor() *PerformanceMonitor {
	return &PerformanceMonitor{
		metrics:           make([]PerformanceMetrics, 0),
		maxMetricsHistory: 10000, // 保留最近 10,000 個記錄
		startTime:         time.Now(),
		errorCounts:       make(map[string]int64),
		toolUsage:         make(map[string]int64),
	}
}

// RecordMetric 記錄效能指標
func (pm *PerformanceMonitor) RecordMetric(metric PerformanceMetrics) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// 記錄時間戳
	metric.Timestamp = time.Now()

	// 獲取記憶體使用量
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	metric.MemoryUsage = m.Alloc

	// 更新峰值記憶體
	if metric.MemoryUsage > pm.peakMemory {
		pm.peakMemory = metric.MemoryUsage
	}

	// 添加到歷史記錄
	pm.metrics = append(pm.metrics, metric)

	// 限制歷史記錄數量
	if len(pm.metrics) > pm.maxMetricsHistory {
		pm.metrics = pm.metrics[1:]
	}

	// 更新聚合統計
	pm.updateAggregateStats(metric)

	// 如果有 SQLite 儲存，將效能指標寫入資料庫
	if globalStorage != nil {
		go func() {
			if err := globalStorage.InsertPerformanceMetric(metric); err != nil {
				log.Printf("Warning: failed to persist performance metric to database: %v", err)
			}
		}()
	}

	// 廣播性能指標事件到 WebSocket 客戶端
	BroadcastPerformanceEvent(metric)
}

// updateAggregateStats 更新聚合統計
func (pm *PerformanceMonitor) updateAggregateStats(metric PerformanceMetrics) {
	pm.totalRequests++

	if metric.APICallSuccess {
		pm.successfulCalls++
	}

	pm.totalAPILatency += metric.APICallLatency
	pm.totalToolTime += metric.ToolExecutionTime
	pm.totalTokens += int64(metric.TokensUsed)
	pm.totalCost += metric.EstimatedCost

	if metric.ErrorType != "" {
		pm.errorCounts[metric.ErrorType]++
	}

	if metric.ToolExecutionType != "" {
		pm.toolUsage[metric.ToolExecutionType]++
	}
}

// GetAnalytics 獲取效能分析數據
func (pm *PerformanceMonitor) GetAnalytics() PerformanceAnalytics {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	// 優先從資料庫獲取統計數據（過去24小時）
	if globalStorage != nil {
		if dbAnalytics, err := globalStorage.GetPerformanceAnalytics(24); err == nil {
			// 補充記憶體相關資訊（資料庫中沒有的）
			var currentMemory uint64
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			currentMemory = m.Alloc

			uptime := time.Since(pm.startTime)

			dbAnalytics.CurrentMemoryUsage = currentMemory
			dbAnalytics.UptimeSeconds = int64(uptime.Seconds())

			// 如果資料庫中沒有錯誤統計，使用記憶體中的
			if len(dbAnalytics.ErrorsByType) == 0 {
				dbAnalytics.ErrorsByType = pm.copyErrorCounts()
			}
			if len(dbAnalytics.ToolUsageStats) == 0 {
				dbAnalytics.ToolUsageStats = pm.copyToolUsage()
			}

			// 確保峰值記憶體不為0（取較大值）
			if pm.peakMemory > dbAnalytics.PeakMemoryUsage {
				dbAnalytics.PeakMemoryUsage = pm.peakMemory
			}

			return dbAnalytics
		}
	}

	// 如果資料庫不可用，回退到記憶體統計
	uptime := time.Since(pm.startTime)

	var avgAPILatency, avgToolExecution time.Duration
	if pm.totalRequests > 0 {
		avgAPILatency = pm.totalAPILatency / time.Duration(pm.totalRequests)
		avgToolExecution = pm.totalToolTime / time.Duration(pm.totalRequests)
	}

	successRate := 0.0
	if pm.totalRequests > 0 {
		successRate = float64(pm.successfulCalls) / float64(pm.totalRequests) * 100
	}

	requestsPerHour := 0.0
	if uptime.Hours() > 0 {
		requestsPerHour = float64(pm.totalRequests) / uptime.Hours()
	}

	costPerRequest := 0.0
	if pm.totalRequests > 0 {
		costPerRequest = pm.totalCost / float64(pm.totalRequests)
	}

	var currentMemory uint64
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	currentMemory = m.Alloc

	return PerformanceAnalytics{
		TotalRequests:      pm.totalRequests,
		SuccessRate:        successRate,
		AvgAPILatency:      avgAPILatency,
		AvgToolExecution:   avgToolExecution,
		TotalTokens:        pm.totalTokens,
		TotalCost:          pm.totalCost,
		ErrorsByType:       pm.copyErrorCounts(),
		ToolUsageStats:     pm.copyToolUsage(),
		PeakMemoryUsage:    pm.peakMemory,
		CurrentMemoryUsage: currentMemory,
		UptimeSeconds:      int64(uptime.Seconds()),
		RequestsPerHour:    requestsPerHour,
		CostPerRequest:     costPerRequest,
	}
}

// GetRecentMetrics 獲取最近的效能指標
func (pm *PerformanceMonitor) GetRecentMetrics(limit int) []PerformanceMetrics {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if limit <= 0 || limit > len(pm.metrics) {
		limit = len(pm.metrics)
	}

	start := len(pm.metrics) - limit
	result := make([]PerformanceMetrics, limit)
	copy(result, pm.metrics[start:])

	return result
}

// GetPerformanceTrends 獲取效能趨勢數據
func (pm *PerformanceMonitor) GetPerformanceTrends(hours int) map[string]interface{} {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	cutoff := time.Now().Add(-time.Duration(hours) * time.Hour)

	var recentMetrics []PerformanceMetrics
	for _, metric := range pm.metrics {
		if metric.Timestamp.After(cutoff) {
			recentMetrics = append(recentMetrics, metric)
		}
	}

	if len(recentMetrics) == 0 {
		return map[string]interface{}{
			"period_hours": hours,
			"data_points": 0,
			"message": "No data available for the specified period",
		}
	}

	// 計算趨勢數據
	totalLatency := time.Duration(0)
	totalTokens := 0
	totalCost := 0.0
	successful := 0

	hourlyStats := make(map[int]map[string]interface{})

	for _, metric := range recentMetrics {
		totalLatency += metric.APICallLatency
		totalTokens += metric.TokensUsed
		totalCost += metric.EstimatedCost
		if metric.APICallSuccess {
			successful++
		}

		hour := metric.Timestamp.Hour()
		if hourlyStats[hour] == nil {
			hourlyStats[hour] = map[string]interface{}{
				"requests": 0,
				"latency_sum": time.Duration(0),
				"tokens": 0,
				"cost": 0.0,
				"errors": 0,
			}
		}

		stats := hourlyStats[hour]
		stats["requests"] = stats["requests"].(int) + 1
		stats["latency_sum"] = stats["latency_sum"].(time.Duration) + metric.APICallLatency
		stats["tokens"] = stats["tokens"].(int) + metric.TokensUsed
		stats["cost"] = stats["cost"].(float64) + metric.EstimatedCost
		if !metric.APICallSuccess || metric.ErrorType != "" {
			stats["errors"] = stats["errors"].(int) + 1
		}
	}

	return map[string]interface{}{
		"period_hours":     hours,
		"data_points":      len(recentMetrics),
		"avg_latency_ms":   float64(totalLatency.Nanoseconds()) / float64(len(recentMetrics)) / 1e6,
		"total_tokens":     totalTokens,
		"total_cost":       totalCost,
		"success_rate":     float64(successful) / float64(len(recentMetrics)) * 100,
		"hourly_breakdown": hourlyStats,
	}
}

// GenerateRecommendations 生成效能最佳化建議
func (pm *PerformanceMonitor) GenerateRecommendations() []PerformanceRecommendation {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	var recommendations []PerformanceRecommendation
	analytics := pm.GetAnalytics()

	// 檢查 API 成功率
	if analytics.SuccessRate < 95.0 {
		recommendations = append(recommendations, PerformanceRecommendation{
			Type:        "reliability",
			Priority:    "high",
			Title:       "低成功率警告",
			Description: "API 呼叫成功率低於 95%，建議檢查錯誤日誌和網路連線",
			Impact:      "用戶體驗下降，可能導致功能失效",
			Timestamp:   time.Now(),
		})
	}

	// 檢查平均延遲
	if analytics.AvgAPILatency > 5*time.Second {
		recommendations = append(recommendations, PerformanceRecommendation{
			Type:        "performance",
			Priority:    "medium",
			Title:       "API 延遲過高",
			Description: "平均 API 延遲超過 5 秒，考慮最佳化模型或增加快取",
			Impact:      "回應時間慢，影響用戶體驗",
			Timestamp:   time.Now(),
		})
	}

	// 檢查成本效率
	if analytics.CostPerRequest > 0.05 {
		recommendations = append(recommendations, PerformanceRecommendation{
			Type:        "cost",
			Priority:    "medium",
			Title:       "成本偏高",
			Description: "每個請求成本超過 $0.05，建議最佳化 prompt 長度或使用更經濟的模型",
			Impact:      "運營成本增加",
			Timestamp:   time.Now(),
		})
	}

	// 檢查記憶體使用
	if analytics.CurrentMemoryUsage > 500*1024*1024 { // 500MB
		recommendations = append(recommendations, PerformanceRecommendation{
			Type:        "resource",
			Priority:    "medium",
			Title:       "記憶體使用偏高",
			Description: "當前記憶體使用超過 500MB，考慮清理歷史記錄或重啟服務",
			Impact:      "系統效能下降，可能導致崩潰",
			Timestamp:   time.Now(),
		})
	}

	// 檢查工具使用模式
	mostUsedTool := ""
	maxUsage := int64(0)
	for tool, count := range analytics.ToolUsageStats {
		if count > maxUsage {
			maxUsage = count
			mostUsedTool = tool
		}
	}

	if mostUsedTool != "" && maxUsage > analytics.TotalRequests/2 {
		recommendations = append(recommendations, PerformanceRecommendation{
			Type:        "optimization",
			Priority:    "low",
			Title:       "工具使用不平衡",
			Description: fmt.Sprintf("工具 '%s' 使用率過高 (%d%%），考慮最佳化工作流程", mostUsedTool, maxUsage*100/analytics.TotalRequests),
			Impact:      "可能存在更高效的解決方案",
			Timestamp:   time.Now(),
		})
	}

	return recommendations
}

// copyErrorCounts 複製錯誤計數 (thread-safe)
func (pm *PerformanceMonitor) copyErrorCounts() map[string]int64 {
	result := make(map[string]int64)
	for k, v := range pm.errorCounts {
		result[k] = v
	}
	return result
}

// copyToolUsage 複製工具使用統計 (thread-safe)
func (pm *PerformanceMonitor) copyToolUsage() map[string]int64 {
	result := make(map[string]int64)
	for k, v := range pm.toolUsage {
		result[k] = v
	}
	return result
}

// ExportMetrics 導出效能數據為 JSON
func (pm *PerformanceMonitor) ExportMetrics() ([]byte, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	data := map[string]interface{}{
		"export_timestamp": time.Now(),
		"analytics":        pm.GetAnalytics(),
		"recommendations":  pm.GenerateRecommendations(),
		"recent_metrics":   pm.GetRecentMetrics(100), // 最近 100 個記錄
		"trends_24h":       pm.GetPerformanceTrends(24),
	}

	return json.MarshalIndent(data, "", "  ")
}

// 全域效能監控器實例
var performanceMonitor *PerformanceMonitor

// InitPerformanceMonitor 初始化效能監控器
func InitPerformanceMonitor() {
	performanceMonitor = NewPerformanceMonitor()
}

// GetUptimeSeconds returns the uptime in seconds from the performance monitor
func GetUptimeSeconds() int64 {
	if performanceMonitor == nil {
		return 0
	}
	performanceMonitor.mu.RLock()
	defer performanceMonitor.mu.RUnlock()
	return int64(time.Since(performanceMonitor.startTime).Seconds())
}

// RecordAPICall 記錄 API 呼叫效能
func RecordAPICall(latency time.Duration, success bool, tokensUsed int, cost float64, chatID int64, errorType string) {
	if performanceMonitor != nil {
		metric := PerformanceMetrics{
			APICallLatency: latency,
			APICallSuccess: success,
			TokensUsed:     tokensUsed,
			EstimatedCost:  cost,
			ChatID:         chatID,
			ErrorType:      errorType,
		}
		performanceMonitor.RecordMetric(metric)
	}
}

// RecordToolExecution 記錄工具執行效能
func RecordToolExecution(toolType string, executionTime time.Duration, chatID int64, success bool) {
	if performanceMonitor != nil {
		metric := PerformanceMetrics{
			ToolExecutionTime: executionTime,
			ToolExecutionType: toolType,
			APICallSuccess:    success, // 重複使用此欄位表示工具執行成功
			ChatID:           chatID,
		}
		performanceMonitor.RecordMetric(metric)
	}
}