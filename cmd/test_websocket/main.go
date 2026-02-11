package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// WebSocketEvent represents the event structure we expect from Alice
type WebSocketEvent struct {
	Type      string      `json:"type"`
	Timestamp time.Time   `json:"timestamp"`
	Data      interface{} `json:"data"`
}

func main() {
	fmt.Println("🧪 Testing Alice WebSocket Real-time Events")
	fmt.Println("=" + strings.Repeat("=", 50))

	// WebSocket server URL
	serverURL := "ws://localhost:8080/ws"
	if len(os.Args) > 1 {
		serverURL = os.Args[1]
	}

	fmt.Printf("Connecting to: %s\n\n", serverURL)

	// Parse URL
	u, err := url.Parse(serverURL)
	if err != nil {
		log.Fatalf("Failed to parse WebSocket URL: %v", err)
	}

	// Set up interrupt handler
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	// Connect to WebSocket
	fmt.Println("📡 Attempting WebSocket connection...")
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		log.Fatalf("Failed to connect to WebSocket: %v", err)
	}
	defer conn.Close()

	fmt.Println("✅ Connected to Alice WebSocket server!")
	fmt.Println("🎧 Listening for real-time events...\n")

	// Channel for receiving messages
	done := make(chan struct{})

	// Statistics
	eventCounts := make(map[string]int)
	startTime := time.Now()

	// Start listening for messages
	go func() {
		defer close(done)
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					log.Printf("WebSocket error: %v", err)
				}
				return
			}

			// Parse the event
			var event WebSocketEvent
			if err := json.Unmarshal(message, &event); err != nil {
				fmt.Printf("❌ Failed to parse event: %v\n", err)
				continue
			}

			// Count events
			eventCounts[event.Type]++

			// Display the event
			displayEvent(event)
		}
	}()

	// Send periodic ping messages
	pingTicker := time.NewTicker(30 * time.Second)
	defer pingTicker.Stop()

	go func() {
		for {
			select {
			case <-pingTicker.C:
				pingMessage := map[string]interface{}{
					"type":      "ping",
					"timestamp": time.Now(),
				}
				if err := conn.WriteJSON(pingMessage); err != nil {
					log.Printf("Failed to send ping: %v", err)
					return
				}
				fmt.Println("📤 Sent ping to server")
			case <-done:
				return
			}
		}
	}()

	// Statistics ticker
	statsTicker := time.NewTicker(30 * time.Second)
	defer statsTicker.Stop()

	go func() {
		for {
			select {
			case <-statsTicker.C:
				showStatistics(eventCounts, startTime)
			case <-done:
				return
			}
		}
	}()

	// Wait for interrupt
	for {
		select {
		case <-done:
			fmt.Println("\n🔌 WebSocket connection closed")
			showStatistics(eventCounts, startTime)
			return
		case <-interrupt:
			fmt.Println("\n⏹️ Interrupt received, closing connection...")

			// Cleanly close the connection
			err := conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			if err != nil {
				log.Println("Error during close:", err)
				return
			}
			select {
			case <-done:
			case <-time.After(time.Second):
			}
			showStatistics(eventCounts, startTime)
			return
		}
	}
}

func displayEvent(event WebSocketEvent) {
	timestamp := event.Timestamp.Format("15:04:05")

	switch event.Type {
	case "tool_execution_start":
		data := event.Data.(map[string]interface{})
		toolName := data["tool_name"]
		chatID := data["chat_id"]
		fmt.Printf("🔧 [%s] Tool started: %s (Chat: %.0f)\n", timestamp, toolName, chatID)

	case "tool_execution":
		data := event.Data.(map[string]interface{})
		toolName := data["tool_name"]
		status := data["status"]
		chatID := data["chat_id"]
		duration := data["duration_ms"]

		statusIcon := getStatusIcon(status)
		fmt.Printf("%s [%s] Tool completed: %s -> %s (Chat: %.0f, Duration: %.0fms)\n",
			statusIcon, timestamp, toolName, status, chatID, duration)

	case "decision_complete":
		data := event.Data.(map[string]interface{})
		success := data["success"].(bool)
		taskType := data["task_type"]
		duration := data["duration_ms"]

		successIcon := "✅"
		if !success {
			successIcon = "❌"
		}
		fmt.Printf("%s [%s] Decision: %s -> %s (Duration: %.0fms)\n",
			successIcon, timestamp, taskType, getSuccessStatus(success), duration)

	case "performance_metric":
		data := event.Data.(map[string]interface{})
		toolType := data["tool_execution_type"]
		apiLatency := data["api_latency_ms"]
		memUsage := data["memory_usage"]

		fmt.Printf("📊 [%s] Performance: %s (API: %.0fms, Memory: %.0f bytes)\n",
			timestamp, toolType, apiLatency, memUsage)

	case "security_alert":
		data := event.Data.(map[string]interface{})
		severity := data["severity"]
		eventType := data["event_type"]
		description := data["description"]

		severityIcon := getSeverityIcon(severity.(string))
		fmt.Printf("%s [%s] Security: [%s] %s - %s\n",
			severityIcon, timestamp, strings.ToUpper(severity.(string)), eventType, description)

	case "agent_status":
		data := event.Data.(map[string]interface{})
		chatID := data["chat_id"]
		status := data["status"]
		fmt.Printf("👤 [%s] Agent %.0f: %s\n", timestamp, chatID, status)

	case "pong":
		fmt.Printf("📥 [%s] Pong received from server\n", timestamp)

	default:
		fmt.Printf("❓ [%s] Unknown event: %s\n", timestamp, event.Type)
	}
}

func getStatusIcon(status interface{}) string {
	switch status.(string) {
	case "success":
		return "✅"
	case "error":
		return "❌"
	case "running":
		return "🔄"
	default:
		return "❓"
	}
}

func getSuccessStatus(success bool) string {
	if success {
		return "SUCCESS"
	}
	return "FAILED"
}

func getSeverityIcon(severity string) string {
	switch severity {
	case "critical":
		return "🚨"
	case "high":
		return "⚠️"
	case "medium":
		return "🔸"
	case "low":
		return "🔹"
	default:
		return "ℹ️"
	}
}

func showStatistics(eventCounts map[string]int, startTime time.Time) {
	fmt.Println("\n" + strings.Repeat("=", 50))
	fmt.Println("📈 WebSocket Event Statistics")
	fmt.Println(strings.Repeat("-", 50))

	duration := time.Since(startTime)
	totalEvents := 0
	for _, count := range eventCounts {
		totalEvents += count
	}

	fmt.Printf("⏱️  Connected for: %v\n", duration.Round(time.Second))
	fmt.Printf("📦 Total events: %d\n", totalEvents)

	if totalEvents > 0 {
		fmt.Printf("📊 Events per minute: %.1f\n\n", float64(totalEvents)/duration.Minutes())

		fmt.Println("Event breakdown:")
		for eventType, count := range eventCounts {
			percentage := float64(count) / float64(totalEvents) * 100
			fmt.Printf("  %-20s: %4d (%.1f%%)\n", eventType, count, percentage)
		}
	}

	fmt.Println(strings.Repeat("=", 50) + "\n")
}