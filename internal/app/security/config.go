package security

// SecurityConfig 安全配置
type SecurityConfig struct {
	EnableRateLimiting    bool     `json:"enable_rate_limiting"`
	RateLimitRPM          int      `json:"rate_limit_rpm"`
	RateLimitBurst        int      `json:"rate_limit_burst"`
	EnablePIIDetection    bool     `json:"enable_pii_detection"`
	EnableAuditLogging    bool     `json:"enable_audit_logging"`
	DataRetentionDays     int      `json:"data_retention_days"`
	RequireAuthentication bool     `json:"require_authentication"`
	EncryptionKey         string   `json:"encryption_key,omitempty"`
	AllowedIPs            []string `json:"allowed_ips"`
	BlockedIPs            []string `json:"blocked_ips"`
	SessionTimeoutMinutes int      `json:"session_timeout_minutes"`
	MaxConcurrentSessions int      `json:"max_concurrent_sessions"`
}
