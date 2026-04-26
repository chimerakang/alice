package app

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	appengine "claude-tg-agent/internal/app/engine"
	"claude-tg-agent/internal/app/security"
)

// WebSocketEvent 定義 WebSocket 事件的結構
type WebSocketEvent struct {
	Type      string      `json:"type"`
	Timestamp time.Time   `json:"timestamp"`
	Data      interface{} `json:"data"`
}

// WebSocketClient 代表一個 WebSocket 客戶端連接
type WebSocketClient struct {
	ID       string
	Conn     *websocket.Conn
	Hub      *WebSocketHub
	Send     chan []byte
	LastSeen time.Time
}

// WebSocketHub 管理所有 WebSocket 連接
type WebSocketHub struct {
	mu          sync.RWMutex
	clients     map[*WebSocketClient]bool
	broadcast   chan []byte
	register    chan *WebSocketClient
	unregister  chan *WebSocketClient
	upgrader    websocket.Upgrader
	eventBuffer []WebSocketEvent
	bufferSize  int
}

// 全域 WebSocket Hub 實例
var globalWebSocketHub *WebSocketHub

// NewWebSocketHub 建立新的 WebSocket Hub
func NewWebSocketHub() *WebSocketHub {
	return &WebSocketHub{
		clients:     make(map[*WebSocketClient]bool),
		broadcast:   make(chan []byte, 256),
		register:    make(chan *WebSocketClient),
		unregister:  make(chan *WebSocketClient),
		eventBuffer: make([]WebSocketEvent, 0),
		bufferSize:  100, // 緩存最近 100 個事件
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				// 在生產環境中，這裡應該檢查來源
				return true
			},
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
		},
	}
}

// Run 啟動 WebSocket Hub 的主循環
func (h *WebSocketHub) Run() {
	log.Printf("WebSocket Hub started")
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
			log.Printf("WebSocket client %s connected (total: %d)", client.ID, len(h.clients))

			// 發送最近的事件給新連接的客戶端
			h.sendRecentEvents(client)

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.Send)
				h.mu.Unlock()
				log.Printf("WebSocket client %s disconnected (total: %d)", client.ID, len(h.clients))
			} else {
				h.mu.Unlock()
			}

		case message := <-h.broadcast:
			h.mu.RLock()
			for client := range h.clients {
				select {
				case client.Send <- message:
				default:
					close(client.Send)
					delete(h.clients, client)
				}
			}
			h.mu.RUnlock()
		}
	}
}

// sendRecentEvents 發送最近的事件給新連接的客戶端
func (h *WebSocketHub) sendRecentEvents(client *WebSocketClient) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, event := range h.eventBuffer {
		eventData, _ := json.Marshal(event)
		select {
		case client.Send <- eventData:
		default:
			return
		}
	}
}

// BroadcastEvent 廣播事件到所有連接的客戶端
func (h *WebSocketHub) BroadcastEvent(eventType string, data interface{}) {
	event := WebSocketEvent{
		Type:      eventType,
		Timestamp: time.Now(),
		Data:      data,
	}

	// 添加到事件緩存
	h.mu.Lock()
	h.eventBuffer = append(h.eventBuffer, event)
	if len(h.eventBuffer) > h.bufferSize {
		h.eventBuffer = h.eventBuffer[1:] // 移除最舊的事件
	}
	h.mu.Unlock()

	// 序列化事件
	eventData, err := json.Marshal(event)
	if err != nil {
		log.Printf("Error marshaling WebSocket event: %v", err)
		return
	}

	// 廣播事件
	select {
	case h.broadcast <- eventData:
	default:
		log.Printf("Warning: WebSocket broadcast channel is full, dropping event")
	}
}

// GetConnectedClients 獲取當前連接的客戶端數量
func (h *WebSocketHub) GetConnectedClients() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// HandleWebSocket 處理 WebSocket 連接請求
func (h *WebSocketHub) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}

	client := &WebSocketClient{
		ID:       generateClientID(),
		Conn:     conn,
		Hub:      h,
		Send:     make(chan []byte, 256),
		LastSeen: time.Now(),
	}

	client.Hub.register <- client

	// 啟動客戶端的讀寫 goroutines
	go client.writePump()
	go client.readPump()
}

