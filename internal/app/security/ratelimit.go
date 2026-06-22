package security

import (
	"sync"
	"time"
)

// RateLimiter 速率限制器
type RateLimiter struct {
	mu            sync.RWMutex
	visitors      map[string]*visitorInfo
	rpm           int
	burst         int
	cleanupPeriod time.Duration
}

type visitorInfo struct {
	tokens     int
	lastSeen   time.Time
	requests   []time.Time
	blocked    bool
	blockUntil time.Time
}

// Allow 檢查速率限制
func (rl *RateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	visitor, exists := rl.visitors[key]

	if !exists {
		visitor = &visitorInfo{
			tokens:   rl.burst,
			lastSeen: now,
			requests: []time.Time{},
		}
		rl.visitors[key] = visitor
	}

	if visitor.blocked && now.Before(visitor.blockUntil) {
		return false
	}
	visitor.blocked = false

	elapsed := now.Sub(visitor.lastSeen)
	tokensToAdd := int(elapsed.Seconds() * float64(rl.rpm) / 60.0)
	if tokensToAdd > 0 {
		visitor.tokens = minInt(rl.burst, visitor.tokens+tokensToAdd)
		visitor.lastSeen = now
	}

	cutoff := now.Add(-time.Minute)
	var recent []time.Time
	for _, reqTime := range visitor.requests {
		if reqTime.After(cutoff) {
			recent = append(recent, reqTime)
		}
	}
	visitor.requests = recent

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

func (rl *RateLimiter) cleanupExpired() {
	ticker := time.NewTicker(rl.cleanupPeriod)
	defer ticker.Stop()

	for range ticker.C {
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

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
