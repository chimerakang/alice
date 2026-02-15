package app

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// SecurityConfig 安全配置
type SecurityConfig struct {
	EnableRateLimiting    bool          `json:"enable_rate_limiting"`
	RateLimitRPM          int           `json:"rate_limit_rpm"`      // requests per minute
	RateLimitBurst        int           `json:"rate_limit_burst"`    // burst capacity
	EnablePIIDetection    bool          `json:"enable_pii_detection"`
	EnableAuditLogging    bool          `json:"enable_audit_logging"`
	DataRetentionDays     int           `json:"data_retention_days"`
	RequireAuthentication bool          `json:"require_authentication"`
	EncryptionKey         string        `json:"encryption_key,omitempty"` // Base64 encoded
	AllowedIPs            []string      `json:"allowed_ips"`
	BlockedIPs            []string      `json:"blocked_ips"`
	SessionTimeoutMinutes int           `json:"session_timeout_minutes"`
	MaxConcurrentSessions int           `json:"max_concurrent_sessions"`
}

// MultimediaConfig 多媒體處理設定
type MultimediaConfig struct {
	EnablePhotoSupport    bool   `json:"enable_photo_support"`
	EnableVoiceSupport    bool   `json:"enable_voice_support"`
	OpenAIAPIKey          string `json:"openai_api_key"`
	MaxFileSizeMB         int    `json:"max_file_size_mb"`
	TempDownloadDir       string `json:"temp_download_dir"`
	VoiceToTextProvider   string `json:"voice_to_text_provider"` // "openai_whisper"
}

// SecurityEvent 安全事件記錄
type SecurityEvent struct {
	ID          string                 `json:"id"`
	Timestamp   time.Time              `json:"timestamp"`
	EventType   string                 `json:"event_type"`
	Severity    string                 `json:"severity"` // low, medium, high, critical
	Description string                 `json:"description"`
	UserID      int64                  `json:"user_id,omitempty"`
	IP          string                 `json:"ip,omitempty"`
	UserAgent   string                 `json:"user_agent,omitempty"`
	Details     map[string]interface{} `json:"details,omitempty"`
	Mitigated   bool                   `json:"mitigated"`
}

// PIIPattern PII 偵測模式
type PIIPattern struct {
	Name        string `json:"name"`
	Pattern     string `json:"pattern"`
	Severity    string `json:"severity"`
	Replacement string `json:"replacement"`
}

// RateLimiter 速率限制器
type RateLimiter struct {
	mu       sync.RWMutex
	visitors       map[string]*VisitorInfo
	rpm            int
	burst          int
	cleanupPeriod  time.Duration
}

// VisitorInfo 訪客資訊
type VisitorInfo struct {
	tokens     int
	lastSeen   time.Time
	requests   []time.Time
	blocked    bool
	blockUntil time.Time
}

// SecurityManager 安全管理器
type SecurityManager struct {
	config       SecurityConfig
	rateLimiter  *RateLimiter
	auditLog     []SecurityEvent
	auditMu      sync.RWMutex
	cipher       cipher.AEAD
	piiPatterns  []PIIPattern
	sessions     map[string]SessionInfo
	sessionMu    sync.RWMutex
}

// SessionInfo 會話資訊
type SessionInfo struct {
	UserID    int64     `json:"user_id"`
	CreatedAt time.Time `json:"created_at"`
	LastSeen  time.Time `json:"last_seen"`
	IP        string    `json:"ip"`
	UserAgent string    `json:"user_agent"`
}

// NewSecurityManager 建立安全管理器
func NewSecurityManager(config SecurityConfig) (*SecurityManager, error) {
	sm := &SecurityManager{
		config:      config,
		auditLog:    make([]SecurityEvent, 0),
		sessions:    make(map[string]SessionInfo),
		piiPatterns: getDefaultPIIPatterns(),
	}

	// 初始化速率限制器
	if config.EnableRateLimiting {
		sm.rateLimiter = &RateLimiter{
			visitors:      make(map[string]*VisitorInfo),
			rpm:           config.RateLimitRPM,
			burst:         config.RateLimitBurst,
			cleanupPeriod: 5 * time.Minute,
		}
		go sm.rateLimiter.cleanupExpired()
	}

	// 初始化加密
	if config.EncryptionKey != "" {
		if err := sm.initEncryption(config.EncryptionKey); err != nil {
			return nil, fmt.Errorf("encryption init failed: %w", err)
		}
	}

	return sm, nil
}

