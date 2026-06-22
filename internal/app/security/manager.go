package security

import (
	"crypto/cipher"
	"fmt"
	"log"
	"sync"
	"time"
)

// SecurityManager 安全管理器
type SecurityManager struct {
	config      SecurityConfig
	rateLimiter *RateLimiter
	auditLog    []SecurityEvent
	auditMu     sync.RWMutex
	cipher      cipher.AEAD
	piiPatterns []PIIPattern
	sessions    map[string]SessionInfo
	sessionMu   sync.RWMutex
}

// NewSecurityManager 建立安全管理器
func NewSecurityManager(config SecurityConfig) (*SecurityManager, error) {
	sm := &SecurityManager{
		config:      config,
		auditLog:    make([]SecurityEvent, 0),
		sessions:    make(map[string]SessionInfo),
		piiPatterns: getDefaultPIIPatterns(),
	}

	if config.EnableRateLimiting {
		sm.rateLimiter = &RateLimiter{
			visitors:      make(map[string]*visitorInfo),
			rpm:           config.RateLimitRPM,
			burst:         config.RateLimitBurst,
			cleanupPeriod: 5 * time.Minute,
		}
		go sm.rateLimiter.cleanupExpired()
	}

	if config.EncryptionKey != "" {
		if err := sm.initEncryption(config.EncryptionKey); err != nil {
			return nil, fmt.Errorf("encryption init failed: %w", err)
		}
	}

	return sm, nil
}

// LogSecurityEvent 記錄安全事件
func (sm *SecurityManager) LogSecurityEvent(event SecurityEvent) {
	if !sm.config.EnableAuditLogging {
		return
	}

	sm.auditMu.Lock()
	event.ID = fmt.Sprintf("sec_%d", time.Now().UnixNano())
	event.EventID = event.ID
	event.Timestamp = time.Now()

	sm.auditLog = append(sm.auditLog, event)

	if len(sm.auditLog) > 10000 {
		sm.auditLog = sm.auditLog[1000:]
	}
	sm.auditMu.Unlock()

	if OnPersistEvent != nil {
		go func(e SecurityEvent) {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("Warning: OnPersistEvent panicked: %v", r)
				}
			}()
			OnPersistEvent(e)
		}(event)
	}

	if OnBroadcastEvent != nil {
		OnBroadcastEvent(event)
	}
}

// GetSecurityEvents 從記憶體 audit log 取得最近的安全事件
func (sm *SecurityManager) GetSecurityEvents(limit int, severity string) []SecurityEvent {
	sm.auditMu.RLock()
	defer sm.auditMu.RUnlock()

	var filtered []SecurityEvent
	for i := len(sm.auditLog) - 1; i >= 0; i-- {
		event := sm.auditLog[i]
		if severity == "" || event.Severity == severity {
			filtered = append(filtered, event)
			if limit > 0 && len(filtered) >= limit {
				break
			}
		}
	}

	return filtered
}

// GetSecurityStats 獲取安全統計
func (sm *SecurityManager) GetSecurityStats() map[string]interface{} {
	sm.auditMu.RLock()
	defer sm.auditMu.RUnlock()

	eventTypes := make(map[string]int)
	severities := make(map[string]int)

	for _, event := range sm.auditLog {
		eventTypes[event.EventType]++
		severities[event.Severity]++
	}

	stats := map[string]interface{}{
		"total_events":  len(sm.auditLog),
		"event_types":   eventTypes,
		"severities":    severities,
		"rate_limiting": sm.config.EnableRateLimiting,
		"pii_detection": sm.config.EnablePIIDetection,
		"audit_logging": sm.config.EnableAuditLogging,
		"encryption":    sm.cipher != nil,
	}

	if sm.rateLimiter != nil {
		sm.rateLimiter.mu.RLock()
		stats["active_visitors"] = len(sm.rateLimiter.visitors)
		sm.rateLimiter.mu.RUnlock()
	}

	return stats
}
