package app

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

// ScheduledTask represents a cron-scheduled task
type ScheduledTask struct {
	ID         string    `json:"id"`
	ChatID     int64     `json:"chat_id"`
	ThreadID   int       `json:"thread_id"`
	Name       string    `json:"name"`
	CronExpr   string    `json:"cron_expr"`
	TaskType   string    `json:"task_type"` // "command" or "prompt"
	Payload    string    `json:"payload"`
	Enabled    bool      `json:"enabled"`
	CreatedAt  time.Time `json:"created_at"`
	LastRunAt  time.Time `json:"last_run_at"`
	LastResult string    `json:"last_result"`
	LastStatus string    `json:"last_status"` // "success", "failed", ""
	RunCount   int       `json:"run_count"`
	FailCount  int       `json:"fail_count"`
}

// CronScheduler manages scheduled task execution
type CronScheduler struct {
	cron     *cron.Cron
	bot      *TelegramBot
	client   Client
	entryMap map[string]cron.EntryID // task ID → cron entry ID
	mu       sync.RWMutex
}

// Scheduler limits
const (
	maxTasksPerUser   = 20
	minScheduleMinute = 5 // minimum interval: 5 minutes
	cronTaskTimeout   = 5 * time.Minute
)

// Global scheduler instance
var globalCronScheduler *CronScheduler

// NewCronScheduler creates a new CronScheduler
func NewCronScheduler(bot *TelegramBot, client Client) *CronScheduler {
	return &CronScheduler{
		cron:     cron.New(cron.WithSeconds()),
		bot:      bot,
		client:   client,
		entryMap: make(map[string]cron.EntryID),
	}
}

// Start loads tasks from DB and starts the scheduler
func (cs *CronScheduler) Start() error {
	if globalStorage == nil {
		return fmt.Errorf("storage not available")
	}

	sqliteStorage, ok := globalStorage.(*SQLiteStorage)
	if !ok {
		return fmt.Errorf("storage type not supported")
	}

	tasks, err := sqliteStorage.GetEnabledScheduledTasks()
	if err != nil {
		return fmt.Errorf("failed to load scheduled tasks: %w", err)
	}

	for _, task := range tasks {
		if err := cs.registerTask(task); err != nil {
			log.Printf("[cron] failed to register task %s: %v", task.ID, err)
		}
	}

	cs.cron.Start()
	log.Printf("[cron] scheduler started with %d tasks", len(tasks))
	return nil
}

// Stop gracefully stops the scheduler
func (cs *CronScheduler) Stop() {
	ctx := cs.cron.Stop()
	<-ctx.Done()
	log.Printf("[cron] scheduler stopped")
}

// Add creates and registers a new scheduled task
func (cs *CronScheduler) Add(task ScheduledTask) error {
	// Validate cron expression
	if _, err := cron.ParseStandard(task.CronExpr); err != nil {
		return fmt.Errorf("invalid cron expression %q: %w", task.CronExpr, err)
	}

	// Check per-user task limit
	if globalStorage != nil {
		if sqliteStorage, ok := globalStorage.(*SQLiteStorage); ok {
			existing, _ := sqliteStorage.GetScheduledTasksByChat(task.ChatID)
			if len(existing) >= maxTasksPerUser {
				return fmt.Errorf("task limit reached (%d/%d)", len(existing), maxTasksPerUser)
			}
		}
	}

	// Save to database
	if globalStorage != nil {
		if sqliteStorage, ok := globalStorage.(*SQLiteStorage); ok {
			if err := sqliteStorage.InsertScheduledTask(task); err != nil {
				return fmt.Errorf("failed to save task: %w", err)
			}
		}
	}

	// Register with cron
	if task.Enabled {
		if err := cs.registerTask(task); err != nil {
			return fmt.Errorf("failed to register task: %w", err)
		}
	}

	log.Printf("[cron] task added: %s (%s) for chat %d", task.Name, task.CronExpr, task.ChatID)
	return nil
}

// Remove deletes a scheduled task
func (cs *CronScheduler) Remove(taskID string, chatID int64) error {
	cs.mu.Lock()
	if entryID, ok := cs.entryMap[taskID]; ok {
		cs.cron.Remove(entryID)
		delete(cs.entryMap, taskID)
	}
	cs.mu.Unlock()

	if globalStorage != nil {
		if sqliteStorage, ok := globalStorage.(*SQLiteStorage); ok {
			return sqliteStorage.DeleteScheduledTask(taskID, chatID)
		}
	}
	return nil
}