// initEncryption 初始化加密
func (sm *SecurityManager) initEncryption(keyStr string) error {
	key, err := base64.StdEncoding.DecodeString(keyStr)
	if err != nil {
		// 如果不是 base64，則使用字串的 SHA256 作為金鑰
		hash := sha256.Sum256([]byte(keyStr))
		key = hash[:]
	}

	if len(key) != 32 {
		return fmt.Errorf("encryption key must be 32 bytes (got %d)", len(key))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}

	sm.cipher, err = cipher.NewGCM(block)
	if err != nil {
		return err
	}

	return nil
}

// getDefaultPIIPatterns 獲取預設 PII 偵測模式
func getDefaultPIIPatterns() []PIIPattern {
	return []PIIPattern{
		{
			Name:        "email",
			Pattern:     `\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Z|a-z]{2,}\b`,
			Severity:    "medium",
			Replacement: "[EMAIL_REDACTED]",
		},
		{
			Name:        "phone",
			Pattern:     `(\+?\d{1,3}[-.\s]?)?\(?\d{3}\)?[-.\s]?\d{3}[-.\s]?\d{4}`,
			Severity:    "medium",
			Replacement: "[PHONE_REDACTED]",
		},
		{
			Name:        "ssn",
			Pattern:     `\b\d{3}-?\d{2}-?\d{4}\b`,
			Severity:    "high",
			Replacement: "[SSN_REDACTED]",
		},
		{
			Name:        "credit_card",
			// 更嚴格的信用卡檢測：必須有分隔符或上下文關鍵詞
			Pattern:     `(?i)(?:card\s*(?:number|#)?[:=\s]*)?(?:\b\d{4}[\s-]\d{4}[\s-]\d{4}[\s-]\d{4}\b|\b\d{4}\s\d{4}\s\d{4}\s\d{4}\b)`,
			Severity:    "high",
			Replacement: "[CARD_REDACTED]",
		},
		{
			Name:        "ip_address",
			Pattern:     `\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b`,
			Severity:    "low",
			Replacement: "[IP_REDACTED]",
		},
		{
			Name:        "api_key",
			Pattern:     `(?i)(api[_-]?key|token|secret)["\s:=]+([a-zA-Z0-9_-]{20,})`,
			Severity:    "critical",
			Replacement: "[API_KEY_REDACTED]",
		},
	}
}

// RateLimitMiddleware HTTP 速率限制中間件
func (sm *SecurityManager) RateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if sm.rateLimiter == nil {
			next.ServeHTTP(w, r)
			return
		}

		ip := getClientIP(r)
		// Skip rate limiting for localhost/private IPs (dashboard, internal services)
		if isLocalOrPrivateIP(ip) {
			next.ServeHTTP(w, r)
			return
		}
		if !sm.rateLimiter.Allow(ip) {
			sm.LogSecurityEvent(SecurityEvent{
				EventType:   "rate_limit_exceeded",
				Severity:    "medium",
				Description: "Rate limit exceeded",
				IP:          ip,
				UserAgent:   r.UserAgent(),
			})

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error":   "Rate limit exceeded",
				"message": "Too many requests, please try again later",
			})
			return
		}

		next.ServeHTTP(w, r)
	})
}

