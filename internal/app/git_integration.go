package app

import (
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	alicev1 "claude-tg-agent/gen/go/alice/v1"
)

// GitManager handles all Git operations and state detection
type GitManager struct {
	mu    sync.RWMutex
	cache map[string]*CachedGitState
}

// CachedGitState represents cached Git state with TTL
type CachedGitState struct {
	State     *alicev1.GitState
	Timestamp time.Time
	TTL       time.Duration
}

// NewGitManager creates a new GitManager instance
func NewGitManager() *GitManager {
	return &GitManager{
		cache: make(map[string]*CachedGitState),
	}
}

// GetGitState retrieves current Git state for a given project directory
func (gm *GitManager) GetGitState(projectDir string) (*alicev1.GitState, error) {
	gm.mu.RLock()
	cached, exists := gm.cache[projectDir]
	gm.mu.RUnlock()

	// Check if cache is valid
	if exists && time.Since(cached.Timestamp) < cached.TTL {
		return cached.State, nil
	}

	// Get fresh Git state
	state, err := gm.detectGitState(projectDir)
	if err != nil {
		return nil, err
	}

	// Cache the result
	gm.mu.Lock()
	gm.cache[projectDir] = &CachedGitState{
		State:     state,
		Timestamp: time.Now(),
		TTL:       30 * time.Second, // Cache for 30 seconds
	}
	gm.mu.Unlock()

	return state, nil
}

// detectGitState performs actual Git state detection
func (gm *GitManager) detectGitState(projectDir string) (*alicev1.GitState, error) {
	// Check if directory is a Git repository
	if !gm.isGitRepository(projectDir) {
		return nil, fmt.Errorf("directory %s is not a Git repository", projectDir)
	}

	gitState := &alicev1.GitState{}

	// Get current branch
	if branch, err := gm.getCurrentBranch(projectDir); err == nil {
		gitState.Branch = branch
	}

	// Get commit hash
	if hash, err := gm.getCommitHash(projectDir); err == nil {
		gitState.CommitHash = hash
	}

	// Get remote URL
	if remoteURL, err := gm.getRemoteURL(projectDir); err == nil {
		gitState.RemoteUrl = remoteURL
	}

	// Check if repository has uncommitted changes
	isDirty, modifiedFiles, err := gm.checkDirtyState(projectDir)
	if err == nil {
		gitState.IsDirty = isDirty
		gitState.ModifiedFiles = modifiedFiles
	}

	return gitState, nil
}

// isGitRepository checks if the given directory is a Git repository
func (gm *GitManager) isGitRepository(projectDir string) bool {
	cmd := exec.Command("git", "rev-parse", "--git-dir")
	cmd.Dir = projectDir
	err := cmd.Run()
	return err == nil
}

