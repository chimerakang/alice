package app

import (
	"encoding/json"
	"log"
	"net/http"
	"time"
)

// handleSourceStats 返回來源分布統計
func (wi *WebInterface) handleSourceStats(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if globalStorage == nil {
			http.Error(w, `{"error":"storage not available"}`, http.StatusServiceUnavailable)
			return
		}

		// 獲取所有決策記錄
		decisions, err := globalStorage.GetDecisionLogs(1000, 0) // 獲取最近 1000 條
		if err != nil {
			log.Printf("[web] error getting decisions for source stats: %v", err)
			http.Error(w, `{"error":"failed to get decisions"}`, http.StatusInternalServerError)
			return
		}

		// 統計來源分布
		sourceCounts := make(map[string]int)
		for _, decision := range decisions {
			source := decision.Source
			if source == "" {
				source = "telegram" // 預設為 telegram
			}
			sourceCounts[source]++
		}

		// 構建回應
		response := map[string]interface{}{
			"total_decisions": len(decisions),
			"source_counts":   sourceCounts,
			"timestamp":       time.Now(),
		}

		json.NewEncoder(w).Encode(response)
	})(w, r)
}

// handleSourcePerformance 返回來源效能對比
func (wi *WebInterface) handleSourcePerformance(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if globalStorage == nil {
			http.Error(w, `{"error":"storage not available"}`, http.StatusServiceUnavailable)
			return
		}

		// 獲取所有決策記錄
		decisions, err := globalStorage.GetDecisionLogs(1000, 0)
		if err != nil {
			log.Printf("[web] error getting decisions for source performance: %v", err)
			http.Error(w, `{"error":"failed to get decisions"}`, http.StatusInternalServerError)
			return
		}

		// 按來源分組並計算效能
		sourcePerformance := make(map[string]map[string]interface{})

		for _, decision := range decisions {
			source := decision.Source
			if source == "" {
				source = "telegram"
			}

			if sourcePerformance[source] == nil {
				sourcePerformance[source] = map[string]interface{}{
					"total_decisions": 0,
					"total_duration":  0,
					"successful":      0,
					"avg_duration":    0.0,
					"success_rate":    0.0,
				}
			}

			perf := sourcePerformance[source]
			perf["total_decisions"] = perf["total_decisions"].(int) + 1

			// 累計執行時間
			if decision.DurationMs > 0 {
				perf["total_duration"] = perf["total_duration"].(int) + decision.DurationMs
			}

			// 判斷是否成功（檢查 outcome 和 tool_calls 狀態）
			isSuccess := true
			if decision.Outcome.Success == false {
				isSuccess = false
			} else {
				// 檢查 tool calls 是否有錯誤
				for _, toolCall := range decision.ToolCalls {
					if toolCall.Status == "STATUS_ERROR" || toolCall.Status == "4" {
						isSuccess = false
						break
					}
				}
			}
			if isSuccess {
				perf["successful"] = perf["successful"].(int) + 1
			}
		}

		// 計算平均值
		for _, perf := range sourcePerformance {
			totalDecisions := perf["total_decisions"].(int)
			totalDuration := perf["total_duration"].(int)
			successful := perf["successful"].(int)

			if totalDecisions > 0 {
				perf["avg_duration"] = float64(totalDuration) / float64(totalDecisions)
				perf["success_rate"] = (float64(successful) / float64(totalDecisions)) * 100
			}
		}

		response := map[string]interface{}{
			"source_performance": sourcePerformance,
			"total_decisions":    len(decisions),
			"timestamp":          time.Now(),
		}

		json.NewEncoder(w).Encode(response)
	})(w, r)
}