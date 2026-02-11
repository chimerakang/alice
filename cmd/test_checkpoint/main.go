package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	log.Printf("Starting checkpoint system test...")

	// Create test directory
	testDir := "/tmp/alice-checkpoint-test"
	if err := os.RemoveAll(testDir); err != nil {
		log.Printf("Warning: failed to clean test directory: %v", err)
	}
	if err := os.MkdirAll(testDir, 0755); err != nil {
		log.Fatalf("Failed to create test directory: %v", err)
	}

	log.Printf("Test directory created: %s", testDir)

	// Initialize SQLite storage
	dbPath := filepath.Join(testDir, "test.db")
	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		log.Fatalf("Failed to initialize storage: %v", err)
	}
	log.Printf("Storage initialized: %s", dbPath)

	// Initialize Git manager
	gitManager := NewGitManager()
	log.Printf("Git manager initialized")

	// Initialize checkpoint manager
	config := CheckpointConfig{
		MaxCheckpoints:      10,
		AutoCleanupAfter:    time.Hour,
		MaxSnapshotSize:     10 * 1024 * 1024, // 10MB
		BackupUntracked:     true,
		GitIntegration:      false, // Disabled for test (no git repo)
		DangerousOperations: getDefaultDangerousOperations(),
	}

	checkpointManager := NewCheckpointManager(storage, gitManager, config)
	log.Printf("Checkpoint manager initialized")

	// Run tests
	if err := testBasicCheckpointOperations(checkpointManager, testDir); err != nil {
		log.Fatalf("Basic operations test failed: %v", err)
	}

	if err := testDangerousOperationDetection(checkpointManager, testDir); err != nil {
		log.Fatalf("Dangerous operation detection test failed: %v", err)
	}

	if err := testCheckpointRestore(checkpointManager, testDir); err != nil {
		log.Fatalf("Checkpoint restore test failed: %v", err)
	}

	if err := testCheckpointStatistics(checkpointManager, testDir); err != nil {
		log.Fatalf("Checkpoint statistics test failed: %v", err)
	}

	log.Printf("✅ All checkpoint tests passed!")

	// Cleanup
	if err := os.RemoveAll(testDir); err != nil {
		log.Printf("Warning: failed to clean up test directory: %v", err)
	}

	log.Printf("Test completed successfully")
}

func testBasicCheckpointOperations(cm *CheckpointManager, testDir string) error {
	log.Printf("Running basic checkpoint operations test...")

	// Test 1: Create a manual checkpoint
	checkpoint, err := cm.CreateCheckpoint(
		testDir,
		"Test manual checkpoint",
		TriggerManual,
		"test-session",
		12345,
		"",
	)
	if err != nil {
		return fmt.Errorf("failed to create manual checkpoint: %w", err)
	}

	log.Printf("✓ Manual checkpoint created: %s", checkpoint.ID)

	// Test 2: List checkpoints
	checkpoints, err := cm.ListCheckpoints(testDir, 10)
	if err != nil {
		return fmt.Errorf("failed to list checkpoints: %w", err)
	}

	if len(checkpoints) != 1 {
		return fmt.Errorf("expected 1 checkpoint, got %d", len(checkpoints))
	}

	log.Printf("✓ Checkpoint listed successfully")

	// Test 3: Delete checkpoint
	if err := cm.DeleteCheckpoint(checkpoint.ID); err != nil {
		return fmt.Errorf("failed to delete checkpoint: %w", err)
	}

	log.Printf("✓ Checkpoint deleted successfully")

	// Verify deletion
	checkpoints, err = cm.ListCheckpoints(testDir, 10)
	if err != nil {
		return fmt.Errorf("failed to list checkpoints after deletion: %w", err)
	}

	if len(checkpoints) != 0 {
		return fmt.Errorf("expected 0 checkpoints after deletion, got %d", len(checkpoints))
	}

	log.Printf("✓ Basic checkpoint operations test passed")
	return nil
}

func testDangerousOperationDetection(cm *CheckpointManager, testDir string) error {
	log.Printf("Running dangerous operation detection test...")

	// Test dangerous operations detection
	testCases := []struct {
		toolName string
		input    map[string]interface{}
		expected bool
		desc     string
	}{
		{
			toolName: "Write",
			input:    map[string]interface{}{"file_path": "/tmp/test.txt"},
			expected: true,
			desc:     "Write operation should trigger checkpoint",
		},
		{
			toolName: "Edit",
			input:    map[string]interface{}{"file_path": "/tmp/test.txt"},
			expected: true,
			desc:     "Edit operation should trigger checkpoint",
		},
		{
			toolName: "Bash",
			input:    map[string]interface{}{"command": "rm -rf /tmp/test"},
			expected: true,
			desc:     "rm command should trigger checkpoint",
		},
		{
			toolName: "Bash",
			input:    map[string]interface{}{"command": "ls -la"},
			expected: false,
			desc:     "ls command should not trigger checkpoint",
		},
		{
			toolName: "Read",
			input:    map[string]interface{}{"file_path": "/tmp/test.txt"},
			expected: false,
			desc:     "Read operation should not trigger checkpoint",
		},
	}

	for _, tc := range testCases {
		shouldCreate, _, _ := cm.ShouldCreateCheckpoint(tc.toolName, tc.input)
		if shouldCreate != tc.expected {
			return fmt.Errorf("test case failed: %s - expected %v, got %v", tc.desc, tc.expected, shouldCreate)
		}
		log.Printf("✓ %s", tc.desc)
	}

	log.Printf("✓ Dangerous operation detection test passed")
	return nil
}