// Pause disables a scheduled task
func (cs *CronScheduler) Pause(taskID string, chatID int64) error {
	cs.mu.Lock()
	if entryID, ok := cs.entryMap[taskID]; ok {
		cs.cron.Remove(entryID)
		delete(cs.entryMap, taskID)
	}
	cs.mu.Unlock()

	if globalStorage != nil {
		if sqliteStorage, ok := globalStorage.(*SQLiteStorage); ok {
			return sqliteStorage.UpdateScheduledTaskEnabled(taskID, chatID, false)
		}
	}
	return nil
}

// Resume re-enables a paused task
func (cs *CronScheduler) Resume(taskID string, chatID int64) error {
	if globalStorage == nil {
		return fmt.Errorf("storage not available")
	}

	sqliteStorage, ok := globalStorage.(*SQLiteStorage)
	if !ok {
		return fmt.Errorf("storage type not supported")
	}

	if err := sqliteStorage.UpdateScheduledTaskEnabled(taskID, chatID, true); err != nil {
		return err
	}

	// Reload and register the task
	task, err := sqliteStorage.GetScheduledTask(taskID, chatID)
	if err != nil {
		return err
	}

	return cs.registerTask(task)
}

// RunNow manually triggers a task immediately
func (cs *CronScheduler) RunNow(taskID string, chatID int64) error {
	if globalStorage == nil {
		return fmt.Errorf("storage not available")
	}

	sqliteStorage, ok := globalStorage.(*SQLiteStorage)
	if !ok {
		return fmt.Errorf("storage type not supported")
	}

	task, err := sqliteStorage.GetScheduledTask(taskID, chatID)
	if err != nil {
		return err
	}

	go cs.executeTask(task)
	return nil
}

// List returns all tasks for a specific chat
func (cs *CronScheduler) List(chatID int64) []ScheduledTask {
	if globalStorage == nil {
		return nil
	}

	sqliteStorage, ok := globalStorage.(*SQLiteStorage)
	if !ok {
		return nil
	}

	tasks, err := sqliteStorage.GetScheduledTasksByChat(chatID)
	if err != nil {
		log.Printf("[cron] failed to list tasks: %v", err)
		return nil
	}
	return tasks
}

// registerTask adds a task to the cron scheduler
func (cs *CronScheduler) registerTask(task ScheduledTask) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	// Remove existing entry if any
	if entryID, ok := cs.entryMap[task.ID]; ok {
		cs.cron.Remove(entryID)
	}

	// Use standard 5-field cron expression (robfig/cron with seconds needs wrapper)
	taskCopy := task
	entryID, err := cs.cron.AddFunc("0 "+task.CronExpr, func() {
		cs.executeTask(taskCopy)
	})
	if err != nil {
		return fmt.Errorf("failed to add cron entry: %w", err)
	}

	cs.entryMap[task.ID] = entryID
	return nil
}

// executeTask runs a scheduled task and sends the result
func (cs *CronScheduler) executeTask(task ScheduledTask) {
	startTime := time.Now()
	key := chatKey{chatID: task.ChatID, threadID: task.ThreadID}

	log.Printf("[cron] executing task %s (%s) for chat %d", task.ID, task.Name, task.ChatID)

	// Notify start
	if cs.bot != nil {
		startMsg := fmt.Sprintf("⏰ [排程] 開始執行: %s", task.Name)
		cs.bot.send(key, startMsg)
	}

	var result string
	var execErr error

	ctx, cancel := context.WithTimeout(context.Background(), cronTaskTimeout)
	defer cancel()

	switch task.TaskType {
	case "command":
		result, execErr = cs.executeCommand(ctx, task)
	case "prompt":
		result, execErr = cs.executePrompt(task)
	default:
		execErr = fmt.Errorf("unknown task type: %s", task.TaskType)
	}

	duration := time.Since(startTime)
	status := "success"
	if execErr != nil {
		status = "failed"
		result = execErr.Error()
	}

	// Update task stats in DB
	if globalStorage != nil {
		if sqliteStorage, ok := globalStorage.(*SQLiteStorage); ok {
			sqliteStorage.UpdateScheduledTaskResult(task.ID, status, truncateCronResult(result, 500), duration)
		}
	}

	// Send result to chat
	if cs.bot != nil {
		var msg string
		if status == "success" {
			msg = fmt.Sprintf("✅ [排程] %s 完成 (%.1fs)\n\n%s", task.Name, duration.Seconds(), truncateCronResult(result, 1000))
		} else {
			msg = fmt.Sprintf("❌ [排程] %s 失敗 (%.1fs)\n\n%s", task.Name, duration.Seconds(), result)
		}
		cs.bot.send(key, msg)
	}

	log.Printf("[cron] task %s %s (%.1fs)", task.ID, status, duration.Seconds())
}

