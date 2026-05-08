package app

import (
	"encoding/json"
	"testing"
)

func TestModelRoutingConfigUnmarshalAcceptsStickyModeAliases(t *testing.T) {
	cfg := ModelRoutingConfig{StickySession: true, StickyMode: true, SessionIdleTimeoutMin: 5, SessionIdleTimeout: 5}
	if err := json.Unmarshal([]byte(`{"sticky_mode":false,"session_idle_timeout":12}`), &cfg); err != nil {
		t.Fatalf("json.Unmarshal returned error: %v", err)
	}
	if cfg.StickyEnabled() {
		t.Fatalf("StickyEnabled = true, want false")
	}
	if cfg.SessionIdleTimeoutMin != 12 || cfg.SessionIdleTimeout != 12 {
		t.Fatalf("idle timeout = %d/%d, want 12/12", cfg.SessionIdleTimeoutMin, cfg.SessionIdleTimeout)
	}
}

func TestModelRoutingConfigUnmarshalKeepsLegacyKeys(t *testing.T) {
	cfg := ModelRoutingConfig{StickySession: true, StickyMode: true, SessionIdleTimeoutMin: 5, SessionIdleTimeout: 5}
	if err := json.Unmarshal([]byte(`{"sticky_session":false,"session_idle_timeout_min":9}`), &cfg); err != nil {
		t.Fatalf("json.Unmarshal returned error: %v", err)
	}
	if cfg.StickyEnabled() {
		t.Fatalf("StickyEnabled = true, want false")
	}
	if cfg.SessionIdleTimeoutMin != 9 || cfg.SessionIdleTimeout != 9 {
		t.Fatalf("idle timeout = %d/%d, want 9/9", cfg.SessionIdleTimeoutMin, cfg.SessionIdleTimeout)
	}
}

func TestModelRoutingConfigNormalizeUsesConservativeDefaults(t *testing.T) {
	cfg := ModelRoutingConfig{}
	cfg.Normalize()
	// Default 24h (1440 min) per #170 — Telegram users routinely pause for
	// hours/overnight; the previous 5-min default wiped recent context on
	// normal-life pauses.
	if cfg.IdleTimeoutMinutes() != 1440 {
		t.Fatalf("IdleTimeoutMinutes = %d, want 1440 (24h)", cfg.IdleTimeoutMinutes())
	}
	if cfg.StickyEnabled() {
		t.Fatalf("StickyEnabled = true, want false for zero-value config")
	}
}