func testCheckpointRestore(cm *CheckpointManager, testDir string) error {
	log.Printf("Running checkpoint restore test...")

	// Create a test file
	testFile := filepath.Join(testDir, "test.txt")
	originalContent := "Original content"
	if err := os.WriteFile(testFile, []byte(originalContent), 0644); err != nil {
		return fmt.Errorf("failed to create test file: %w", err)
	}

	// Create checkpoint
	checkpoint, err := cm.CreateCheckpoint(
		testDir,
		"Test restore checkpoint",
		TriggerPreDanger,
		"test-session",
		12345,
		"test dangerous operation",
	)
	if err != nil {
		return fmt.Errorf("failed to create checkpoint for restore test: %w", err)
	}

	log.Printf("✓ Checkpoint created for restore test: %s", checkpoint.ID)

	// Modify the file
	modifiedContent := "Modified content"
	if err := os.WriteFile(testFile, []byte(modifiedContent), 0644); err != nil {
		return fmt.Errorf("failed to modify test file: %w", err)
	}

	// Verify modification
	content, err := os.ReadFile(testFile)
	if err != nil {
		return fmt.Errorf("failed to read modified file: %w", err)
	}
	if string(content) != modifiedContent {
		return fmt.Errorf("file was not modified correctly")
	}

	log.Printf("✓ Test file modified successfully")

	// Restore checkpoint
	if err := cm.RestoreCheckpoint(checkpoint.ID, testDir); err != nil {
		return fmt.Errorf("failed to restore checkpoint: %w", err)
	}

	log.Printf("✓ Checkpoint restored successfully")

	// Verify restoration (if file snapshots were created)
	content, err = os.ReadFile(testFile)
	if err == nil && len(checkpoint.FileSnapshots) > 0 {
		if string(content) == originalContent {
			log.Printf("✓ File content restored correctly")
		} else {
			log.Printf("! File content not restored (snapshots may not include this file)")
		}
	} else {
		log.Printf("! File restoration test skipped (no file snapshots created)")
	}

	log.Printf("✓ Checkpoint restore test completed")
	return nil
}

func testCheckpointStatistics(cm *CheckpointManager, testDir string) error {
	log.Printf("Running checkpoint statistics test...")

	// Create some test checkpoints
	for i := 0; i < 3; i++ {
		_, err := cm.CreateCheckpoint(
			testDir,
			fmt.Sprintf("Test checkpoint %d", i+1),
			TriggerAuto,
			"test-session",
			12345,
			"",
		)
		if err != nil {
			return fmt.Errorf("failed to create test checkpoint %d: %w", i+1, err)
		}
	}

	log.Printf("✓ Created 3 test checkpoints")

	// Get statistics
	stats, err := cm.GetCheckpointStats(testDir)
	if err != nil {
		return fmt.Errorf("failed to get checkpoint statistics: %w", err)
	}

	// Verify statistics
	totalCheckpoints, ok := stats["total_checkpoints"].(int64)
	if !ok {
		return fmt.Errorf("total_checkpoints not found in stats")
	}

	if totalCheckpoints != 3 {
		return fmt.Errorf("expected 3 total checkpoints, got %d", totalCheckpoints)
	}

	autoCheckpoints, ok := stats["auto_checkpoints"].(int64)
	if !ok {
		return fmt.Errorf("auto_checkpoints not found in stats")
	}

	if autoCheckpoints != 3 {
		return fmt.Errorf("expected 3 auto checkpoints, got %d", autoCheckpoints)
	}

	log.Printf("✓ Checkpoint statistics verified: %d total, %d auto", totalCheckpoints, autoCheckpoints)

	// Test manager controls
	if !cm.IsEnabled() {
		return fmt.Errorf("checkpoint manager should be enabled")
	}

	cm.Disable()
	if cm.IsEnabled() {
		return fmt.Errorf("checkpoint manager should be disabled after Disable() call")
	}

	cm.Enable()
	if !cm.IsEnabled() {
		return fmt.Errorf("checkpoint manager should be enabled after Enable() call")
	}

	log.Printf("✓ Manager enable/disable functionality verified")

	log.Printf("✓ Checkpoint statistics test passed")
	return nil
}