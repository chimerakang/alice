package app

import (
	"encoding/json"
	"fmt"
	"log"
	"runtime"
	"strings"
	"sync"
	"time"
)

// ModelPricing 模型費率表（每百萬 tokens 美元）
// 從 config.json 讀取，通過 InitModelPricing() 初始化
var ModelPricing = map[string]struct {
	InputPerMTok  float64
	OutputPerMTok float64
}{
	"haiku":     {1.00, 5.00},   // Default: Claude Haiku 4.5
	"sonnet":    {3.00, 15.00},  // Default: Claude Sonnet 4.6
	"opus":      {5.00, 25.00},  // Default: Claude Opus 4.7 / 4.8 (standard tier)
	"opus_fast": {10.00, 50.00}, // Default: Claude Opus 4.8 Fast Mode (research preview; CLI headless flag pending — see #178)
}

// InitModelPricing 從 config 初始化模型費率
func InitModelPricing(config *ModelPricingConfig) {
	if config == nil {
		return
	}
	if config.Haiku.Input > 0 {
		ModelPricing["haiku"] = struct {
			InputPerMTok  float64
			OutputPerMTok float64
		}{config.Haiku.Input, config.Haiku.Output}
	}
	if config.Sonnet.Input > 0 {
		ModelPricing["sonnet"] = struct {
			InputPerMTok  float64
			OutputPerMTok float64
		}{config.Sonnet.Input, config.Sonnet.Output}
	}
	if config.Opus.Input > 0 {
		ModelPricing["opus"] = struct {
			InputPerMTok  float64
			OutputPerMTok float64
		}{config.Opus.Input, config.Opus.Output}
	}
	if config.OpusFast.Input > 0 {
		ModelPricing["opus_fast"] = struct {
			InputPerMTok  float64
			OutputPerMTok float64
		}{config.OpusFast.Input, config.OpusFast.Output}
	}
}

// EstimateClaudeCost computes a USD cost estimate for a single Claude CLI call
// from its usage token breakdown. Used as a fallback when the CLI returns
// total_cost_usd=0 (typical under Max subscription); also used for Hermes
// summary when no authoritative cost is available.
//
// Honours Anthropic prompt-cache pricing:
//   - uncached input        × 1.00x base rate
//   - cache_read_input      × 0.10x base rate
//   - cache_creation_input  × 1.25x base rate
//   - output                ×       output rate
//
// Returns 0 when the model cannot be matched against the pricing table; the
// caller should treat that as "unknown" rather than "free". See issue #148.
func EstimateClaudeCost(model string, inputTokens, cacheReadTokens, cacheCreationTokens, outputTokens int) float64 {
	short := ExtractModelShortName(model)
	rate, ok := ModelPricing[short]
	if !ok {
		return 0
	}
	weightedInput := float64(inputTokens) +
		float64(cacheReadTokens)*0.1 +
		float64(cacheCreationTokens)*1.25
	return (weightedInput*rate.InputPerMTok + float64(outputTokens)*rate.OutputPerMTok) / 1_000_000
}

// ExtractModelShortName 從完整模型 ID 提取簡短名稱
// 例如:
//
//	claude-opus-4-6           → "opus"
//	claude-opus-4-7-fast      → "opus_fast"
//	claude-haiku-4-5-20251001 → "haiku"
//	gpt-5.5-pro               → "gpt-5.5"
//	gpt-5.3-codex             → "codex"
//	gpt-4o-mini               → "gpt-4o"
//	o4-mini                   → "o4-mini"
func ExtractModelShortName(fullModelID string) string {
	m := strings.ToLower(fullModelID)
	switch {
	case strings.Contains(m, "opus") && strings.Contains(m, "fast"):
		return "opus_fast"
	case strings.Contains(m, "opus"):
		return "opus"
	case strings.Contains(m, "sonnet"):
		return "sonnet"
	case strings.Contains(m, "haiku"):
		return "haiku"
	case strings.Contains(m, "codex"):
		return "codex"
	case strings.Contains(m, "gpt-5.5"):
		return "gpt-5.5"
	case strings.Contains(m, "gpt-5.4"):
		return "gpt-5.4"
	case strings.Contains(m, "gpt-4o"):
		return "gpt-4o"
	case strings.Contains(m, "gpt-4.1"):
		return "gpt-4.1"
	case strings.Contains(m, "gpt-"):
		return "gpt"
	case strings.HasPrefix(m, "o3"):
		return "o3"
	case strings.HasPrefix(m, "o4"):
		return "o4-mini"
	}
	// 如果無法識別，返回完整 ID
	return fullModelID
}

