package security

import "time"

// SecurityEvent 安全事件記錄
type SecurityEvent struct {
	ID          string                 `json:"id,omitempty"`
	EventID     string                 `json:"event_id"`
	Timestamp   time.Time              `json:"timestamp"`
	EventType   string                 `json:"event_type"`
	Severity    string                 `json:"severity"`
	Description string                 `json:"description"`
	UserID      int64                  `json:"user_id,omitempty"`
	IP          string                 `json:"ip,omitempty"`
	UserAgent   string                 `json:"user_agent,omitempty"`
	Details     map[string]interface{} `json:"details,omitempty"`
	Mitigated   bool                   `json:"mitigated"`
}

// SessionInfo 會話資訊
type SessionInfo struct {
	UserID    int64     `json:"user_id"`
	CreatedAt time.Time `json:"created_at"`
	LastSeen  time.Time `json:"last_seen"`
	IP        string    `json:"ip"`
	UserAgent string    `json:"user_agent"`
}

// OnPersistEvent 持久化 callback（由 app 層注入）
var OnPersistEvent func(SecurityEvent)

// OnBroadcastEvent 廣播 callback（由 app 層注入）
var OnBroadcastEvent func(SecurityEvent)
