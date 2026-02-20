package app

import (
	"testing"
)

// TestSelectModelHaikuRules tests that Haiku rules are correctly identified
func TestSelectModelHaikuRules(t *testing.T) {
	agent := &Agent{}

	testCases := []struct {
		message      string
		expectedModel string
		expectedReason string
	}{
		{"Please translate this to English", "haiku", "static_rule"},
		{"Can you summarize this file?", "haiku", "static_rule"},
		{"Explain how this function works", "haiku", "static_rule"},
		{"Convert this to JSON format", "haiku", "static_rule"},
		{"Read the config.json file", "haiku", "static_rule"},
		{"What's the status of the build?", "haiku", "static_rule"},
		{"Polish this code snippet", "haiku", "static_rule"},
	}

	for _, tc := range testCases {
		model, reason := agent.selectModel(tc.message)
		if model != tc.expectedModel {
			t.Errorf("Message: %q\nExpected model: %s, got: %s", tc.message, tc.expectedModel, model)
		}
		if reason != tc.expectedReason {
			t.Errorf("Message: %q\nExpected reason: %s, got: %s", tc.message, tc.expectedReason, reason)
		}
	}
}

// TestSelectModelOpusRules tests that Opus rules are correctly identified
func TestSelectModelOpusRules(t *testing.T) {
	agent := &Agent{}

	testCases := []struct {
		message      string
		expectedModel string
	}{
		{"Refactor this entire module", "opus"},
		{"Design the architecture for this system", "opus"},
		{"Update multiple files across the codebase", "opus"},
		{"Debug this complex race condition", "opus"},
		{"Implement a new sorting algorithm", "opus"},
		{"Optimize the performance of this database query", "opus"},
	}

	for _, tc := range testCases {
		model, _ := agent.selectModel(tc.message)
		if model != tc.expectedModel {
			t.Errorf("Message: %q\nExpected model: %s, got: %s", tc.message, tc.expectedModel, model)
		}
	}
}

// TestSelectModelDefaultFallback tests that default Sonnet is returned for unmatched messages
func TestSelectModelDefaultFallback(t *testing.T) {
	agent := &Agent{}

	testCases := []struct {
		message string
	}{
		{"Hello, how are you?"},
		{"What's the weather like?"},
		{"Tell me a joke"},
		{"Random message"},
	}

	for _, tc := range testCases {
		model, reason := agent.selectModel(tc.message)
		if model != "sonnet" {
			t.Errorf("Message: %q\nExpected default model 'sonnet', got: %s", tc.message, model)
		}
		if reason != "default" {
			t.Errorf("Message: %q\nExpected default reason, got: %s", tc.message, reason)
		}
	}
}

// TestSelectModelCaseInsensitive tests that pattern matching is case-insensitive
func TestSelectModelCaseInsensitive(t *testing.T) {
	agent := &Agent{}

	testCases := []struct {
		message      string
		expectedModel string
	}{
		{"TRANSLATE this text", "haiku"},
		{"Translate THIS text", "haiku"},
		{"TrAnSlAtE this text", "haiku"},
		{"REFACTOR my code", "opus"},
		{"Refactor MY code", "opus"},
		{"ReFaCtOr my code", "opus"},
	}

	for _, tc := range testCases {
		model, _ := agent.selectModel(tc.message)
		if model != tc.expectedModel {
			t.Errorf("Message: %q\nExpected model: %s, got: %s", tc.message, tc.expectedModel, model)
		}
	}
}

// TestSelectModelPriorityOrder tests that higher priority (lower number) rules take precedence
func TestSelectModelPriorityOrder(t *testing.T) {
	agent := &Agent{}

	// Message containing both translation (Priority 1) and refactor (Priority 20)
	// Should choose translation (Haiku) due to lower priority number
	message := "Translate and refactor this code"
	model, _ := agent.selectModel(message)
	if model != "haiku" {
		t.Errorf("Message: %q\nExpected Haiku (priority 1), got: %s", message, model)
	}
}