// PerformanceMetrics 效能指標結構
type PerformanceMetrics struct {
	Timestamp         time.Time     `json:"timestamp"`
	APICallLatency    time.Duration `json:"api_call_latency"`
	APICallSuccess    bool          `json:"api_call_success"`
	ToolExecutionTime time.Duration `json:"tool_execution_time"`
	ToolExecutionType string        `json:"tool_execution_type"`
	TokensUsed        int           `json:"tokens_used"`
	InputTokens       int           `json:"input_tokens"`
	CacheReadTokens   int           `json:"cache_read_tokens"`
	CacheWriteTokens  int           `json:"cache_write_tokens"`
	OutputTokens      int           `json:"output_tokens"`
	EstimatedCost     float64       `json:"estimated_cost"`
	MemoryUsage       uint64        `json:"memory_usage"`
	ErrorType         string        `json:"error_type,omitempty"`
	ChatID            int64         `json:"chat_id"`
	ProjectPath       string        `json:"project_path,omitempty"`
	AgentType         string        `json:"agent_type,omitempty"`
	Model             string        `json:"model,omitempty"` // NEW: "haiku", "sonnet", "opus"
	FastMode          bool          `json:"fast_mode,omitempty"` // Opus Fast Mode tier (see #178)
}

// PerformanceAnalytics 效能分析數據
type PerformanceAnalytics struct {
	TotalRequests      int64            `json:"total_requests"`
	SuccessRate        float64          `json:"success_rate"`
	AvgAPILatency      time.Duration    `json:"avg_api_latency"`
	AvgToolExecution   time.Duration    `json:"avg_tool_execution"`
	TotalTokens        int64            `json:"total_tokens"`
	TotalCost          float64          `json:"total_cost"`
	ErrorsByType       map[string]int64 `json:"errors_by_type"`
	ToolUsageStats     map[string]int64 `json:"tool_usage_stats"`
	PeakMemoryUsage    uint64           `json:"peak_memory_usage"`
	CurrentMemoryUsage uint64           `json:"current_memory_usage"`
	UptimeSeconds      int64            `json:"uptime_seconds"`
	RequestsPerHour    float64          `json:"requests_per_hour"`
	CostPerRequest     float64          `json:"cost_per_request"`
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
	totalRequests   int64
	successfulCalls int64
	totalAPILatency time.Duration
	totalToolTime   time.Duration
	totalTokens     int64
	totalCacheRead  int64
	totalCacheWrite int64
	totalCost       float64
	errorCounts     map[string]int64
	toolUsage       map[string]int64
	peakMemory      uint64
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
	pm.totalCacheRead += int64(metric.CacheReadTokens)
	pm.totalCacheWrite += int64(metric.CacheWriteTokens)
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

// LoadFromDB loads recent performance metrics from database into memory on startup
func (pm *PerformanceMonitor) LoadFromDB(storage Storage) {
	metrics, err := storage.GetPerformanceMetrics(pm.maxMetricsHistory, 0)
	if err != nil {
		log.Printf("[perf-monitor] failed to load from DB: %v", err)
		return
	}
	reversePerformanceMetrics(metrics)
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.metrics = metrics
	// Rebuild aggregate stats from loaded metrics
	for _, m := range metrics {
		pm.totalRequests++
		if m.APICallSuccess {
			pm.successfulCalls++
		}
		pm.totalAPILatency += m.APICallLatency
		pm.totalToolTime += m.ToolExecutionTime
		pm.totalTokens += int64(m.TokensUsed)
		pm.totalCacheRead += int64(m.CacheReadTokens)
		pm.totalCacheWrite += int64(m.CacheWriteTokens)
		pm.totalCost += m.EstimatedCost
		if m.ErrorType != "" {
			pm.errorCounts[m.ErrorType]++
		}
		if m.ToolExecutionType != "" {
			pm.toolUsage[m.ToolExecutionType]++
		}
		if m.MemoryUsage > pm.peakMemory {
			pm.peakMemory = m.MemoryUsage
		}
	}
	log.Printf("[perf-monitor] loaded %d metrics from DB", len(metrics))
}

func reversePerformanceMetrics(metrics []PerformanceMetrics) {
	for i, j := 0, len(metrics)-1; i < j; i, j = i+1, j-1 {
		metrics[i], metrics[j] = metrics[j], metrics[i]
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
			"data_points":  0,
			"message":      "No data available for the specified period",
		}
	}

	// 計算趨勢數據
	totalLatency := time.Duration(0)
	totalTokens := 0
	totalCost := 0.0
	totalCacheRead := 0
	totalCacheWrite := 0
	successful := 0

	hourlyStats := make(map[int]map[string]interface{})

	for _, metric := range recentMetrics {
		totalLatency += metric.APICallLatency
		totalTokens += metric.TokensUsed
		totalCost += metric.EstimatedCost
		totalCacheRead += metric.CacheReadTokens
		totalCacheWrite += metric.CacheWriteTokens
		if metric.APICallSuccess {
			successful++
		}

		hour := metric.Timestamp.Hour()
		if hourlyStats[hour] == nil {
			hourlyStats[hour] = map[string]interface{}{
				"requests":    0,
				"latency_sum": time.Duration(0),
				"tokens":      0,
				"cost":        0.0,
				"cache_read":  0,
				"cache_write": 0,
				"errors":      0,
			}
		}

		stats := hourlyStats[hour]
		stats["requests"] = stats["requests"].(int) + 1
		stats["latency_sum"] = stats["latency_sum"].(time.Duration) + metric.APICallLatency
		stats["tokens"] = stats["tokens"].(int) + metric.TokensUsed
		stats["cost"] = stats["cost"].(float64) + metric.EstimatedCost
		stats["cache_read"] = stats["cache_read"].(int) + metric.CacheReadTokens
		stats["cache_write"] = stats["cache_write"].(int) + metric.CacheWriteTokens
		if !metric.APICallSuccess || metric.ErrorType != "" {
			stats["errors"] = stats["errors"].(int) + 1
		}
	}

	cacheDenom := totalTokens
	cacheHitRate := 0.0
	if cacheDenom > 0 {
		cacheHitRate = float64(totalCacheRead) / float64(cacheDenom) * 100
	}
	return map[string]interface{}{
		"period_hours":       hours,
		"data_points":        len(recentMetrics),
		"avg_latency_ms":     float64(totalLatency.Nanoseconds()) / float64(len(recentMetrics)) / 1e6,
		"total_tokens":       totalTokens,
		"total_cost":         totalCost,
		"cache_read_tokens":  totalCacheRead,
		"cache_write_tokens": totalCacheWrite,
		"cache_hit_rate":     cacheHitRate,
		"success_rate":       float64(successful) / float64(len(recentMetrics)) * 100,
		"hourly_breakdown":   hourlyStats,
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
func RecordAPICall(latency time.Duration, success bool, tokensUsed int, cost float64, chatID int64, projectPath string, errorType string, model string) {
	RecordAPICallWithCache(latency, success, tokensUsed, cost, chatID, projectPath, errorType, model, 0, 0, 0, 0)
}

func RecordAPICallWithCache(latency time.Duration, success bool, tokensUsed int, cost float64, chatID int64, projectPath string, errorType string, model string, inputTokens, cacheReadTokens, cacheWriteTokens, outputTokens int) {
	if performanceMonitor != nil {
		metric := PerformanceMetrics{
			APICallLatency:   latency,
			APICallSuccess:   success,
			TokensUsed:       tokensUsed,
			InputTokens:      inputTokens,
			CacheReadTokens:  cacheReadTokens,
			CacheWriteTokens: cacheWriteTokens,
			OutputTokens:     outputTokens,
			EstimatedCost:    cost,
			ChatID:           chatID,
			ProjectPath:      projectPath,
			ErrorType:        errorType,
			Model:            model, // NEW: 模型資訊
		}
		performanceMonitor.RecordMetric(metric)
	}
}

// RecordToolExecution 記錄工具執行效能
func RecordToolExecution(toolType string, executionTime time.Duration, chatID int64, projectPath string, success bool) {
	if performanceMonitor != nil {
		metric := PerformanceMetrics{
			ToolExecutionTime: executionTime,
			ToolExecutionType: toolType,
			APICallSuccess:    success, // 重複使用此欄位表示工具執行成功
			ChatID:            chatID,
			ProjectPath:       projectPath,
		}
		performanceMonitor.RecordMetric(metric)
	}
}