// IPFilterMiddleware IP 過濾中間件
func (sm *SecurityManager) IPFilterMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := getClientIP(r)

		// 檢查黑名單
		for _, blockedIP := range sm.config.BlockedIPs {
			if ip == blockedIP || sm.matchIPPattern(ip, blockedIP) {
				sm.LogSecurityEvent(SecurityEvent{
					EventType:   "blocked_ip_access",
					Severity:    "high",
					Description: fmt.Sprintf("Access denied for blocked IP: %s", ip),
					IP:          ip,
					UserAgent:   r.UserAgent(),
				})

				w.WriteHeader(http.StatusForbidden)
				return
			}
		}

		// 檢查白名單（如果配置了）
		if len(sm.config.AllowedIPs) > 0 {
			allowed := false
			for _, allowedIP := range sm.config.AllowedIPs {
				if ip == allowedIP || sm.matchIPPattern(ip, allowedIP) {
					allowed = true
					break
				}
			}
			if !allowed {
				sm.LogSecurityEvent(SecurityEvent{
					EventType:   "unauthorized_ip_access",
					Severity:    "medium",
					Description: fmt.Sprintf("Access denied for non-whitelisted IP: %s", ip),
					IP:          ip,
					UserAgent:   r.UserAgent(),
				})

				w.WriteHeader(http.StatusForbidden)
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

// matchIPPattern 匹配 IP 模式（支持 CIDR）
func (sm *SecurityManager) matchIPPattern(ip, pattern string) bool {
	if strings.Contains(pattern, "/") {
		_, network, err := net.ParseCIDR(pattern)
		if err != nil {
			return false
		}
		clientIP := net.ParseIP(ip)
		return network.Contains(clientIP)
	}
	return ip == pattern
}

// SecurityHeadersMiddleware 安全標頭中間件
func (sm *SecurityManager) SecurityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 安全標頭
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'")

		next.ServeHTTP(w, r)
	})
}

// Allow 檢查速率限制
func (rl *RateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	visitor, exists := rl.visitors[key]

	if !exists {
		visitor = &VisitorInfo{
			tokens:   rl.burst,
			lastSeen: now,
			requests: []time.Time{},
		}
		rl.visitors[key] = visitor
	}

	// 檢查是否被阻擋
	if visitor.blocked && now.Before(visitor.blockUntil) {
		return false
	}
	visitor.blocked = false

	// 更新令牌（按秒計算，避免不滿一分鐘時 token 無法補充）
	elapsed := now.Sub(visitor.lastSeen)
	tokensToAdd := int(elapsed.Seconds() * float64(rl.rpm) / 60.0)
	if tokensToAdd > 0 {
		visitor.tokens = min(rl.burst, visitor.tokens+tokensToAdd)
		visitor.lastSeen = now
	}

	// 清理舊的請求記錄
	cutoff := now.Add(-time.Minute)
	var recent []time.Time
	for _, reqTime := range visitor.requests {
		if reqTime.After(cutoff) {
			recent = append(recent, reqTime)
		}
	}
	visitor.requests = recent

	// 檢查 RPM 限制
	if len(visitor.requests) >= rl.rpm {
		visitor.blocked = true
		visitor.blockUntil = now.Add(time.Minute)
		return false
	}

	if visitor.tokens <= 0 {
		return false
	}

	visitor.tokens--
	visitor.requests = append(visitor.requests, now)
	return true
}

// cleanupExpired 清理過期的訪客資訊
func (rl *RateLimiter) cleanupExpired() {
	ticker := time.NewTicker(rl.cleanupPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			rl.mu.Lock()
			cutoff := time.Now().Add(-time.Hour)
			for key, visitor := range rl.visitors {
				if visitor.lastSeen.Before(cutoff) && !visitor.blocked {
					delete(rl.visitors, key)
				}
			}
			rl.mu.Unlock()
		}
	}
}

// DetectAndFilterPII 偵測和過濾 PII
func (sm *SecurityManager) DetectAndFilterPII(text string, logEvent bool) (filtered string, detected []string) {
	if !sm.config.EnablePIIDetection {
		return text, nil
	}

	filtered = text
	detected = []string{}

	for _, pattern := range sm.piiPatterns {
		regex, err := regexp.Compile(pattern.Pattern)
		if err != nil {
			continue
		}

		matches := regex.FindAllString(text, -1)
		if len(matches) > 0 {
			detected = append(detected, pattern.Name)
			filtered = regex.ReplaceAllString(filtered, pattern.Replacement)

			// 只在需要時記錄 PII 偵測事件
			if logEvent {
				sm.LogSecurityEvent(SecurityEvent{
					EventType:   "pii_detected",
					Severity:    pattern.Severity,
					Description: fmt.Sprintf("PII detected: %s", pattern.Name),
					Details: map[string]interface{}{
						"pattern":     pattern.Name,
						"matches":     len(matches),
						"context":     "generic",
						"location":    "System",
						"source_type": "text_processing",
					},
				})
			}
		}
	}

	return filtered, detected
}

