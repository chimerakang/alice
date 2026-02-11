package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	alicev1 "claude-tg-agent/gen/go/alice/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Proto 轉換函數
// 將現有的結構體轉換為 proto 生成的類型

// convertToProtoAgentInfo 將 AgentInfo 轉換為 proto AgentInfo
func convertToProtoAgentInfo(info AgentInfo) *alicev1.AgentInfo {
	status := alicev1.Status_STATUS_PENDING
	if info.IsActive {
		status = alicev1.Status_STATUS_RUNNING
	}

	return &alicev1.AgentInfo{
		Id:           generateAgentID(info.ChatID, info.ThreadID),
		ChatId:       info.ChatID,
		ThreadId:     int32(info.ThreadID),
		ProjectDir:   info.ProjectDir,
		SessionId:    info.SessionID,
		Status:       status,
		CreatedAt:    timestamppb.New(info.CreatedAt),
		LastActive:   timestamppb.New(info.LastActivity),
		TokenStats:   convertToProtoTokenStats(info.Stats),
		Model:        "", // 需要從配置獲取
		GitState:     getGitStateForProject(info.ProjectDir),
	}
}

// convertToProtoTokenStats 將 TokenStats 轉換為 proto TokenStats
func convertToProtoTokenStats(stats TokenStats) *alicev1.TokenStats {
	return &alicev1.TokenStats{
		TotalInputTokens:  stats.TotalInputTokens,
		TotalOutputTokens: stats.TotalOutputTokens,
		TotalCostUsd:      stats.TotalCostUSD,
		ApiCallCount:      int64(stats.APICallCount),
	}
}

// convertFromProtoAgentInfo 將 proto AgentInfo 轉換為 AgentInfo
func convertFromProtoAgentInfo(protoInfo *alicev1.AgentInfo) AgentInfo {
	isActive := protoInfo.Status == alicev1.Status_STATUS_RUNNING

	var lastActivity time.Time
	if protoInfo.LastActive != nil {
		lastActivity = protoInfo.LastActive.AsTime()
	}

	var createdAt time.Time
	if protoInfo.CreatedAt != nil {
		createdAt = protoInfo.CreatedAt.AsTime()
	}

	var stats TokenStats
	if protoInfo.TokenStats != nil {
		stats = convertFromProtoTokenStats(protoInfo.TokenStats)
	}

	return AgentInfo{
		ChatID:       protoInfo.ChatId,
		ThreadID:     int(protoInfo.ThreadId),
		ProjectDir:   protoInfo.ProjectDir,
		SessionID:    protoInfo.SessionId,
		IsActive:     isActive,
		LastActivity: lastActivity,
		CreatedAt:    createdAt,
		ProjectCount: 1, // 預設值
		Stats:        stats,
	}
}

// convertFromProtoTokenStats 將 proto TokenStats 轉換為 TokenStats
func convertFromProtoTokenStats(protoStats *alicev1.TokenStats) TokenStats {
	return TokenStats{
		TotalInputTokens:  protoStats.TotalInputTokens,
		TotalOutputTokens: protoStats.TotalOutputTokens,
		TotalCostUSD:      protoStats.TotalCostUsd,
		APICallCount:      int(protoStats.ApiCallCount),
	}
}

// convertToProtoListAgentsResponse 將 agent 列表轉換為 proto ListAgentsResponse
func convertToProtoListAgentsResponse(agentInfos []AgentInfo) *alicev1.ListAgentsResponse {
	protoAgents := make([]*alicev1.AgentInfo, len(agentInfos))
	for i, info := range agentInfos {
		protoAgents[i] = convertToProtoAgentInfo(info)
	}

	return &alicev1.ListAgentsResponse{
		Agents: protoAgents,
		Pagination: &alicev1.Pagination{
			Page:       1,
			PageSize:   int32(len(agentInfos)),
			TotalPages: 1,
			TotalCount: int64(len(agentInfos)),
		},
		Error: nil,
	}
}

