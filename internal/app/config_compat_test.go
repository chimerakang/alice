package app

import (
	"encoding/json"
	"testing"
)

// A config written for the removed task system must still load.
func TestOldHermesConfigStillParses(t *testing.T) {
	var cfg Config
	old := `{"hermes":{"enabled":true,"auto_route_complex":true,"planner_effort":"low",
		"budget":{"max_total_tokens":1},"github_integration":{"enabled":true}},
		"fleet":{"backend":"hermes","enabled":true}}`
	if err := json.Unmarshal([]byte(old), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Hermes.PlannerEffort != "low" || cfg.Fleet.backendWarning() == "" {
		t.Fatalf("hermes=%+v warning=%q", cfg.Hermes, cfg.Fleet.backendWarning())
	}
}