// Encrypt 加密文字
func (sm *SecurityManager) Encrypt(plaintext string) (string, error) {
	if sm.cipher == nil {
		return plaintext, fmt.Errorf("encryption not configured")
	}

	nonce := make([]byte, sm.cipher.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := sm.cipher.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt 解密文字
func (sm *SecurityManager) Decrypt(ciphertext string) (string, error) {
	if sm.cipher == nil {
		return ciphertext, fmt.Errorf("encryption not configured")
	}

	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}

	nonceSize := sm.cipher.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, cipherBytes := data[:nonceSize], data[nonceSize:]
	plaintext, err := sm.cipher.Open(nil, nonce, cipherBytes, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}

// LogSecurityEvent 記錄安全事件
func (sm *SecurityManager) LogSecurityEvent(event SecurityEvent) {
	if !sm.config.EnableAuditLogging {
		return
	}

	sm.auditMu.Lock()
	defer sm.auditMu.Unlock()

	event.ID = fmt.Sprintf("sec_%d", time.Now().UnixNano())
	event.Timestamp = time.Now()

	sm.auditLog = append(sm.auditLog, event)

	// 保持日誌大小限制
	if len(sm.auditLog) > 10000 {
		sm.auditLog = sm.auditLog[1000:]
	}

	// 如果有 SQLite 儲存，將安全事件寫入資料庫
	if globalStorage != nil {
		go func() {
			if err := globalStorage.InsertSecurityEvent(event); err != nil {
				log.Printf("Warning: failed to persist security event to database: %v", err)
			}
		}()
	}

	// 廣播安全事件到 WebSocket 客戶端
	BroadcastSecurityEvent(event)
}

// GetSecurityEvents 獲取安全事件
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
		"total_events":   len(sm.auditLog),
		"event_types":    eventTypes,
		"severities":     severities,
		"rate_limiting":  sm.config.EnableRateLimiting,
		"pii_detection":  sm.config.EnablePIIDetection,
		"audit_logging":  sm.config.EnableAuditLogging,
		"encryption":     sm.cipher != nil,
	}

	if sm.rateLimiter != nil {
		sm.rateLimiter.mu.RLock()
		stats["active_visitors"] = len(sm.rateLimiter.visitors)
		sm.rateLimiter.mu.RUnlock()
	}

	return stats
}

// getClientIP 獲取客戶端 IP
// isLocalOrPrivateIP checks if an IP is localhost or a private/Docker network address.
func isLocalOrPrivateIP(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	if ip.IsLoopback() {
		return true
	}
	// Docker bridge networks (172.16-31.x.x, 10.x.x.x, 192.168.x.x)
	privateRanges := []net.IPNet{
		{IP: net.IP{10, 0, 0, 0}, Mask: net.CIDRMask(8, 32)},
		{IP: net.IP{172, 16, 0, 0}, Mask: net.CIDRMask(12, 32)},
		{IP: net.IP{192, 168, 0, 0}, Mask: net.CIDRMask(16, 32)},
	}
	for _, r := range privateRanges {
		if r.Contains(ip) {
			return true
		}
	}
	return false
}

func getClientIP(r *http.Request) string {
	// 檢查 X-Forwarded-For 標頭
	forwarded := r.Header.Get("X-Forwarded-For")
	if forwarded != "" {
		// 取第一個 IP（如果有多個代理）
		ips := strings.Split(forwarded, ",")
		return strings.TrimSpace(ips[0])
	}

	// 檢查 X-Real-IP 標頭
	realIP := r.Header.Get("X-Real-IP")
	if realIP != "" {
		return realIP
	}

	// 使用 RemoteAddr
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

// min 輔助函數
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// 全域安全管理器實例
var globalSecurityManager *SecurityManager

// InitSecurity 初始化安全管理器
func InitSecurity(config SecurityConfig) error {
	var err error
	globalSecurityManager, err = NewSecurityManager(config)
	return err
}