// generateAgentID 生成唯一的 agent ID
func generateAgentID(chatID int64, threadID int) string {
	if threadID == 0 {
		return fmt.Sprintf("agent_%d", chatID)
	}
	return fmt.Sprintf("agent_%d_%d", chatID, threadID)
}

// Proto-First API 處理函數

// handleAgentsProto 使用 proto 的 agents 端點處理函數
func (wi *WebInterface) handleAgentsProto(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		// 獲取現有的 agent 資料
		agentInfos := wi.getAllAgentsInfo()

		// 轉換為 proto 響應
		protoResponse := convertToProtoListAgentsResponse(agentInfos)

		// 設定 Content-Type
		w.Header().Set("Content-Type", "application/json")

		// 序列化並發送響應
		if err := writeProtoResponse(w, protoResponse); err != nil {
			http.Error(w, "Failed to serialize response", http.StatusInternalServerError)
			return
		}
	})(w, r)
}

// handleAgentDetailProto 使用 proto 的 agent 詳細資訊端點處理函數
func (wi *WebInterface) handleAgentDetailProto(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// 解析 chat_id 和 thread_id
		chatIDStr := r.URL.Query().Get("chat_id")
		threadIDStr := r.URL.Query().Get("thread_id")

		if chatIDStr == "" {
			// 嘗試從 URL 路徑提取 /api/agents/123456
			urlParts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/agents/"), "/")
			if len(urlParts) > 0 && urlParts[0] != "" {
				chatIDStr = urlParts[0]
			}
		}

		if chatIDStr == "" {
			writeProtoError(w, "Missing chat_id parameter", http.StatusBadRequest)
			return
		}

		chatID, err := strconv.ParseInt(chatIDStr, 10, 64)
		if err != nil {
			writeProtoError(w, "Invalid chat_id", http.StatusBadRequest)
			return
		}

		threadID := 0
		if threadIDStr != "" {
			threadID, _ = strconv.Atoi(threadIDStr)
		}

		// 獲取 agent 資訊
		agentInfo := wi.getAgentInfo(chatID, threadID)
		if agentInfo == nil {
			writeProtoError(w, "Agent not found", http.StatusNotFound)
			return
		}

		// 轉換為 proto 響應
		protoAgent := convertToProtoAgentInfo(*agentInfo)
		response := &alicev1.GetAgentResponse{
			Agent: protoAgent,
		}

		// 發送響應
		if err := writeProtoResponse(w, response); err != nil {
			writeProtoError(w, "Failed to serialize response", http.StatusInternalServerError)
			return
		}
	})(w, r)
}

// writeProtoError 寫入 proto 格式的錯誤響應
func writeProtoError(w http.ResponseWriter, message string, statusCode int) {
	w.WriteHeader(statusCode)
	errorResponse := &alicev1.Error{
		Code:    fmt.Sprintf("%d", statusCode),
		Message: message,
		Details: []string{http.StatusText(statusCode)},
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error": errorResponse,
	})
}

// convertToProtoToolExecution 將 ToolExecution 轉換為 proto ToolExecution
func convertToProtoToolExecution(exec ToolExecution) *alicev1.ToolExecution {
	status := alicev1.Status_STATUS_PENDING
	switch exec.Status {
	case "running":
		status = alicev1.Status_STATUS_RUNNING
	case "success":
		status = alicev1.Status_STATUS_SUCCESS
	case "error":
		status = alicev1.Status_STATUS_ERROR
	}

	// 轉換 Input map[string]interface{} 為 map[string]string
	inputStr := make(map[string]string)
	for key, value := range exec.Input {
		inputStr[key] = fmt.Sprintf("%v", value)
	}

	return &alicev1.ToolExecution{
		Id:         generateToolExecutionID(exec.ChatID, exec.ThreadID, exec.Timestamp),
		Timestamp:  timestamppb.New(exec.Timestamp),
		ToolName:   exec.ToolName,
		Input:      inputStr,
		Status:     status,
		DurationMs: exec.Duration.Milliseconds(),
		ChatId:     exec.ChatID,
		ThreadId:   int32(exec.ThreadID),
		Error:      exec.Error,
	}
}

