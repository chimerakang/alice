package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"os/exec"
	"time"
	"sync"

	alicev1 "claude-tg-agent/gen/go/alice/v1"
)

// Git integration test - standalone version

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

func main() {
	fmt.Println("🧪 Testing Git Integration...")
	fmt.Println("=" + strings.Repeat("=", 50))

	// Create Git manager
	gitManager := NewGitManager()

	// Test 1: Check if current directory is a Git repository
	fmt.Println("\n1️⃣ Testing Git repository detection...")
	currentDir, _ := os.Getwd()

	// Go up two levels to get to Alice project root
	projectDir := filepath.Dir(filepath.Dir(currentDir))
	fmt.Printf("Testing directory: %s\n", projectDir)

	gitState, err := gitManager.GetGitState(projectDir)
	if err != nil {
		fmt.Printf("❌ Git state error: %v\n", err)

		// Try current directory
		fmt.Printf("Trying current directory: %s\n", currentDir)
		gitState, err = gitManager.GetGitState(currentDir)
		if err != nil {
			fmt.Printf("❌ Git state error: %v\n", err)
			fmt.Println("   Make sure you're in a Git repository!")
			return
		}
		projectDir = currentDir
	}

	fmt.Printf("✅ Git repository detected!\n")
	fmt.Printf("   Branch: %s\n", gitState.Branch)
	fmt.Printf("   Commit: %s\n", gitState.CommitHash)
	fmt.Printf("   Remote: %s\n", gitState.RemoteUrl)
	fmt.Printf("   Clean: %t\n", !gitState.IsDirty)
	if gitState.IsDirty {
		fmt.Printf("   Modified files (%d):\n", len(gitState.ModifiedFiles))
		for i, file := range gitState.ModifiedFiles {
			if i < 5 { // Show only first 5 files
				fmt.Printf("     - %s\n", file)
			}
		}
		if len(gitState.ModifiedFiles) > 5 {
			fmt.Printf("     ... and %d more\n", len(gitState.ModifiedFiles)-5)
		}
	}

	// Test 2: Test caching
	fmt.Println("\n2️⃣ Testing Git state caching...")
	start := time.Now()
	_, err = gitManager.GetGitState(projectDir)
	firstCall := time.Since(start)

	start = time.Now()
	_, err = gitManager.GetGitState(projectDir)
	secondCall := time.Since(start)

	fmt.Printf("First call: %v\n", firstCall)
	fmt.Printf("Second call (cached): %v\n", secondCall)
	if secondCall < firstCall {
		fmt.Println("✅ Caching is working!")
	} else {
		fmt.Println("⚠️  Caching may not be effective")
	}

	fmt.Println("\n✅ Git integration test completed!")
}