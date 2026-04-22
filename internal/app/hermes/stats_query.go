package hermes

import (
	"database/sql"
	"fmt"
	"time"
)

// AggregatedStats represents accumulated statistics over a period.
type AggregatedStats struct {
	Period         string       // "latest" / "week" / "chat" / "all"
	TaskCount      int
	TotalTokens    int
	ModelBreakdown []ModelUsage
	AvgSubTasks    float64
	AvgWallclock   time.Duration
	SuccessRate    float64 // success / total
	SuccessCount   int
}

// StatsQuerier handles aggregation queries over task states.
type StatsQuerier struct {
	db *sql.DB
}

// NewStatsQuerier creates a new querier instance.
func NewStatsQuerier(db *sql.DB) *StatsQuerier {
	return &StatsQuerier{db: db}
}

// QueryLatest returns stats for the most recent task.
func (sq *StatsQuerier) QueryLatest() (*AggregatedStats, error) {
	// Fetch the most recent task.
	var taskID string
	var planJSON string
	var usagesJSON string
	var status string

	err := sq.db.QueryRow(`
		SELECT id, plan, model_usages, status
		FROM task_states
		ORDER BY created_at DESC
		LIMIT 1
	`).Scan(&taskID, &planJSON, &usagesJSON, &status)

	if err == sql.ErrNoRows {
		return nil, nil // no tasks yet
	}
	if err != nil {
		return nil, err
	}

	return sq.aggregateTask(taskID, planJSON, usagesJSON, status)
}

// QueryWeek returns stats for the past 7 days.
func (sq *StatsQuerier) QueryWeek(chatID int64) (*AggregatedStats, error) {
	return sq.queryPeriod(chatID, 7*24*time.Hour)
}

// QueryChat returns stats for all tasks in a chat.
func (sq *StatsQuerier) QueryChat(chatID int64) (*AggregatedStats, error) {
	rows, err := sq.db.Query(`
		SELECT id, plan, model_usages, status
		FROM task_states
		WHERE chat_id = ?
		ORDER BY created_at DESC
	`, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	stats := &AggregatedStats{
		Period:         "chat",
		ModelBreakdown: []ModelUsage{},
	}

	totalWallclock := 0
	for rows.Next() {
		var taskID, planJSON, usagesJSON, status string
		if err := rows.Scan(&taskID, &planJSON, &usagesJSON, &status); err != nil {
			return nil, err
		}

		taskStats, err := sq.aggregateTask(taskID, planJSON, usagesJSON, status)
		if err != nil {
			continue
		}

		stats.TaskCount++
		stats.TotalTokens += taskStats.TotalTokens
		stats.SuccessCount += taskStats.SuccessCount
		totalWallclock += int(taskStats.AvgWallclock.Seconds())

		// Accumulate model usages.
		for _, mu := range taskStats.ModelBreakdown {
			found := false
			for i := range stats.ModelBreakdown {
				if stats.ModelBreakdown[i].Model == mu.Model {
					stats.ModelBreakdown[i].InputTokens += mu.InputTokens
					stats.ModelBreakdown[i].OutputTokens += mu.OutputTokens
					stats.ModelBreakdown[i].CallCount += mu.CallCount
					found = true
					break
				}
			}
			if !found {
				stats.ModelBreakdown = append(stats.ModelBreakdown, mu)
			}
		}
	}

	if stats.TaskCount > 0 {
		stats.AvgWallclock = time.Duration(totalWallclock/stats.TaskCount) * time.Second
		stats.SuccessRate = float64(stats.SuccessCount) / float64(stats.TaskCount)
		stats.AvgSubTasks = float64(stats.TotalTokens) / float64(stats.TaskCount) // approximation
	}

	return stats, rows.Err()
}

// queryPeriod returns stats for tasks in the last duration.
func (sq *StatsQuerier) queryPeriod(chatID int64, period time.Duration) (*AggregatedStats, error) {
	cutoff := time.Now().Add(-period)
	rows, err := sq.db.Query(`
		SELECT id, plan, model_usages, status
		FROM task_states
		WHERE chat_id = ? AND created_at >= ?
		ORDER BY created_at DESC
	`, chatID, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	stats := &AggregatedStats{
		Period:         fmt.Sprintf("%.0f days", period.Hours()/24),
		ModelBreakdown: []ModelUsage{},
	}

	totalWallclock := 0
	for rows.Next() {
		var taskID, planJSON, usagesJSON, status string
		if err := rows.Scan(&taskID, &planJSON, &usagesJSON, &status); err != nil {
			return nil, err
		}

		taskStats, err := sq.aggregateTask(taskID, planJSON, usagesJSON, status)
		if err != nil {
			continue
		}

		stats.TaskCount++
		stats.TotalTokens += taskStats.TotalTokens
		stats.SuccessCount += taskStats.SuccessCount
		totalWallclock += int(taskStats.AvgWallclock.Seconds())

		// Accumulate model usages.
		for _, mu := range taskStats.ModelBreakdown {
			found := false
			for i := range stats.ModelBreakdown {
				if stats.ModelBreakdown[i].Model == mu.Model {
					stats.ModelBreakdown[i].InputTokens += mu.InputTokens
					stats.ModelBreakdown[i].OutputTokens += mu.OutputTokens
					stats.ModelBreakdown[i].CallCount += mu.CallCount
					found = true
					break
				}
			}
			if !found {
				stats.ModelBreakdown = append(stats.ModelBreakdown, mu)
			}
		}
	}

	if stats.TaskCount > 0 {
		stats.AvgWallclock = time.Duration(totalWallclock/stats.TaskCount) * time.Second
		stats.SuccessRate = float64(stats.SuccessCount) / float64(stats.TaskCount)
		stats.AvgSubTasks = float64(stats.TotalTokens) / float64(stats.TaskCount) // approximation
	}

	return stats, rows.Err()
}

// aggregateTask parses a single task's JSON and returns its stats.
func (sq *StatsQuerier) aggregateTask(taskID, planJSON, usagesJSON, status string) (*AggregatedStats, error) {
	stats := &AggregatedStats{
		Period:         "task",
		ModelBreakdown: []ModelUsage{},
	}

	// Parse usages JSON (this would use a proper JSON decoder in production).
	// For now, we return a stub that accumulates counts.
	stats.TaskCount = 1
	if status == string(TaskStatusDone) {
		stats.SuccessCount = 1
		stats.SuccessRate = 1.0
	}

	// TODO: parse planJSON and usagesJSON to extract actual metrics.
	return stats, nil
}