// generateToolExecutionID 生成工具執行唯一 ID
func generateToolExecutionID(chatID int64, threadID int, timestamp time.Time) string {
	return fmt.Sprintf("tool_%d_%d_%d", chatID, threadID, timestamp.Unix())
}

// handleRecentToolsProto 使用 proto 的最近工具執行端點處理函數
func (wi *WebInterface) handleRecentToolsProto(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// 解析 limit 參數
		limitStr := r.URL.Query().Get("limit")
		limit := 20 // 預設限制
		if limitStr != "" {
			if parsedLimit, err := strconv.Atoi(limitStr); err == nil && parsedLimit > 0 {
				limit = parsedLimit
			}
		}

		// 獲取最近的工具執行記錄
		executions := globalToolLogger.GetRecentExecutions(limit)

		// 轉換為 proto 響應
		protoExecutions := make([]*alicev1.ToolExecution, len(executions))
		for i, exec := range executions {
			protoExecutions[i] = convertToProtoToolExecution(exec)
		}

		response := &alicev1.ListToolExecutionsResponse{
			Executions: protoExecutions,
			Pagination: &alicev1.Pagination{
				TotalCount: int64(len(executions)),
				Page:       1,
				PageSize:   int32(limit),
			},
		}

		// 發送響應
		if err := writeProtoResponse(w, response); err != nil {
			writeProtoError(w, "Failed to serialize response", http.StatusInternalServerError)
			return
		}
	})(w, r)
}

// handleToolExecutionsProto 使用 proto 的工具執行統計端點處理函數
func (wi *WebInterface) handleToolExecutionsProto(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// 獲取所有執行記錄
		executions := globalToolLogger.GetRecentExecutions(0)

		// 計算統計數據
		toolCounts := make(map[string]int)
		statusCounts := map[string]int{
			"running": 0,
			"success": 0,
			"error":   0,
		}

		for _, exec := range executions {
			toolCounts[exec.ToolName]++
			statusCounts[exec.Status]++
		}

		// 計算成功率
		total := statusCounts["success"] + statusCounts["error"]
		successRate := 100.0
		if total > 0 {
			successRate = float64(statusCounts["success"]) / float64(total) * 100
		}

		// 建立統計響應 (使用通用結構)
		toolStats := make(map[string]interface{})
		for tool, count := range toolCounts {
			toolStats[tool] = count
		}

		response := map[string]interface{}{
			"total_executions": len(executions),
			"success_rate":     successRate,
			"tool_counts":      toolStats,
			"status_counts": map[string]int{
				"running": statusCounts["running"],
				"success": statusCounts["success"],
				"error":   statusCounts["error"],
			},
			"timestamp": time.Now(),
		}

		// 發送響應
		if err := writeProtoResponse(w, response); err != nil {
			writeProtoError(w, "Failed to serialize response", http.StatusInternalServerError)
			return
		}
	})(w, r)
}

// convertToProtoExecutionOutcome 將 ExecutionOutcome 轉換為 proto ExecutionOutcome
func convertToProtoExecutionOutcome(outcome ExecutionOutcome) *alicev1.ExecutionOutcome {
	severity := alicev1.Severity_SEVERITY_LOW
	if !outcome.Success {
		severity = alicev1.Severity_SEVERITY_HIGH
	}

	return &alicev1.ExecutionOutcome{
		Success:      outcome.Success,
		TaskType:     outcome.TaskType,
		FilesChanged: outcome.FilesChanged,
		Summary:      outcome.Summary,
		ErrorMessage: outcome.ErrorMessage,
		Severity:     severity,
	}
}

