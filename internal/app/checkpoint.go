package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Checkpoint represents a point-in-time snapshot of the project state
type Checkpoint struct {
	ID              string            `json:"id" db:"id"`
	Timestamp       time.Time         `json:"timestamp" db:"timestamp"`
	ProjectDir      string            `json:"project_dir" db:"project_dir"`
	GitCommitHash   string            `json:"git_commit_hash" db:"git_commit_hash"`
	GitBranch       string            `json:"git_branch" db:"git_branch"`
	GitStashRef     string            `json:"git_stash_ref" db:"git_stash_ref"`
	Description     string            `json:"description" db:"description"`
	TriggerType     CheckpointTrigger `json:"trigger_type" db:"trigger_type"`
	SessionID       string            `json:"session_id" db:"session_id"`
	ChatID          int64             `json:"chat_id" db:"chat_id"`
	FileSnapshots   map[string]string `json:"file_snapshots" db:"files_snapshot_json"`
	PreCondition    string            `json:"pre_condition" db:"pre_condition"`
	DangerousOp     string            `json:"dangerous_op" db:"dangerous_op"`
	CreatedBy       string            `json:"created_by" db:"created_by"`
	Size            int64             `json:"size" db:"size"`
	IsActive        bool              `json:"is_active" db:"is_active"`
}

// CheckpointTrigger represents different types of checkpoint triggers
type CheckpointTrigger string

const (
	TriggerAuto      CheckpointTrigger = "auto"
	TriggerManual    CheckpointTrigger = "manual"
	TriggerPreDanger CheckpointTrigger = "pre_danger"
	TriggerScheduled CheckpointTrigger = "scheduled"
	TriggerMilestone CheckpointTrigger = "milestone"
)

// DangerousOperation represents operations that require checkpoints
type DangerousOperation struct {
	Tool        string   `json:"tool"`
	Operation   string   `json:"operation"`
	Patterns    []string `json:"patterns"`
	RiskLevel   RiskLevel `json:"risk_level"`
	Description string   `json:"description"`
}

// RiskLevel represents the risk level of an operation
type RiskLevel int

const (
	RiskLow RiskLevel = iota
	RiskMedium
	RiskHigh
	RiskCritical
)

var riskLevelNames = map[RiskLevel]string{
	RiskLow:      "low",
	RiskMedium:   "medium",
	RiskHigh:     "high",
	RiskCritical: "critical",
}

func (rl RiskLevel) String() string {
	if name, ok := riskLevelNames[rl]; ok {
		return name
	}
	return "unknown"
}

// CheckpointManager manages project snapshots and restoration
type CheckpointManager struct {
	storage    Storage
	gitManager *GitManager
	config     CheckpointConfig
	enabled    bool
}

// CheckpointConfig holds configuration for checkpoint behavior
type CheckpointConfig struct {
	MaxCheckpoints      int           `json:"max_checkpoints"`
	AutoCleanupAfter    time.Duration `json:"auto_cleanup_after"`
	MaxSnapshotSize     int64         `json:"max_snapshot_size"`
	BackupUntracked     bool          `json:"backup_untracked"`
	GitIntegration      bool          `json:"git_integration"`
	DangerousOperations []DangerousOperation `json:"dangerous_operations"`
}

// NewCheckpointManager creates a new checkpoint manager
func NewCheckpointManager(storage Storage, gitManager *GitManager, config CheckpointConfig) *CheckpointManager {
	if config.MaxCheckpoints == 0 {
		config.MaxCheckpoints = 50 // Default max checkpoints
	}
	if config.AutoCleanupAfter == 0 {
		config.AutoCleanupAfter = 7 * 24 * time.Hour // Default 7 days
	}
	if config.MaxSnapshotSize == 0 {
		config.MaxSnapshotSize = 100 * 1024 * 1024 // Default 100MB
	}

	// Define default dangerous operations if not configured
	if len(config.DangerousOperations) == 0 {
		config.DangerousOperations = getDefaultDangerousOperations()
	}

	cm := &CheckpointManager{
		storage:    storage,
		gitManager: gitManager,
		config:     config,
		enabled:    true,
	}

	// Initialize database tables
	if err := cm.initializeTables(); err != nil {
		log.Printf("Warning: Failed to initialize checkpoint tables: %v", err)
	}

	return cm
}