// executeCommand runs a shell command via the Agent's CLI
func (cs *CronScheduler) executeCommand(ctx context.Context, task ScheduledTask) (string, error) {
	if cs.client == nil {
		return "", fmt.Errorf("CLI client not available")
	}

	// Wrap the command as a prompt asking Claude to execute it
	prompt := fmt.Sprintf("Execute this command and report the result concisely:\n```\n%s\n```", task.Payload)
	resp, err := cs.client.Call(ctx, prompt, ".", "", "")
	if err != nil {
		return "", err
	}
	return resp.TextContent, nil
}

// executePrompt sends a prompt to the Agent
func (cs *CronScheduler) executePrompt(task ScheduledTask) (string, error) {
	if cs.client == nil {
		return "", fmt.Errorf("CLI client not available")
	}

	// Get project directory for this chat's topic setting
	projectDir := "."
	if globalStorage != nil {
		if dir, err := globalStorage.GetTopicSetting(task.ChatID, task.ThreadID); err == nil && dir != "" {
			projectDir = dir
		}
	}

	agent := NewAgent(cs.client, projectDir, task.ChatID, task.ThreadID)
	result, err := agent.Run(task.Payload, nil)
	return result, err
}

// --- Natural language to cron expression ---

// ParseNaturalLanguageCron converts common natural language scheduling to cron expressions
func ParseNaturalLanguageCron(input string) (string, string) {
	lower := strings.ToLower(strings.TrimSpace(input))

	patterns := []struct {
		keywords []string
		cronExpr string
		name     string
	}{
		// Every N minutes/hours
		{[]string{"every 5 min", "每5分鐘", "每五分鐘"}, "*/5 * * * *", "every 5 minutes"},
		{[]string{"every 10 min", "每10分鐘", "每十分鐘"}, "*/10 * * * *", "every 10 minutes"},
		{[]string{"every 30 min", "每30分鐘", "每半小時"}, "*/30 * * * *", "every 30 minutes"},
		{[]string{"every hour", "每小時", "每一小時"}, "0 * * * *", "every hour"},
		{[]string{"every 6 hour", "每6小時", "每六小時"}, "0 */6 * * *", "every 6 hours"},

		// Daily
		{[]string{"每天早上九點", "每天早上9點", "every day 9am", "daily 9am", "daily 9:00"}, "0 9 * * *", "daily 9:00"},
		{[]string{"每天早上八點", "每天早上8點", "every day 8am", "daily 8am", "daily 8:00"}, "0 8 * * *", "daily 8:00"},
		{[]string{"每天早上十點", "每天早上10點", "daily 10am", "daily 10:00"}, "0 10 * * *", "daily 10:00"},
		{[]string{"每天中午", "every day noon", "daily noon", "daily 12:00"}, "0 12 * * *", "daily 12:00"},
		{[]string{"每天下午三點", "每天下午3點", "daily 3pm", "daily 15:00"}, "0 15 * * *", "daily 15:00"},
		{[]string{"每天下班前", "每天下午六點", "每天下午6點", "daily 6pm", "daily 18:00"}, "0 18 * * 1-5", "weekdays 18:00"},
		{[]string{"每天凌晨", "每天凌晨兩點", "每天凌晨2點", "daily 2am"}, "0 2 * * *", "daily 2:00"},

		// Weekly
		{[]string{"每週一早上", "每周一早上", "every monday morning", "weekly monday"}, "0 8 * * 1", "Monday 8:00"},
		{[]string{"每週五下午", "每周五下午", "every friday afternoon"}, "0 15 * * 5", "Friday 15:00"},
		{[]string{"每週日", "每周日", "every sunday"}, "0 9 * * 0", "Sunday 9:00"},

		// Monthly
		{[]string{"每月1號", "每月一號", "every month 1st", "monthly"}, "0 0 1 * *", "1st of month"},
		{[]string{"每月15號", "每月十五號", "every month 15th"}, "0 0 15 * *", "15th of month"},
	}

	for _, p := range patterns {
		for _, kw := range p.keywords {
			if strings.Contains(lower, kw) {
				return p.cronExpr, p.name
			}
		}
	}

	// If no pattern matched, check if it's already a valid cron expression
	if _, err := cron.ParseStandard(input); err == nil {
		return input, "custom schedule"
	}

	return "", ""
}

func truncateCronResult(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
