package main

import (
	"fmt"
	"os"
	"strings"
)

// testGitIntegration 測試 Git 整合功能
func testGitIntegration() {
	fmt.Println("🧪 Testing Git Integration...")
	fmt.Println("=" + strings.Repeat("=", 50))

	// Initialize Git integration
	InitGitIntegration()

	// Test 1: Check if current directory is a Git repository
	fmt.Println("\n1️⃣ Testing Git repository detection...")
	currentDir, _ := os.Getwd()
	fmt.Printf("Current directory: %s\n", currentDir)

	gitState, err := gitManager.GetGitState(currentDir)
	if err != nil {
		fmt.Printf("❌ Git state error: %v\n", err)
	} else {
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
	}

	// Test 2: Test Git operations manager
	fmt.Println("\n2️⃣ Testing Git operations manager...")
	permissions := gitOperationManager.GetOperationPermissions()
	fmt.Printf("Safety mode: %t\n", gitOperationManager.safetyMode)
	fmt.Println("Operation permissions:")
	for op, allowed := range permissions {
		status := "❌ Denied"
		if allowed {
			status = "✅ Allowed"
		}
		fmt.Printf("   git %s: %s\n", op, status)
	}

	// Test 3: Test safe Git operations
	fmt.Println("\n3️⃣ Testing safe Git operations...")

	// Try safe operations
	safeOps := []string{"status", "log", "diff"}
	for _, op := range safeOps {
		fmt.Printf("Testing 'git %s'... ", op)
		output, err := gitOperationManager.ExecuteGitOperation(currentDir, op, []string{})
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
		} else {
			fmt.Printf("✅ Success (output: %d chars)\n", len(output))
		}
	}

	// Test 4: Test dangerous operations (should be blocked)
	fmt.Println("\n4️⃣ Testing dangerous operations (should be blocked)...")
	dangerousOps := []string{"add", "commit", "push"}
	for _, op := range dangerousOps {
		fmt.Printf("Testing 'git %s'... ", op)
		_, err := gitOperationManager.ExecuteGitOperation(currentDir, op, []string{"."})
		if err != nil {
			fmt.Printf("✅ Correctly blocked: %v\n", err)
		} else {
			fmt.Printf("⚠️  Unexpectedly allowed!\n")
		}
	}

	// Test 5: Test Git event logging
	fmt.Println("\n5️⃣ Testing Git event logging...")

	// Log a test event
	testEvent := GitEvent{
		ProjectDir:  currentDir,
		Operation:   "status",
		Args:        []string{},
		Success:     true,
		Output:      "Test output",
		ChatID:      12345,
		ThreadID:    1,
		UserPrompt:  "Test git status",
	}

	gitEventLogger.LogGitEvent(testEvent)

	events := gitEventLogger.GetRecentEvents(5)
	fmt.Printf("Recent Git events: %d\n", len(events))

	for i, event := range events {
		fmt.Printf("   %d. git %s (%s) - Chat %d\n",
			i+1, event.Operation,
			func() string { if event.Success { return "✅" } else { return "❌" } }(),
			event.ChatID)
	}

	fmt.Println("\n✅ Git integration test completed!")
}