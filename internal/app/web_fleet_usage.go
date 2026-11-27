package app

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

var errFleetBackendUsageUnavailable = errors.New("backend usage requires a newer Fleet store")

type fleetBackendUsageStore interface {
	VisitBackendUsage(context.Context, time.Time, time.Time, func(string, int64, int64, float64, int64)) error
}

func (wi *WebInterface) fleetBackendUsage(ctx context.Context, svc *FleetService, r *http.Request) (proto.Message, error) {
	query := r.URL.Query()
	end := time.Now().UTC()
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
	store, ok := any(svc.Store).(fleetBackendUsageStore)
	if !ok {
		return nil, errFleetBackendUsageUnavailable
	}

	// Preserve integer token counts as decimal strings in the JSON response.
	rows := make([]any, 0)
	err := store.VisitBackendUsage(ctx, start, end, func(backend string, input, output int64, costUSD float64, tasks int64) {
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
	return structpb.NewStruct(map[string]any{"since": start.Format(time.RFC3339Nano), "until": end.Format(time.RFC3339Nano), "backends": rows})
}
