package app

import (
	"fmt"
	"strings"
)

// ParallelTask is one goal parsed from a /parallel command.
type ParallelTask struct {
	ID          string
	Description string
}

// ParseParallelTasks parses user input into parallel tasks
// Format: numbered list "1. task one\n2. task two\n3. task three"
func ParseParallelTasks(input string) []ParallelTask {
	var tasks []ParallelTask
	lines := strings.Split(input, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Strip number prefix like "1. ", "2. ", "- ", etc.
		desc := line
		for i, c := range line {
			if c == '.' || c == ')' {
				if i > 0 && i < 4 {
					desc = strings.TrimSpace(line[i+1:])
					break
				}
			}
			if c == '-' && i == 0 {
				desc = strings.TrimSpace(line[1:])
				break
			}
		}

		if desc == "" {
			continue
		}

		taskID := fmt.Sprintf("task_%d", len(tasks)+1)
		tasks = append(tasks, ParallelTask{
			ID:          taskID,
			Description: desc,
		})
	}

	return tasks
}

// containsAny checks if a string contains any of the given substrings
func containsAny(s string, substrings []string) bool {
	for _, substring := range substrings {
		if strings.Contains(s, substring) {
			return true
		}
	}
	return false
}