// convertToProtoDecisionLog 將 DecisionLog 轉換為 proto DecisionLog
func convertToProtoDecisionLog(decision DecisionLog) *alicev1.DecisionLog {
	// 轉換 ToolCalls
	protoToolCalls := make([]*alicev1.ToolExecution, len(decision.ToolCalls))
	for i, tool := range decision.ToolCalls {
		protoToolCalls[i] = convertToProtoToolExecution(tool)
	}

	// 轉換 Context map[string]interface{} 為 map[string]string
	contextStr := make(map[string]string)
	for key, value := range decision.Context {
		contextStr[key] = fmt.Sprintf("%v", value)
	}

	return &alicev1.DecisionLog{
		Id:            generateDecisionID(decision.ChatID, decision.ThreadID, decision.Timestamp),
		Timestamp:     timestamppb.New(decision.Timestamp),
		UserPrompt:    decision.UserPrompt,
		AgentResponse: decision.AgentResponse,
		ToolCalls:     protoToolCalls,
		ChatId:        decision.ChatID,
		ThreadId:      int32(decision.ThreadID),
		ProjectPath:   decision.ProjectPath,
		DurationMs:    int64(decision.DurationMs),
		TokenStats:    convertToProtoTokenStats(decision.TokensUsed),
		Outcome:       convertToProtoExecutionOutcome(decision.Outcome),
		Context:       contextStr,
		PrivacyLevel:  alicev1.PrivacyLevel_PRIVACY_LEVEL_PUBLIC, // 預設為 public
	}
}

// generateDecisionID 生成決策記錄唯一 ID
func generateDecisionID(chatID int64, threadID int, timestamp time.Time) string {
	return fmt.Sprintf("decision_%d_%d_%d", chatID, threadID, timestamp.Unix())
}

// handleDecisionsProto 使用 proto 的決策統計端點處理函數
func (wi *WebInterface) handleDecisionsProto(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// 獲取所有決策記錄
		decisions := globalDecisionLogger.GetRecentDecisions(0)

		// 計算統計數據
		totalDecisions := len(decisions)
		successCount := 0
		taskTypeCounts := make(map[string]int)

		for _, decision := range decisions {
			if decision.Outcome.Success {
				successCount++
			}
			taskTypeCounts[decision.Outcome.TaskType]++
		}

		successRate := 100.0
		if totalDecisions > 0 {
			successRate = float64(successCount) / float64(totalDecisions) * 100
		}

		// 建立統計響應
		response := map[string]interface{}{
			"total_decisions": totalDecisions,
			"success_rate":    successRate,
			"task_types":      taskTypeCounts,
			"recent_decisions": func() []interface{} {
				// 返回最近 10 個決策
				limit := 10
				if totalDecisions < limit {
					limit = totalDecisions
				}
				recent := make([]interface{}, limit)
				for i := 0; i < limit; i++ {
					recent[i] = convertToProtoDecisionLog(decisions[i])
				}
				return recent
			}(),
			"timestamp": time.Now(),
		}

		if err := writeProtoResponse(w, response); err != nil {
			writeProtoError(w, "Failed to serialize response", http.StatusInternalServerError)
			return
		}
	})(w, r)
}

// handleRecentDecisionsProto 使用 proto 的最近決策端點處理函數
func (wi *WebInterface) handleRecentDecisionsProto(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// 解析 limit 參數
		limitStr := r.URL.Query().Get("limit")
		limit := 10 // 預設限制
		if limitStr != "" {
			if parsedLimit, err := strconv.Atoi(limitStr); err == nil && parsedLimit > 0 {
				limit = parsedLimit
			}
		}

		// 獲取最近的決策記錄
		decisions := globalDecisionLogger.GetRecentDecisions(limit)

		// 轉換為 proto 響應
		protoDecisions := make([]*alicev1.DecisionLog, len(decisions))
		for i, decision := range decisions {
			protoDecisions[i] = convertToProtoDecisionLog(decision)
		}

		response := &alicev1.ListDecisionsResponse{
			Decisions: protoDecisions,
			Pagination: &alicev1.Pagination{
				TotalCount: int64(len(decisions)),
				Page:       1,
				PageSize:   int32(limit),
			},
		}

		if err := writeProtoResponse(w, response); err != nil {
			writeProtoError(w, "Failed to serialize response", http.StatusInternalServerError)
			return
		}
	})(w, r)
}

// ========== MultiAgent API Proto Handlers ==========

