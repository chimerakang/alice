package app

import (
	"regexp"
	"testing"
	"time"
)

func TestRouteWithQuota(t *testing.T) {
	now := time.Now()
	low, high := 0.2, 0.8
	full := fleetQuotaSnapshot{Blocked: true, ResetsAt: now.Add(time.Hour)}
	earlier := fleetQuotaSnapshot{Blocked: true, ResetsAt: now.Add(time.Minute)}
	for _, tc := range []struct {
		name          string
		eligible      bool
		claude, codex fleetQuotaSnapshot
		want          fleetQuotaDecision
	}{
		{"prefer more capacity", true, fleetQuotaSnapshot{Remaining: &low}, fleetQuotaSnapshot{Remaining: &high}, fleetQuotaDecision{Backend: fleetBackendCodex}},
		{"USD budget or Ship waits for Claude", false, full, earlier, fleetQuotaDecision{WaitUntil: full.ResetsAt}},
		{"both exhausted wait for earliest reset", true, full, earlier, fleetQuotaDecision{WaitUntil: earlier.ResetsAt}},
		{"reset permits routing again", true, fleetQuotaSnapshot{Blocked: true, ResetsAt: now}, earlier, fleetQuotaDecision{Backend: fleetBackendClaude}},
		{"unknown reset still blocks", false, fleetQuotaSnapshot{Blocked: true}, fleetQuotaSnapshot{}, fleetQuotaDecision{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := routeWithQuota(now, fleetBackendClaude, tc.eligible, tc.claude, tc.codex); got != tc.want {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestMatchFleetQuotaError(t *testing.T) {
	patterns := []*regexp.Regexp{regexp.MustCompile(`weekly limit; reset (?P<reset>\S+)`), regexp.MustCompile(`subscription quota exhausted`)}
	for _, message := range []string{"weekly limit; reset 2026-11-27T14:00:00+08:00", "subscription quota exhausted", "weekly limit; reset invalid", "network timeout"} {
		blocked, reset, err := matchFleetQuotaError(message, patterns, time.RFC3339, time.UTC)
		switch message {
		case "network timeout":
			if blocked || err != nil {
				t.Fatalf("unrelated error marked blocked")
			}
		case "weekly limit; reset invalid":
			if !blocked || err == nil {
				t.Fatalf("invalid reset should retain block")
			}
		case "subscription quota exhausted":
			if !blocked || !reset.IsZero() || err != nil {
				t.Fatalf("unknown reset should retain block")
			}
		default:
			if !blocked || reset.UTC().Hour() != 6 || err != nil {
				t.Fatalf("wrong parsed reset: %v %v", reset, err)
			}
		}
	}
}