// getDefaultDangerousOperations returns the default set of dangerous operations
func getDefaultDangerousOperations() []DangerousOperation {
	return []DangerousOperation{
		{
			Tool:        "Write",
			Operation:   "file_creation",
			Patterns:    []string{"*"},
			RiskLevel:   RiskMedium,
			Description: "Creating new files",
		},
		{
			Tool:        "Edit",
			Operation:   "file_modification",
			Patterns:    []string{"*"},
			RiskLevel:   RiskMedium,
			Description: "Modifying existing files",
		},
		{
			Tool:        "Bash",
			Operation:   "file_deletion",
			Patterns:    []string{"rm", "rmdir", "del"},
			RiskLevel:   RiskHigh,
			Description: "Deleting files or directories",
		},
		{
			Tool:        "Bash",
			Operation:   "file_movement",
			Patterns:    []string{"mv", "move", "cp"},
			RiskLevel:   RiskMedium,
			Description: "Moving or copying files",
		},
		{
			Tool:        "Bash",
			Operation:   "git_operations",
			Patterns:    []string{"git reset", "git checkout", "git rebase", "git merge"},
			RiskLevel:   RiskHigh,
			Description: "Destructive Git operations",
		},
		{
			Tool:        "Bash",
			Operation:   "system_modification",
			Patterns:    []string{"chmod", "chown", "sudo"},
			RiskLevel:   RiskCritical,
			Description: "System permission modifications",
		},
		{
			Tool:        "Bash",
			Operation:   "package_management",
			Patterns:    []string{"npm install", "pip install", "brew install", "apt-get"},
			RiskLevel:   RiskMedium,
			Description: "Package installation/removal",
		},
	}
}

// initializeTables creates the necessary database tables for checkpoints
func (cm *CheckpointManager) initializeTables() error {
	query := `
	CREATE TABLE IF NOT EXISTS checkpoints (
		id TEXT PRIMARY KEY,
		timestamp DATETIME NOT NULL,
		project_dir TEXT NOT NULL,
		git_commit_hash TEXT,
		git_branch TEXT,
		git_stash_ref TEXT,
		description TEXT,
		trigger_type TEXT NOT NULL,
		session_id TEXT,
		chat_id INTEGER,
		files_snapshot_json TEXT,
		pre_condition TEXT,
		dangerous_op TEXT,
		created_by TEXT,
		size INTEGER DEFAULT 0,
		is_active BOOLEAN DEFAULT 1
	);

	CREATE INDEX IF NOT EXISTS idx_checkpoints_project ON checkpoints(project_dir);
	CREATE INDEX IF NOT EXISTS idx_checkpoints_timestamp ON checkpoints(timestamp);
	CREATE INDEX IF NOT EXISTS idx_checkpoints_trigger ON checkpoints(trigger_type);
	CREATE INDEX IF NOT EXISTS idx_checkpoints_chat ON checkpoints(chat_id);
	`

	// Get underlying database connection
	db := cm.storage.GetDB().(*sql.DB)
	_, err := db.Exec(query)
	return err
}

// ShouldCreateCheckpoint determines if a checkpoint should be created before executing a tool
func (cm *CheckpointManager) ShouldCreateCheckpoint(toolName string, input map[string]interface{}) (bool, DangerousOperation, string) {
	if !cm.enabled {
		return false, DangerousOperation{}, ""
	}

	for _, dangerousOp := range cm.config.DangerousOperations {
		if dangerousOp.Tool != toolName {
			continue
		}

		switch toolName {
		case "Write", "Edit":
			// Always checkpoint before file modifications
			return true, dangerousOp, fmt.Sprintf("Before %s operation", toolName)

		case "Bash":
			if command, ok := input["command"].(string); ok {
				if cm.isCommandDangerous(command, dangerousOp.Patterns) {
					return true, dangerousOp, fmt.Sprintf("Before dangerous bash command: %s", command)
				}
			}
		}
	}

	return false, DangerousOperation{}, ""
}

