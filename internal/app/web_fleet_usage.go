package app

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

func (wi *WebInterface) fleetBackendUsage(ctx context.Context, svc *FleetService, r *http.Request) (proto.Message, error) {
	query := r.URL.Query()
	backendFilter := query.Get("backend")
	switch backendFilter {
	case "", fleetBackendClaude, fleetBackendCodex, "unattributed":
	default:
		return nil, fleetBadRequest("backend must be claude, codex or unattributed")
	}
	now := time.Now().UTC()
	end := now
	if raw := query.Get("until"); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return nil, fleetBadRequest("until must be an RFC3339 timestamp")
		}
		end = parsed
	}
	start := end.Add(-24 * time.Hour)
	if raw := query.Get("since"); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return nil, fleetBadRequest("since must be an RFC3339 timestamp")
		}
		start = parsed
	}
	if !start.Before(end) {
		return nil, fleetBadRequest("since must be earlier than until")
	}
	// Preserve integer token counts as decimal strings in the JSON response.
	rows := make([]any, 0)
	err := svc.Store.VisitBackendUsage(ctx, start, end, func(backend string, input, output int64, costUSD float64, tasks int64) {
		if backendFilter != "" && backendFilter != backend && !(backendFilter == "unattributed" && backend == "") {
			return
		}
		rows = append(rows, map[string]any{
			"backend":       backend,
			"input_tokens":  strconv.FormatInt(input, 10),
			"output_tokens": strconv.FormatInt(output, 10),
			"cost_usd":      costUSD,
			"tasks":         strconv.FormatInt(tasks, 10),
		})
	})
	if err != nil {
		return nil, err
	}
	remaining, err := svc.estimatedBackendQuota(ctx, now)
	if err != nil {
		return nil, err
	}
	quota := make([]any, 0, 2)
	for _, backend := range []string{fleetBackendClaude, fleetBackendCodex} {
		blocked, reset, err := svc.Store.BackendQuotaBlocked(ctx, backend)
		if err != nil {
			return nil, err
		}
		entry := map[string]any{"backend": backend, "blocked": blocked}
		measured, measurementReset, known, err := svc.Store.BackendQuotaMeasurement(ctx, backend)
		if err != nil {
			return nil, err
		}
		if known {
			entry["provider_remaining"] = measured
			if !blocked {
				reset = measurementReset
			}
		} else if estimate, ok := remaining[backend]; ok {
			entry["estimated_remaining"] = estimate
		}
		if reset.After(now) {
			entry["resets_at"] = reset.Format(time.RFC3339Nano)
		}
		quota = append(quota, entry)
	}
	return structpb.NewStruct(map[string]any{"since": start.Format(time.RFC3339Nano), "until": end.Format(time.RFC3339Nano), "backends": rows, "quota": quota})
}