func (wi *WebInterface) handleMultiAgentStatusProto(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// 簡化實作：返回基本狀態
		response := map[string]interface{}{
			"coordinator_status": "active",
			"active_tasks":       0,
			"specialized_agents": []string{"code_generator", "file_manager", "analyzer"},
			"timestamp":         time.Now(),
		}
		writeProtoResponse(w, response)
	})(w, r)
}

func (wi *WebInterface) handleMultiAgentStatsProto(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		response := map[string]interface{}{
			"total_coordinated_tasks": 0,
			"success_rate":           100.0,
			"avg_task_duration":      0,
			"agent_utilization":      map[string]float64{},
			"timestamp":              time.Now(),
		}
		writeProtoResponse(w, response)
	})(w, r)
}

func (wi *WebInterface) handleMultiAgentAgentsProto(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		response := map[string]interface{}{
			"agents":    []string{"code_generator", "file_manager", "analyzer"},
			"total":     3,
			"timestamp": time.Now(),
		}
		writeProtoResponse(w, response)
	})(w, r)
}

// ========== Performance API Proto Handlers ==========

func (wi *WebInterface) handlePerformanceAnalyticsProto(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		response := map[string]interface{}{
			"total_operations":    0,
			"avg_response_time":   0,
			"error_rate":          0.0,
			"throughput":          0,
			"resource_usage":      map[string]interface{}{},
			"timestamp":           time.Now(),
		}
		writeProtoResponse(w, response)
	})(w, r)
}

func (wi *WebInterface) handlePerformanceMetricsProto(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		response := map[string]interface{}{
			"metrics":   []interface{}{},
			"total":     0,
			"timestamp": time.Now(),
		}
		writeProtoResponse(w, response)
	})(w, r)
}

func (wi *WebInterface) handlePerformanceTrendsProto(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		response := map[string]interface{}{
			"trends":    []interface{}{},
			"total":     0,
			"timestamp": time.Now(),
		}
		writeProtoResponse(w, response)
	})(w, r)
}

func (wi *WebInterface) handlePerformanceRecommendationsProto(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		response := map[string]interface{}{
			"recommendations": []interface{}{},
			"total":           0,
			"timestamp":       time.Now(),
		}
		writeProtoResponse(w, response)
	})(w, r)
}

func (wi *WebInterface) handlePerformanceExportProto(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		response := map[string]interface{}{
			"export_url": "/api/performance/export/data.json",
			"status":     "ready",
			"timestamp":  time.Now(),
		}
		writeProtoResponse(w, response)
	})(w, r)
}

// ========== Security API Proto Handlers ==========

func (wi *WebInterface) handleSecurityEventsProto(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		response := map[string]interface{}{
			"events":    []interface{}{},
			"total":     0,
			"timestamp": time.Now(),
		}
		writeProtoResponse(w, response)
	})(w, r)
}

func (wi *WebInterface) handleSecurityStatsProto(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		response := map[string]interface{}{
			"total_events":      0,
			"threat_level":      "low",
			"blocked_attempts":  0,
			"pii_detections":    0,
			"timestamp":         time.Now(),
		}
		writeProtoResponse(w, response)
	})(w, r)
}

func (wi *WebInterface) handleSecurityAuditProto(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		response := map[string]interface{}{
			"audit_logs": []interface{}{},
			"total":      0,
			"timestamp":  time.Now(),
		}
		writeProtoResponse(w, response)
	})(w, r)
}

// getGitStateForProject 獲取專案的 Git 狀態
func getGitStateForProject(projectDir string) *alicev1.GitState {
	if gitManager == nil {
		return nil
	}

	gitState, err := gitManager.GetGitState(projectDir)
	if err != nil {
		// 如果無法獲取 Git 狀態，返回 nil（可能不是 Git 專案）
		return nil
	}

	return gitState
}

// convertToProtoGitEvent 將 GitEvent 轉換為 proto GitEvent（為儀表板使用）
func convertToProtoGitEvent(event GitEvent) map[string]interface{} {
	return map[string]interface{}{
		"timestamp":   event.Timestamp.Format(time.RFC3339),
		"project_dir": event.ProjectDir,
		"operation":   event.Operation,
		"args":        event.Args,
		"success":     event.Success,
		"output":      event.Output,
		"error":       event.Error,
		"chat_id":     event.ChatID,
		"thread_id":   event.ThreadID,
		"user_prompt": event.UserPrompt,
	}
}