// isCommandDangerous checks if a bash command matches dangerous patterns
func (cm *CheckpointManager) isCommandDangerous(command string, patterns []string) bool {
	commandLower := strings.ToLower(command)
	for _, pattern := range patterns {
		if strings.Contains(commandLower, strings.ToLower(pattern)) {
			return true
		}
	}
	return false
}

// CreateCheckpoint creates a new checkpoint
func (cm *CheckpointManager) CreateCheckpoint(projectDir, description string, triggerType CheckpointTrigger, sessionID string, chatID int64, dangerousOp string) (*Checkpoint, error) {
	if !cm.enabled {
		return nil, fmt.Errorf("checkpoint manager is disabled")
	}

	// Validate project directory
	if _, err := os.Stat(projectDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("project directory does not exist: %s", projectDir)
	}

	// Generate unique ID
	checkpointID := cm.generateCheckpointID(projectDir, triggerType)

	checkpoint := &Checkpoint{
		ID:           checkpointID,
		Timestamp:    time.Now(),
		ProjectDir:   projectDir,
		Description:  description,
		TriggerType:  triggerType,
		SessionID:    sessionID,
		ChatID:       chatID,
		DangerousOp:  dangerousOp,
		CreatedBy:    "alice-agent",
		IsActive:     true,
		FileSnapshots: make(map[string]string),
	}

	// Get Git information if available
	if cm.config.GitIntegration && cm.gitManager != nil {
		if gitState, err := cm.gitManager.GetGitState(projectDir); err == nil {
			checkpoint.GitCommitHash = gitState.CommitHash
			checkpoint.GitBranch = gitState.Branch
		}

		// Create Git stash if there are changes
		if stashRef, err := cm.createGitStash(projectDir, checkpointID); err == nil {
			checkpoint.GitStashRef = stashRef
		}
	}

	// Create file snapshots for untracked/modified files
	if cm.config.BackupUntracked {
		if err := cm.createFileSnapshots(projectDir, checkpoint); err != nil {
			log.Printf("Warning: Failed to create file snapshots: %v", err)
		}
	}

	// Calculate checkpoint size
	checkpoint.Size = cm.calculateCheckpointSize(checkpoint)

	// Validate size limit
	if checkpoint.Size > cm.config.MaxSnapshotSize {
		return nil, fmt.Errorf("checkpoint size (%d bytes) exceeds maximum allowed (%d bytes)", checkpoint.Size, cm.config.MaxSnapshotSize)
	}

	// Store checkpoint in database
	if err := cm.storeCheckpoint(checkpoint); err != nil {
		return nil, fmt.Errorf("failed to store checkpoint: %w", err)
	}

	// Clean up old checkpoints
	go cm.cleanupOldCheckpoints(projectDir)

	log.Printf("Created checkpoint %s for project %s (trigger: %s, size: %d bytes)",
		checkpointID, projectDir, triggerType, checkpoint.Size)

	return checkpoint, nil
}

// generateCheckpointID generates a unique ID for a checkpoint
func (cm *CheckpointManager) generateCheckpointID(projectDir string, triggerType CheckpointTrigger) string {
	data := fmt.Sprintf("%s-%s-%d", projectDir, triggerType, time.Now().UnixNano())
	hash := sha256.Sum256([]byte(data))
	return fmt.Sprintf("cp_%x", hash[:8])
}

// createGitStash creates a Git stash for the current state
func (cm *CheckpointManager) createGitStash(projectDir, checkpointID string) (string, error) {
	cmd := exec.Command("git", "stash", "create", fmt.Sprintf("checkpoint-%s", checkpointID))
	cmd.Dir = projectDir

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to create git stash: %w", err)
	}

	stashRef := strings.TrimSpace(string(output))
	if stashRef == "" {
		return "", fmt.Errorf("no changes to stash")
	}

	log.Printf("Created git stash %s for checkpoint %s", stashRef, checkpointID)
	return stashRef, nil
}

