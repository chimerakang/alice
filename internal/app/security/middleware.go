package security

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
)

// RateLimitMiddleware HTTP 速率限制中間件
func (sm *SecurityManager) RateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if sm.rateLimiter == nil {
			next.ServeHTTP(w, r)
			return
		}

		ip := getClientIP(r)
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
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'")

		next.ServeHTTP(w, r)
	})
}