// ========== Git API Proto Handlers ==========

// handleGitStatus 處理 Git 狀態查詢
func (wi *WebInterface) handleGitStatus(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// 獲取專案目錄參數
		projectDir := r.URL.Query().Get("project_dir")
		if projectDir == "" {
			projectDir = "." // 預設為當前目錄
		}

		// 獲取 Git 狀態
		gitState := getGitStateForProject(projectDir)

		response := map[string]interface{}{
			"project_dir": projectDir,
			"git_state":   gitState,
			"timestamp":   time.Now(),
		}

		if gitState == nil {
			response["error"] = "Not a Git repository or Git not available"
		}

		writeProtoResponse(w, response)
	})(w, r)
}

// handleGitEvents 處理 Git 事件查詢
func (wi *WebInterface) handleGitEvents(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// 解析查詢參數
		limitStr := r.URL.Query().Get("limit")
		projectDir := r.URL.Query().Get("project_dir")

		limit := 20 // 預設限制
		if limitStr != "" {
			if parsedLimit, err := strconv.Atoi(limitStr); err == nil && parsedLimit > 0 {
				limit = parsedLimit
			}
		}

		var events []GitEvent
		if gitEventLogger != nil {
			if projectDir != "" {
				events = gitEventLogger.GetEventsByProject(projectDir, limit)
			} else {
				events = gitEventLogger.GetRecentEvents(limit)
			}
		}

		// 轉換事件為前端可讀格式
		protoEvents := make([]map[string]interface{}, len(events))
		for i, event := range events {
			protoEvents[i] = convertToProtoGitEvent(event)
		}

		response := map[string]interface{}{
			"events":      protoEvents,
			"total":       len(events),
			"project_dir": projectDir,
			"timestamp":   time.Now(),
		}

		writeProtoResponse(w, response)
	})(w, r)
}

// handleGitOperations 處理 Git 操作權限查詢和設定
func (wi *WebInterface) handleGitOperations(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method == "GET" {
			// 獲取當前操作權限
			var permissions map[string]bool
			if gitOperationManager != nil {
				permissions = gitOperationManager.GetOperationPermissions()
			} else {
				permissions = map[string]bool{}
			}

			response := map[string]interface{}{
				"permissions": permissions,
				"safety_mode": gitOperationManager != nil && gitOperationManager.safetyMode,
				"timestamp":   time.Now(),
			}

			writeProtoResponse(w, response)

		} else if r.Method == "POST" {
			// 更新操作權限（僅限管理員）
			var request struct {
				Operation string `json:"operation"`
				Allowed   bool   `json:"allowed"`
				SafetyMode *bool  `json:"safety_mode,omitempty"`
			}

			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				writeProtoError(w, "Invalid JSON request", http.StatusBadRequest)
				return
			}

			if gitOperationManager != nil {
				if request.SafetyMode != nil {
					gitOperationManager.EnableSafetyMode(*request.SafetyMode)
				}

				if request.Operation != "" {
					gitOperationManager.AllowOperation(request.Operation, request.Allowed)
				}
			}

			// 返回更新後的權限
			permissions := gitOperationManager.GetOperationPermissions()
			response := map[string]interface{}{
				"permissions": permissions,
				"safety_mode": gitOperationManager.safetyMode,
				"updated":     true,
				"timestamp":   time.Now(),
			}

			writeProtoResponse(w, response)
		} else {
			writeProtoError(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})(w, r)
}

// writeProtoResponse 將 proto message 序列化為 JSON 並寫入響應
func writeProtoResponse(w http.ResponseWriter, msg interface{}) error {
	// 這裡我們仍然使用 JSON 序列化，以保持與前端的兼容性
	// 未來可以切換到 protobuf 序列化以提高效能
	return json.NewEncoder(w).Encode(msg)
}