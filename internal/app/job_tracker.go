package app

import (
	"fmt"
	"sync"
	"time"
)

type BackgroundJobSnapshot struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Status      string     `json:"status"`
	StartedAt   time.Time  `json:"started_at"`
	EndedAt     *time.Time `json:"ended_at,omitempty"`
	DurationMS  int64      `json:"duration_ms"`
	Error       string     `json:"error,omitempty"`
	LastUpdated time.Time  `json:"last_updated"`
}

type BackgroundJobSummary struct {
	ActiveCount    int                     `json:"active_count"`
	RecentCount    int                     `json:"recent_count"`
	TotalStarted   int64                   `json:"total_started"`
	TotalCompleted int64                   `json:"total_completed"`
	TotalFailed    int64                   `json:"total_failed"`
	Active         []BackgroundJobSnapshot `json:"active"`
	Recent         []BackgroundJobSnapshot `json:"recent"`
}

type BackgroundJobTracker struct {
	mu             sync.Mutex
	nextID         int64
	active         map[string]BackgroundJobSnapshot
	recent         []BackgroundJobSnapshot
	maxRecent      int
	totalStarted   int64
	totalCompleted int64
	totalFailed    int64
}

var globalJobTracker = NewBackgroundJobTracker(50)

func NewBackgroundJobTracker(maxRecent int) *BackgroundJobTracker {
	if maxRecent <= 0 {
		maxRecent = 50
	}
	return &BackgroundJobTracker{
		active:    make(map[string]BackgroundJobSnapshot),
		maxRecent: maxRecent,
	}
}

func (t *BackgroundJobTracker) Start(name string) func(error) {
	if t == nil {
		return func(error) {}
	}
	t.mu.Lock()
	t.nextID++
	id := fmt.Sprintf("job-%d", t.nextID)
	now := time.Now()
	t.active[id] = BackgroundJobSnapshot{
		ID:          id,
		Name:        name,
		Status:      "running",
		StartedAt:   now,
		LastUpdated: now,
	}
	t.totalStarted++
	t.mu.Unlock()

	var once sync.Once
	return func(err error) {
		once.Do(func() {
			t.finish(id, err)
		})
	}
}

func (t *BackgroundJobTracker) finish(id string, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	job, ok := t.active[id]
	if !ok {
		return
	}
	delete(t.active, id)
	now := time.Now()
	job.EndedAt = &now
	job.LastUpdated = now
	job.DurationMS = now.Sub(job.StartedAt).Milliseconds()
	if err != nil {
		job.Status = "failed"
		job.Error = err.Error()
		t.totalFailed++
	} else {
		job.Status = "completed"
		t.totalCompleted++
	}
	t.recent = append([]BackgroundJobSnapshot{job}, t.recent...)
	if len(t.recent) > t.maxRecent {
		t.recent = t.recent[:t.maxRecent]
	}
}

func (t *BackgroundJobTracker) Summary() BackgroundJobSummary {
	if t == nil {
		return BackgroundJobSummary{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	active := make([]BackgroundJobSnapshot, 0, len(t.active))
	for _, job := range t.active {
		active = append(active, job)
	}
	recent := append([]BackgroundJobSnapshot(nil), t.recent...)
	return BackgroundJobSummary{
		ActiveCount:    len(active),
		RecentCount:    len(recent),
		TotalStarted:   t.totalStarted,
		TotalCompleted: t.totalCompleted,
		TotalFailed:    t.totalFailed,
		Active:         active,
		Recent:         recent,
	}
}