// getCurrentBranch gets the current Git branch
func (gm *GitManager) getCurrentBranch(projectDir string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = projectDir
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get current branch: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// getCommitHash gets the current commit hash (short)
func (gm *GitManager) getCommitHash(projectDir string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--short", "HEAD")
	cmd.Dir = projectDir
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get commit hash: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// getRemoteURL gets the remote repository URL
func (gm *GitManager) getRemoteURL(projectDir string) (string, error) {
	cmd := exec.Command("git", "remote", "get-url", "origin")
	cmd.Dir = projectDir
	output, err := cmd.Output()
	if err != nil {
		// Try to get any remote if origin doesn't exist
		cmd = exec.Command("git", "remote")
		cmd.Dir = projectDir
		remotes, err := cmd.Output()
		if err != nil {
			return "", fmt.Errorf("no remotes found: %w", err)
		}

		remoteList := strings.Fields(strings.TrimSpace(string(remotes)))
		if len(remoteList) == 0 {
			return "", fmt.Errorf("no remotes configured")
		}

		// Get URL for first available remote
		cmd = exec.Command("git", "remote", "get-url", remoteList[0])
		cmd.Dir = projectDir
		output, err = cmd.Output()
		if err != nil {
			return "", fmt.Errorf("failed to get remote URL: %w", err)
		}
	}
	return strings.TrimSpace(string(output)), nil
}

// checkDirtyState checks if repository has uncommitted changes
func (gm *GitManager) checkDirtyState(projectDir string) (bool, []string, error) {
	// Check for modified, added, deleted files
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = projectDir
	output, err := cmd.Output()
	if err != nil {
		return false, nil, fmt.Errorf("failed to get Git status: %w", err)
	}

	statusOutput := strings.TrimSpace(string(output))
	if statusOutput == "" {
		return false, []string{}, nil
	}

	// Parse modified files
	lines := strings.Split(statusOutput, "\n")
	modifiedFiles := make([]string, 0, len(lines))

	for _, line := range lines {
		if len(line) > 2 {
			// Extract filename from Git status format
			filename := strings.TrimSpace(line[2:])
			// Handle renamed files (format: "old -> new")
			if strings.Contains(filename, " -> ") {
				parts := strings.Split(filename, " -> ")
				if len(parts) == 2 {
					filename = parts[1]
				}
			}
			modifiedFiles = append(modifiedFiles, filename)
		}
	}

	return true, modifiedFiles, nil
}

// GitOperationManager handles Git operations triggered by AI agents
type GitOperationManager struct {
	gitManager *GitManager
	safetyMode bool
	allowedOps map[string]bool
}

// NewGitOperationManager creates a new Git operation manager
func NewGitOperationManager(gitManager *GitManager) *GitOperationManager {
	return &GitOperationManager{
		gitManager: gitManager,
		safetyMode: true, // Enable safety mode by default
		allowedOps: map[string]bool{
			"status": true,
			"diff":   true,
			"log":    true,
			"show":   true,
			"add":    false, // Requires approval
			"commit": false, // Requires approval
			"push":   false, // Requires approval
			"pull":   false, // Requires approval
		},
	}
}

// ExecuteGitOperation executes a Git operation with safety checks
func (gom *GitOperationManager) ExecuteGitOperation(projectDir, operation string, args []string) (string, error) {
	// Safety check: verify operation is allowed
	if gom.safetyMode {
		allowed, exists := gom.allowedOps[operation]
		if !exists || !allowed {
			return "", fmt.Errorf("Git operation '%s' is not allowed in safety mode", operation)
		}
	}

	// Additional safety checks for destructive operations
	if gom.isDestructiveOperation(operation) {
		if err := gom.performSafetyChecks(projectDir, operation, args); err != nil {
			return "", fmt.Errorf("safety check failed: %w", err)
		}
	}

	// Execute the Git command
	cmdArgs := append([]string{operation}, args...)
	cmd := exec.Command("git", cmdArgs...)
	cmd.Dir = projectDir

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("Git operation failed: %w, output: %s", err, output)
	}

	// Clear cache for this directory after potentially modifying operations
	if gom.isModifyingOperation(operation) {
		gom.gitManager.InvalidateCache(projectDir)
	}

	return strings.TrimSpace(string(output)), nil
}

// isDestructiveOperation checks if an operation is potentially destructive
func (gom *GitOperationManager) isDestructiveOperation(operation string) bool {
	destructiveOps := map[string]bool{
		"reset":   true,
		"rebase":  true,
		"cherry-pick": true,
		"merge":   true,
		"push":    true,
		"force":   true,
	}
	return destructiveOps[operation]
}

// isModifyingOperation checks if an operation modifies the repository state
func (gom *GitOperationManager) isModifyingOperation(operation string) bool {
	modifyingOps := map[string]bool{
		"add":     true,
		"commit":  true,
		"push":    true,
		"pull":    true,
		"reset":   true,
		"rebase":  true,
		"merge":   true,
		"checkout": true,
		"cherry-pick": true,
	}
	return modifyingOps[operation]
}

// performSafetyChecks performs additional safety checks for destructive operations
func (gom *GitOperationManager) performSafetyChecks(projectDir, operation string, args []string) error {
	// Get current Git state
	state, err := gom.gitManager.GetGitState(projectDir)
	if err != nil {
		return fmt.Errorf("cannot get Git state: %w", err)
	}

	// Check if working directory is clean for certain operations
	if state.IsDirty {
		cleanRequiredOps := map[string]bool{
			"rebase": true,
			"merge":  true,
			"checkout": true,
		}
		if cleanRequiredOps[operation] {
			return fmt.Errorf("working directory has uncommitted changes, operation '%s' requires clean state", operation)
		}
	}

	// Check for protected branches
	protectedBranches := map[string]bool{
		"main":   true,
		"master": true,
		"develop": true,
		"production": true,
	}

	if protectedBranches[state.Branch] {
		riskyOps := map[string]bool{
			"push":   true,
			"reset":  true,
			"rebase": true,
		}
		if riskyOps[operation] {
			return fmt.Errorf("operation '%s' not allowed on protected branch '%s'", operation, state.Branch)
		}
	}

	return nil
}

// InvalidateCache removes cached Git state for a directory
func (gm *GitManager) InvalidateCache(projectDir string) {
	gm.mu.Lock()
	delete(gm.cache, projectDir)
	gm.mu.Unlock()
}

// EnableSafetyMode enables/disables safety mode for Git operations
func (gom *GitOperationManager) EnableSafetyMode(enabled bool) {
	gom.safetyMode = enabled
}

// AllowOperation allows/disallows a specific Git operation
func (gom *GitOperationManager) AllowOperation(operation string, allowed bool) {
	gom.allowedOps[operation] = allowed
}

// GetOperationPermissions returns current operation permissions
func (gom *GitOperationManager) GetOperationPermissions() map[string]bool {
	permissions := make(map[string]bool)
	for op, allowed := range gom.allowedOps {
		permissions[op] = allowed
	}
	return permissions
}

// GitEventLogger logs Git operations for audit purposes
type GitEventLogger struct {
	events []GitEvent
	mu     sync.RWMutex
}

// GitEvent represents a Git operation event
type GitEvent struct {
	Timestamp   time.Time
	ProjectDir  string
	Operation   string
	Args        []string
	Success     bool
	Output      string
	Error       string
	ChatID      int64
	ThreadID    int
	UserPrompt  string
}

// NewGitEventLogger creates a new Git event logger
func NewGitEventLogger() *GitEventLogger {
	return &GitEventLogger{
		events: make([]GitEvent, 0, 1000), // Pre-allocate for 1000 events
	}
}

// LogGitEvent logs a Git operation event
func (gel *GitEventLogger) LogGitEvent(event GitEvent) {
	gel.mu.Lock()
	defer gel.mu.Unlock()

	event.Timestamp = time.Now()
	gel.events = append(gel.events, event)

	// Keep only last 1000 events
	if len(gel.events) > 1000 {
		gel.events = gel.events[len(gel.events)-1000:]
	}
}

// GetRecentEvents returns recent Git events
func (gel *GitEventLogger) GetRecentEvents(limit int) []GitEvent {
	gel.mu.RLock()
	defer gel.mu.RUnlock()

	if limit <= 0 || limit > len(gel.events) {
		limit = len(gel.events)
	}

	// Return last 'limit' events
	startIndex := len(gel.events) - limit
	events := make([]GitEvent, limit)
	copy(events, gel.events[startIndex:])

	return events
}

// GetEventsByProject returns Git events for a specific project
func (gel *GitEventLogger) GetEventsByProject(projectDir string, limit int) []GitEvent {
	gel.mu.RLock()
	defer gel.mu.RUnlock()

	projectEvents := make([]GitEvent, 0)
	for i := len(gel.events) - 1; i >= 0 && len(projectEvents) < limit; i-- {
		if gel.events[i].ProjectDir == projectDir {
			projectEvents = append(projectEvents, gel.events[i])
		}
	}

	return projectEvents
}

// Global Git manager instances
var (
	gitManager          *GitManager
	gitOperationManager *GitOperationManager
	gitEventLogger      *GitEventLogger
	globalGitManager    *GitManager // Global reference for checkpoint system
)

// InitGitIntegration initializes the Git integration system
func InitGitIntegration() {
	gitManager = NewGitManager()
	globalGitManager = gitManager // Set global reference
	gitOperationManager = NewGitOperationManager(gitManager)
	gitEventLogger = NewGitEventLogger()
}