// writePump 處理向 WebSocket 連接寫入消息
func (c *WebSocketClient) writePump() {
	ticker := time.NewTicker(54 * time.Second)
	defer func() {
		ticker.Stop()
		c.Conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.Send:
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.Conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			// 批量發送任何等待的消息
			n := len(c.Send)
			for i := 0; i < n; i++ {
				w.Write([]byte{'\n'})
				w.Write(<-c.Send)
			}

			if err := w.Close(); err != nil {
				return
			}
		case <-ticker.C:
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// readPump 處理從 WebSocket 連接讀取消息
func (c *WebSocketClient) readPump() {
	defer func() {
		c.Hub.unregister <- c
		c.Conn.Close()
	}()

	c.Conn.SetReadLimit(512)
	c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.Conn.SetPongHandler(func(string) error {
		c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		c.LastSeen = time.Now()
		return nil
	})

	for {
		_, message, err := c.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			break
		}

		c.LastSeen = time.Now()

		// 處理客戶端發送的消息（目前主要是心跳）
		var clientMessage map[string]interface{}
		if err := json.Unmarshal(message, &clientMessage); err == nil {
			if msgType, ok := clientMessage["type"]; ok && msgType == "ping" {
				// 回應 pong
				pongData, _ := json.Marshal(map[string]interface{}{
					"type":      "pong",
					"timestamp": time.Now(),
				})
				select {
				case c.Send <- pongData:
				default:
					close(c.Send)
					return
				}
			}
		}
	}
}

// InitWebSocket 初始化全域 WebSocket Hub
func InitWebSocket() {
	globalWebSocketHub = NewWebSocketHub()
	go globalWebSocketHub.Run()
	log.Printf("WebSocket system initialized")
}

// BroadcastToolEvent 廣播工具執行事件
func BroadcastToolEvent(eventType string, execution ToolExecution) {
	if globalWebSocketHub == nil {
		return
	}

	data := map[string]interface{}{
		"tool_name":   execution.ToolName,
		"status":      execution.Status,
		"duration_ms": execution.Duration.Milliseconds(),
		"chat_id":     execution.ChatID,
		"thread_id":   execution.ThreadID,
		"timestamp":   execution.Timestamp,
		"error":       execution.Error,
	}

	globalWebSocketHub.BroadcastEvent(eventType, data)
}

// BroadcastDecisionEvent 廣播決策事件
func BroadcastDecisionEvent(decision DecisionLog) {
	if globalWebSocketHub == nil {
		return
	}

	// Use the same converter as REST API so frontend gets consistent structure
	data := convertDecisionToFrontendJSON(decision)
	globalWebSocketHub.BroadcastEvent("decision_complete", data)
}

// BroadcastPerformanceEvent 廣播性能指標事件
func BroadcastPerformanceEvent(metric PerformanceMetrics) {
	if globalWebSocketHub == nil {
		return
	}

	data := map[string]interface{}{
		"api_latency_ms":      metric.APICallLatency.Milliseconds(),
		"api_success":         metric.APICallSuccess,
		"tool_execution_time": metric.ToolExecutionTime.Milliseconds(),
		"tool_execution_type": metric.ToolExecutionType,
		"tokens_used":         metric.TokensUsed,
		"estimated_cost":      metric.EstimatedCost,
		"memory_usage":        metric.MemoryUsage,
		"error_type":          metric.ErrorType,
		"chat_id":             metric.ChatID,
		"agent_type":          metric.AgentType,
		"timestamp":           metric.Timestamp,
	}

	globalWebSocketHub.BroadcastEvent("performance_metric", data)
}

// BroadcastSecurityEvent 廣播安全事件
func BroadcastSecurityEvent(event security.SecurityEvent) {
	if globalWebSocketHub == nil {
		return
	}

	data := map[string]interface{}{
		"event_id":    event.ID,
		"event_type":  event.EventType,
		"severity":    event.Severity,
		"description": event.Description,
		"user_id":     event.UserID,
		"ip":          event.IP,
		"user_agent":  event.UserAgent,
		"mitigated":   event.Mitigated,
		"timestamp":   event.Timestamp,
	}

	globalWebSocketHub.BroadcastEvent("security_alert", data)
}

// BroadcastAgentStatusEvent 廣播代理狀態事件
func BroadcastAgentStatusEvent(chatID int64, status string, details map[string]interface{}) {
	if globalWebSocketHub == nil {
		return
	}

	data := map[string]interface{}{
		"chat_id":   chatID,
		"status":    status,
		"details":   details,
		"timestamp": time.Now(),
	}

	globalWebSocketHub.BroadcastEvent("agent_status", data)
}

// BroadcastReviewEvent broadcasts a compact review summary to connected clients.
func BroadcastReviewEvent(notification appengine.ReviewNotification) {
	if globalWebSocketHub == nil {
		return
	}

	data := map[string]interface{}{
		"task_id":          notification.TaskID,
		"reviewer_model":   notification.ReviewerModel,
		"verdict":          notification.Verdict,
		"overall_score":    notification.OverallScore,
		"issue_tags":       notification.IssueTags,
		"advisory_retry":   notification.AdvisoryRetry,
		"failing_subtasks": notification.FailingSubTasks,
		"retry_note":       notification.RetryNote,
		"timestamp":        time.Now(),
	}

	globalWebSocketHub.BroadcastEvent("review_complete", data)
}

// generateClientID 生成客戶端 ID
func generateClientID() string {
	return time.Now().Format("20060102150405") + "-" +
		string(rune('A'+time.Now().Nanosecond()%26))
}

// GetWebSocketStats 獲取 WebSocket 統計資訊
func GetWebSocketStats() map[string]interface{} {
	if globalWebSocketHub == nil {
		return map[string]interface{}{
			"enabled":           false,
			"connected_clients": 0,
		}
	}

	return map[string]interface{}{
		"enabled":           true,
		"connected_clients": globalWebSocketHub.GetConnectedClients(),
		"event_buffer_size": len(globalWebSocketHub.eventBuffer),
		"max_buffer_size":   globalWebSocketHub.bufferSize,
	}
}
