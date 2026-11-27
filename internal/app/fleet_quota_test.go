package app

import (
	"context"
	"net/http"
	"regexp"
	"testing"
	"time"

	"github.com/chimerakang/alice-core/fleet"
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

func TestFleetBackendUsageFilterAndQuotaEstimate(t *testing.T) {
	e := newWebFleetEnv(t)
	e.svc.Config.ScoutQuota = fleetScoutQuotaConfig{WindowMinutes: 60, ClaudeTokens: 100, CodexTokens: 100}
	task := e.dispatch("/ship quota fixture")
	if err := e.svc.Store.AddBackendUsage(context.Background(), task.ID, "claude", "test-model", fleet.Usage{InputTokens: 25}); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Store.AddUsage(context.Background(), task.ID, fleet.Usage{InputTokens: 10}); err != nil {
		t.Fatal(err)
	}
	until := time.Now().UTC().Add(time.Second).Format(time.RFC3339)
	result := e.get("/api/fleet/usage/backends?backend=claude&until=" + until)
	rows := list(t, result, "backends")
	if len(rows) != 1 || rows[0]["backend"] != "claude" || rows[0]["input_tokens"] != "25" {
		t.Fatalf("filtered backend usage = %v", rows)
	}
	rows = list(t, e.get("/api/fleet/usage/backends?backend=unattributed&until="+until), "backends")
	if len(rows) != 1 || rows[0]["input_tokens"] != "10" {
		t.Fatalf("unattributed usage = %v", rows)
	}
	estimates, err := e.svc.estimatedBackendQuota(context.Background(), time.Now().Add(time.Second))
	if err != nil || estimates[fleetBackendClaude] != 0.75 || estimates[fleetBackendCodex] != 1 {
		t.Fatalf("estimates = %v, err %v", estimates, err)
	}
	if code, _ := e.call(http.MethodGet, "/api/fleet/usage/backends?backend=invalid", "", false); code != http.StatusBadRequest {
		t.Fatalf("invalid backend returned %d", code)
	}
}