// createFileSnapshots creates backup copies of important files
func (cm *CheckpointManager) createFileSnapshots(projectDir string, checkpoint *Checkpoint) error {
	// Get list of files to backup
	filesToBackup, err := cm.getFilesToBackup(projectDir)
	if err != nil {
		return err
	}

	snapshotDir := cm.getSnapshotDir(checkpoint.ID)
	if err := os.MkdirAll(snapshotDir, 0755); err != nil {
		return fmt.Errorf("failed to create snapshot directory: %w", err)
	}

	for _, filePath := range filesToBackup {
		relativePath, err := filepath.Rel(projectDir, filePath)
		if err != nil {
			continue
		}

		// Read original file
		content, err := ioutil.ReadFile(filePath)
		if err != nil {
			log.Printf("Warning: Failed to read file %s: %v", filePath, err)
			continue
		}

		// Create backup path
		backupPath := filepath.Join(snapshotDir, relativePath)
		if err := os.MkdirAll(filepath.Dir(backupPath), 0755); err != nil {
			log.Printf("Warning: Failed to create backup directory for %s: %v", relativePath, err)
			continue
		}

		// Write backup file
		if err := ioutil.WriteFile(backupPath, content, 0644); err != nil {
			log.Printf("Warning: Failed to backup file %s: %v", relativePath, err)
			continue
		}

		checkpoint.FileSnapshots[relativePath] = backupPath
	}

	return nil
}

// getFilesToBackup determines which files should be backed up
func (cm *CheckpointManager) getFilesToBackup(projectDir string) ([]string, error) {
	var files []string

	// Get modified/untracked files from Git if available
	if cm.gitManager != nil {
		if gitState, err := cm.gitManager.GetGitState(projectDir); err == nil && gitState.IsDirty {
			for _, file := range gitState.ModifiedFiles {
				fullPath := filepath.Join(projectDir, file)
				if _, err := os.Stat(fullPath); err == nil {
					files = append(files, fullPath)
				}
			}
		}
	}

	// Backup important configuration files regardless of Git status
	importantFiles := []string{
		"package.json", "go.mod", "requirements.txt", "Gemfile",
		"Dockerfile", "docker-compose.yml", ".env", "config.json",
		"Makefile", "README.md",
	}

	for _, filename := range importantFiles {
		fullPath := filepath.Join(projectDir, filename)
		if _, err := os.Stat(fullPath); err == nil {
			files = append(files, fullPath)
		}
	}

	return files, nil
}

// getSnapshotDir returns the directory for storing file snapshots
func (cm *CheckpointManager) getSnapshotDir(checkpointID string) string {
	return filepath.Join(os.TempDir(), "alice-checkpoints", checkpointID)
}

// calculateCheckpointSize calculates the total size of a checkpoint
func (cm *CheckpointManager) calculateCheckpointSize(checkpoint *Checkpoint) int64 {
	var size int64

	// Calculate size of file snapshots
	for _, backupPath := range checkpoint.FileSnapshots {
		if info, err := os.Stat(backupPath); err == nil {
			size += info.Size()
		}
	}

	// Add metadata size (approximate)
	metadataJSON, _ := json.Marshal(checkpoint)
	size += int64(len(metadataJSON))

	return size
}

// storeCheckpoint stores a checkpoint in the database
func (cm *CheckpointManager) storeCheckpoint(checkpoint *Checkpoint) error {
	fileSnapshotsJSON, err := json.Marshal(checkpoint.FileSnapshots)
	if err != nil {
		return fmt.Errorf("failed to marshal file snapshots: %w", err)
	}

	query := `
		INSERT INTO checkpoints (
			id, timestamp, project_dir, git_commit_hash, git_branch, git_stash_ref,
			description, trigger_type, session_id, chat_id, files_snapshot_json,
			pre_condition, dangerous_op, created_by, size, is_active
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	// Get underlying database connection
	db := cm.storage.GetDB().(*sql.DB)
	_, err = db.Exec(query,
		checkpoint.ID, checkpoint.Timestamp, checkpoint.ProjectDir,
		checkpoint.GitCommitHash, checkpoint.GitBranch, checkpoint.GitStashRef,
		checkpoint.Description, string(checkpoint.TriggerType), checkpoint.SessionID,
		checkpoint.ChatID, string(fileSnapshotsJSON), checkpoint.PreCondition,
		checkpoint.DangerousOp, checkpoint.CreatedBy, checkpoint.Size, checkpoint.IsActive,
	)
	return err
}

// ListCheckpoints retrieves checkpoints for a project
func (cm *CheckpointManager) ListCheckpoints(projectDir string, limit int) ([]Checkpoint, error) {
	var checkpoints []Checkpoint

	query := `
		SELECT id, timestamp, project_dir, git_commit_hash, git_branch, git_stash_ref,
			   description, trigger_type, session_id, chat_id, files_snapshot_json,
			   pre_condition, dangerous_op, created_by, size, is_active
		FROM checkpoints
		WHERE project_dir = ? AND is_active = 1
		ORDER BY timestamp DESC
	`

	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	db := cm.storage.GetDB().(*sql.DB)
	rows, err := db.Query(query, projectDir)
	if err != nil {
		return nil, fmt.Errorf("failed to query checkpoints: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var cp Checkpoint
		var triggerType string
		var fileSnapshotsJSON string

		err := rows.Scan(
			&cp.ID, &cp.Timestamp, &cp.ProjectDir, &cp.GitCommitHash,
			&cp.GitBranch, &cp.GitStashRef, &cp.Description, &triggerType,
			&cp.SessionID, &cp.ChatID, &fileSnapshotsJSON, &cp.PreCondition,
			&cp.DangerousOp, &cp.CreatedBy, &cp.Size, &cp.IsActive,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan checkpoint: %w", err)
		}

		cp.TriggerType = CheckpointTrigger(triggerType)

		// Unmarshal file snapshots
		if fileSnapshotsJSON != "" {
			if err := json.Unmarshal([]byte(fileSnapshotsJSON), &cp.FileSnapshots); err != nil {
				log.Printf("Warning: Failed to unmarshal file snapshots for checkpoint %s: %v", cp.ID, err)
				cp.FileSnapshots = make(map[string]string)
			}
		} else {
			cp.FileSnapshots = make(map[string]string)
		}

		checkpoints = append(checkpoints, cp)
	}

	return checkpoints, rows.Err()
}

// RestoreCheckpoint restores a project to a previous checkpoint
func (cm *CheckpointManager) RestoreCheckpoint(checkpointID, projectDir string) error {
	if !cm.enabled {
		return fmt.Errorf("checkpoint manager is disabled")
	}

	// Find the checkpoint
	checkpoint, err := cm.getCheckpoint(checkpointID)
	if err != nil {
		return fmt.Errorf("failed to find checkpoint: %w", err)
	}

	if checkpoint.ProjectDir != projectDir {
		return fmt.Errorf("checkpoint project directory mismatch")
	}

	log.Printf("Restoring checkpoint %s for project %s", checkpointID, projectDir)

	// Restore Git state if available
	if checkpoint.GitStashRef != "" && cm.config.GitIntegration {
		if err := cm.restoreGitState(projectDir, checkpoint); err != nil {
			log.Printf("Warning: Failed to restore Git state: %v", err)
		}
	}

	// Restore file snapshots
	if len(checkpoint.FileSnapshots) > 0 {
		if err := cm.restoreFileSnapshots(projectDir, checkpoint); err != nil {
			return fmt.Errorf("failed to restore file snapshots: %w", err)
		}
	}

	log.Printf("Successfully restored checkpoint %s", checkpointID)
	return nil
}

// restoreGitState restores the Git state from a checkpoint
func (cm *CheckpointManager) restoreGitState(projectDir string, checkpoint *Checkpoint) error {
	// Apply the stash
	if checkpoint.GitStashRef != "" {
		cmd := exec.Command("git", "stash", "apply", checkpoint.GitStashRef)
		cmd.Dir = projectDir

		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to apply git stash %s: %w\nOutput: %s",
				checkpoint.GitStashRef, err, string(output))
		}

		log.Printf("Applied git stash %s for checkpoint %s", checkpoint.GitStashRef, checkpoint.ID)
	}

	return nil
}

// restoreFileSnapshots restores files from snapshots
func (cm *CheckpointManager) restoreFileSnapshots(projectDir string, checkpoint *Checkpoint) error {
	for relativePath, backupPath := range checkpoint.FileSnapshots {
		originalPath := filepath.Join(projectDir, relativePath)

		// Read backup content
		content, err := ioutil.ReadFile(backupPath)
		if err != nil {
			log.Printf("Warning: Failed to read backup file %s: %v", backupPath, err)
			continue
		}

		// Ensure target directory exists
		if err := os.MkdirAll(filepath.Dir(originalPath), 0755); err != nil {
			log.Printf("Warning: Failed to create directory for %s: %v", originalPath, err)
			continue
		}

		// Restore file
		if err := ioutil.WriteFile(originalPath, content, 0644); err != nil {
			log.Printf("Warning: Failed to restore file %s: %v", originalPath, err)
			continue
		}

		log.Printf("Restored file: %s", relativePath)
	}

	return nil
}

// getCheckpoint retrieves a checkpoint by ID
func (cm *CheckpointManager) getCheckpoint(checkpointID string) (*Checkpoint, error) {
	query := `
		SELECT id, timestamp, project_dir, git_commit_hash, git_branch, git_stash_ref,
			   description, trigger_type, session_id, chat_id, files_snapshot_json,
			   pre_condition, dangerous_op, created_by, size, is_active
		FROM checkpoints
		WHERE id = ? AND is_active = 1
	`

	db := cm.storage.GetDB().(*sql.DB)
	row := db.QueryRow(query, checkpointID)

	var cp Checkpoint
	var triggerType string
	var fileSnapshotsJSON string

	err := row.Scan(
		&cp.ID, &cp.Timestamp, &cp.ProjectDir, &cp.GitCommitHash,
		&cp.GitBranch, &cp.GitStashRef, &cp.Description, &triggerType,
		&cp.SessionID, &cp.ChatID, &fileSnapshotsJSON, &cp.PreCondition,
		&cp.DangerousOp, &cp.CreatedBy, &cp.Size, &cp.IsActive,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("checkpoint not found: %s", checkpointID)
		}
		return nil, fmt.Errorf("failed to query checkpoint: %w", err)
	}

	cp.TriggerType = CheckpointTrigger(triggerType)

	// Unmarshal file snapshots
	if fileSnapshotsJSON != "" {
		if err := json.Unmarshal([]byte(fileSnapshotsJSON), &cp.FileSnapshots); err != nil {
			log.Printf("Warning: Failed to unmarshal file snapshots for checkpoint %s: %v", cp.ID, err)
			cp.FileSnapshots = make(map[string]string)
		}
	} else {
		cp.FileSnapshots = make(map[string]string)
	}

	return &cp, nil
}

// DeleteCheckpoint marks a checkpoint as inactive (soft delete)
func (cm *CheckpointManager) DeleteCheckpoint(checkpointID string) error {
	// Mark as inactive in database
	query := `UPDATE checkpoints SET is_active = 0 WHERE id = ?`
	db := cm.storage.GetDB().(*sql.DB)
	if _, err := db.Exec(query, checkpointID); err != nil {
		return fmt.Errorf("failed to delete checkpoint from database: %w", err)
	}

	// Clean up file snapshots
	snapshotDir := cm.getSnapshotDir(checkpointID)
	if err := os.RemoveAll(snapshotDir); err != nil {
		log.Printf("Warning: Failed to clean up snapshot directory %s: %v", snapshotDir, err)
	}

	log.Printf("Deleted checkpoint %s", checkpointID)
	return nil
}

// cleanupOldCheckpoints removes old checkpoints to maintain limits
func (cm *CheckpointManager) cleanupOldCheckpoints(projectDir string) {
	// Get all checkpoints for the project
	checkpoints, err := cm.ListCheckpoints(projectDir, 0)
	if err != nil {
		log.Printf("Warning: Failed to list checkpoints for cleanup: %v", err)
		return
	}

	// Remove checkpoints beyond the limit
	if len(checkpoints) > cm.config.MaxCheckpoints {
		toDelete := checkpoints[cm.config.MaxCheckpoints:]
		for _, checkpoint := range toDelete {
			if err := cm.DeleteCheckpoint(checkpoint.ID); err != nil {
				log.Printf("Warning: Failed to delete old checkpoint %s: %v", checkpoint.ID, err)
			}
		}
	}

	// Remove checkpoints older than the retention period
	cutoffTime := time.Now().Add(-cm.config.AutoCleanupAfter)
	for _, checkpoint := range checkpoints {
		if checkpoint.Timestamp.Before(cutoffTime) {
			if err := cm.DeleteCheckpoint(checkpoint.ID); err != nil {
				log.Printf("Warning: Failed to delete expired checkpoint %s: %v", checkpoint.ID, err)
			}
		}
	}
}

// GetCheckpointStats returns statistics about checkpoints
func (cm *CheckpointManager) GetCheckpointStats(projectDir string) (map[string]interface{}, error) {
	query := `
		SELECT
			COUNT(*) as total_checkpoints,
			SUM(size) as total_size,
			COUNT(CASE WHEN trigger_type = 'auto' THEN 1 END) as auto_checkpoints,
			COUNT(CASE WHEN trigger_type = 'manual' THEN 1 END) as manual_checkpoints,
			COUNT(CASE WHEN trigger_type = 'pre_danger' THEN 1 END) as pre_danger_checkpoints,
			MIN(timestamp) as oldest_checkpoint,
			MAX(timestamp) as newest_checkpoint
		FROM checkpoints
		WHERE project_dir = ? AND is_active = 1
	`

	db := cm.storage.GetDB().(*sql.DB)
	row := db.QueryRow(query, projectDir)

	var stats struct {
		Total       int64     `db:"total_checkpoints"`
		TotalSize   int64     `db:"total_size"`
		Auto        int64     `db:"auto_checkpoints"`
		Manual      int64     `db:"manual_checkpoints"`
		PreDanger   int64     `db:"pre_danger_checkpoints"`
		Oldest      time.Time `db:"oldest_checkpoint"`
		Newest      time.Time `db:"newest_checkpoint"`
	}

	err := row.Scan(&stats.Total, &stats.TotalSize, &stats.Auto, &stats.Manual, &stats.PreDanger, &stats.Oldest, &stats.Newest)
	if err != nil {
		return nil, fmt.Errorf("failed to get checkpoint stats: %w", err)
	}

	return map[string]interface{}{
		"total_checkpoints":     stats.Total,
		"total_size_bytes":      stats.TotalSize,
		"auto_checkpoints":      stats.Auto,
		"manual_checkpoints":    stats.Manual,
		"pre_danger_checkpoints": stats.PreDanger,
		"oldest_checkpoint":     stats.Oldest,
		"newest_checkpoint":     stats.Newest,
		"max_checkpoints":       cm.config.MaxCheckpoints,
		"cleanup_after_days":    int(cm.config.AutoCleanupAfter.Hours() / 24),
	}, nil
}

// Enable enables the checkpoint manager
func (cm *CheckpointManager) Enable() {
	cm.enabled = true
	log.Printf("Checkpoint manager enabled")
}

// Disable disables the checkpoint manager
func (cm *CheckpointManager) Disable() {
	cm.enabled = false
	log.Printf("Checkpoint manager disabled")
}

// IsEnabled returns whether the checkpoint manager is enabled
func (cm *CheckpointManager) IsEnabled() bool {
	return cm.enabled
}

// Global checkpoint manager instance
var globalCheckpointManager *CheckpointManager

// InitCheckpointManager initializes the global checkpoint manager
func InitCheckpointManager(storage Storage, gitManager *GitManager) error {
	config := CheckpointConfig{
		MaxCheckpoints:   50,
		AutoCleanupAfter: 7 * 24 * time.Hour, // 7 days
		MaxSnapshotSize:  100 * 1024 * 1024,  // 100MB
		BackupUntracked:  true,
		GitIntegration:   true,
		DangerousOperations: getDefaultDangerousOperations(),
	}

	globalCheckpointManager = NewCheckpointManager(storage, gitManager, config)
	log.Printf("Checkpoint manager initialized successfully")
	return nil
}