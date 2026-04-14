package app

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// AutoSkill represents a reusable skill learned from successful task execution
type AutoSkill struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	ToolChain   []ToolStep `json:"tool_chain"`
	Context     string     `json:"context"`
	Tags        []string   `json:"tags"`
	SuccessRate float64    `json:"success_rate"`
	UseCount    int        `json:"use_count"`
	Version     int        `json:"version"`
	Status      string     `json:"status"` // "active", "inactive", "disabled"
	ProjectPath string     `json:"project_path"`
	ChatID      int64      `json:"chat_id"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	LastUsedAt  time.Time  `json:"last_used_at"`
}

// ToolStep represents a single step in a skill's tool chain
type ToolStep struct {
	ToolName string `json:"tool_name"`
	Summary  string `json:"summary"` // brief description of what this step does
}

// SkillManager manages auto-skill identification, storage, and retrieval
type SkillManager struct {
	mu    sync.RWMutex
	cache []AutoSkill // in-memory cache for fast retrieval
}

// Skill identification thresholds
const (
	minToolCalls     = 5 // minimum tool calls to qualify as a skill
	minUniqueTools   = 2 // minimum unique tools to qualify
	maxSkillInject   = 3 // max skills to inject into prompt
	maxInjectTokens  = 1000
	skillExpiryDays  = 30
	minSuccessRate   = 0.5 // auto-disable below this rate
	maxSkillsPerUser = 100
)

// Global skill manager instance
var globalSkillManager *SkillManager

// NewSkillManager creates a new SkillManager and loads existing skills from DB
func NewSkillManager() *SkillManager {
	sm := &SkillManager{
		cache: make([]AutoSkill, 0),
	}
	sm.loadFromDB()
	return sm
}

// InitSkillManager initializes the global skill manager
func InitSkillManager() {
	globalSkillManager = NewSkillManager()
	log.Printf("[skill-manager] initialized with %d skills in cache", len(globalSkillManager.cache))
}

// loadFromDB loads active skills from database into memory cache
func (sm *SkillManager) loadFromDB() {
	if globalStorage == nil {
		return
	}
	sqliteStorage, ok := globalStorage.(*SQLiteStorage)
	if !ok {
		return
	}
	skills, err := sqliteStorage.GetActiveSkills()
	if err != nil {
		log.Printf("[skill-manager] failed to load skills from DB: %v", err)
		return
	}
	sm.mu.Lock()
	sm.cache = skills
	sm.mu.Unlock()
}

// EvaluateDecision checks if a completed decision qualifies as a new skill
func (sm *SkillManager) EvaluateDecision(decision DecisionLog) {
	if globalStorage == nil {
		return
	}

	// Only evaluate successful executions
	if !decision.Outcome.Success {
		return
	}

	toolCalls := decision.ToolCalls
	if len(toolCalls) < minToolCalls {
		return
	}

	// Check unique tool count
	uniqueTools := make(map[string]bool)
	for _, tc := range toolCalls {
		uniqueTools[tc.ToolName] = true
	}
	if len(uniqueTools) < minUniqueTools {
		return
	}

	// Build tool chain
	toolChain := buildToolChain(toolCalls)

	// Generate tags from user prompt and tool names
	tags := generateTags(decision.UserPrompt, toolCalls)

	// Check for existing similar skill
	existing := sm.findSimilarSkill(tags, decision.ProjectPath)
	if existing != nil {
		// Update existing skill version
		sm.updateExistingSkill(existing, toolChain, tags)
		return
	}

	// Generate skill name and description
	name := generateSkillName(decision.UserPrompt, toolCalls)
	description := generateSkillDescription(decision.UserPrompt, decision.Outcome.TaskType)

	skill := AutoSkill{
		ID:          fmt.Sprintf("skill_%d_%d", decision.ChatID, time.Now().UnixMilli()),
		Name:        name,
		Description: description,
		ToolChain:   toolChain,
		Context:     decision.Outcome.TaskType,
		Tags:        tags,
		SuccessRate: 1.0,
		UseCount:    0,
		Version:     1,
		Status:      "active",
		ProjectPath: decision.ProjectPath,
		ChatID:      decision.ChatID,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		LastUsedAt:  time.Now(),
	}

	// Save to database
	sqliteStorage, ok := globalStorage.(*SQLiteStorage)
	if !ok {
		return
	}
	if err := sqliteStorage.InsertAutoSkill(skill); err != nil {
		log.Printf("[skill-manager] failed to save skill: %v", err)
		return
	}

	// Update cache
	sm.mu.Lock()
	sm.cache = append(sm.cache, skill)
	sm.mu.Unlock()

	log.Printf("[skill-manager] new skill created: %s (tools=%d, tags=%v)", name, len(toolChain), tags)
}

// FindRelevantSkills searches for skills matching the user's message
func (sm *SkillManager) FindRelevantSkills(userMessage string, projectPath string) []AutoSkill {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if len(sm.cache) == 0 {
		return nil
	}

	messageLower := strings.ToLower(userMessage)
	messageWords := strings.Fields(messageLower)

	type scored struct {
		skill AutoSkill
		score float64
	}

	var candidates []scored

	for _, skill := range sm.cache {
		if skill.Status != "active" {
			continue
		}

		score := 0.0

		// Tag matching
		for _, tag := range skill.Tags {
			tagLower := strings.ToLower(tag)
			for _, word := range messageWords {
				if strings.Contains(tagLower, word) || strings.Contains(word, tagLower) {
					score += 2.0
				}
			}
		}

		// Description matching
		descLower := strings.ToLower(skill.Description)
		for _, word := range messageWords {
			if len(word) > 2 && strings.Contains(descLower, word) {
				score += 1.0
			}
		}

		// Name matching
		nameLower := strings.ToLower(skill.Name)
		for _, word := range messageWords {
			if len(word) > 2 && strings.Contains(nameLower, word) {
				score += 1.5
			}
		}

		// Project affinity bonus
		if skill.ProjectPath == projectPath {
			score *= 1.5
		}

		// Success rate bonus
		score *= skill.SuccessRate

		// Usage bonus (well-used skills are more reliable)
		if skill.UseCount > 0 {
			score *= 1.0 + float64(skill.UseCount)*0.1
		}

		if score > 1.0 {
			candidates = append(candidates, scored{skill: skill, score: score})
		}
	}

	// Sort by score descending
	for i := 0; i < len(candidates); i++ {
		for j := i + 1; j < len(candidates); j++ {
			if candidates[j].score > candidates[i].score {
				candidates[i], candidates[j] = candidates[j], candidates[i]
			}
		}
	}

	// Return top N
	limit := maxSkillInject
	if len(candidates) < limit {
		limit = len(candidates)
	}

	result := make([]AutoSkill, limit)
	for i := 0; i < limit; i++ {
		result[i] = candidates[i].skill
	}

	return result
}

// FormatSkillsForPrompt formats matched skills for injection into system prompt
func FormatSkillsForPrompt(skills []AutoSkill) string {
	if len(skills) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n[Auto-Skill Reference]\n")
	sb.WriteString("Similar tasks have been successfully completed before using these approaches:\n\n")

	totalLen := 0
	for i, skill := range skills {
		entry := formatSingleSkill(i+1, skill)
		if totalLen+len(entry) > maxInjectTokens*4 { // rough char-to-token ratio
			break
		}
		sb.WriteString(entry)
		totalLen += len(entry)
	}

	return sb.String()
}

// RecordSkillUsage records that a skill was matched and used
func (sm *SkillManager) RecordSkillUsage(skillID string, success bool) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	for i := range sm.cache {
		if sm.cache[i].ID == skillID {
			sm.cache[i].UseCount++
			sm.cache[i].LastUsedAt = time.Now()
			if success {
				// Increase success rate slightly
				sm.cache[i].SuccessRate = sm.cache[i].SuccessRate*0.9 + 0.1
			} else {
				// Decrease success rate
				sm.cache[i].SuccessRate = sm.cache[i].SuccessRate * 0.8
				if sm.cache[i].SuccessRate < minSuccessRate {
					sm.cache[i].Status = "disabled"
					log.Printf("[skill-manager] skill disabled due to low success rate: %s (%.1f%%)", sm.cache[i].Name, sm.cache[i].SuccessRate*100)
				}
			}

			// Persist to DB
			if sqliteStorage, ok := globalStorage.(*SQLiteStorage); ok {
				go func(s AutoSkill) {
					if err := sqliteStorage.UpdateAutoSkill(s); err != nil {
						log.Printf("[skill-manager] failed to update skill: %v", err)
					}
				}(sm.cache[i])
			}
			break
		}
	}
}

// GetSkillsForChat returns all skills for a specific chat
func (sm *SkillManager) GetSkillsForChat(chatID int64) []AutoSkill {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	var result []AutoSkill
	for _, skill := range sm.cache {
		if skill.ChatID == chatID {
			result = append(result, skill)
		}
	}
	return result
}

// DeleteSkill removes a skill by ID
func (sm *SkillManager) DeleteSkill(skillID string, chatID int64) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Verify ownership
	found := false
	for i, skill := range sm.cache {
		if skill.ID == skillID && skill.ChatID == chatID {
			sm.cache = append(sm.cache[:i], sm.cache[i+1:]...)
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("skill not found or access denied")
	}

	if sqliteStorage, ok := globalStorage.(*SQLiteStorage); ok {
		return sqliteStorage.DeleteAutoSkill(skillID)
	}
	return nil
}

// CleanupExpiredSkills deactivates skills that haven't been used within expiry period
func (sm *SkillManager) CleanupExpiredSkills() int {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	expiry := time.Now().AddDate(0, 0, -skillExpiryDays)
	count := 0

	for i := range sm.cache {
		if sm.cache[i].Status == "active" && sm.cache[i].LastUsedAt.Before(expiry) {
			sm.cache[i].Status = "inactive"
			count++

			if sqliteStorage, ok := globalStorage.(*SQLiteStorage); ok {
				go func(s AutoSkill) {
					if err := sqliteStorage.UpdateAutoSkill(s); err != nil {
						log.Printf("[skill-manager] failed to deactivate expired skill: %v", err)
					}
				}(sm.cache[i])
			}
		}
	}

	if count > 0 {
		log.Printf("[skill-manager] deactivated %d expired skills", count)
	}
	return count
}

// findSimilarSkill checks if a similar skill already exists
func (sm *SkillManager) findSimilarSkill(tags []string, projectPath string) *AutoSkill {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	for i, skill := range sm.cache {
		if skill.ProjectPath != projectPath {
			continue
		}
		matchCount := 0
		for _, tag := range tags {
			for _, existingTag := range skill.Tags {
				if strings.EqualFold(tag, existingTag) {
					matchCount++
				}
			}
		}
		// If more than half of tags match, consider it similar
		if len(tags) > 0 && matchCount*2 >= len(tags) {
			return &sm.cache[i]
		}
	}
	return nil
}

// updateExistingSkill updates an existing skill with new execution data
func (sm *SkillManager) updateExistingSkill(existing *AutoSkill, toolChain []ToolStep, tags []string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	for i := range sm.cache {
		if sm.cache[i].ID == existing.ID {
			sm.cache[i].Version++
			sm.cache[i].ToolChain = toolChain
			sm.cache[i].UpdatedAt = time.Now()
			sm.cache[i].LastUsedAt = time.Now()
			// Merge new tags
			tagSet := make(map[string]bool)
			for _, t := range sm.cache[i].Tags {
				tagSet[t] = true
			}
			for _, t := range tags {
				tagSet[t] = true
			}
			mergedTags := make([]string, 0, len(tagSet))
			for t := range tagSet {
				mergedTags = append(mergedTags, t)
			}
			sm.cache[i].Tags = mergedTags

			if sqliteStorage, ok := globalStorage.(*SQLiteStorage); ok {
				go func(s AutoSkill) {
					if err := sqliteStorage.UpdateAutoSkill(s); err != nil {
						log.Printf("[skill-manager] failed to update skill: %v", err)
					}
				}(sm.cache[i])
			}

			log.Printf("[skill-manager] skill updated: %s (v%d)", sm.cache[i].Name, sm.cache[i].Version)
			break
		}
	}
}

// --- Helper functions ---

func buildToolChain(toolCalls []ToolExecution) []ToolStep {
	steps := make([]ToolStep, 0, len(toolCalls))
	for _, tc := range toolCalls {
		summary := summarizeToolInput(tc.ToolName, tc.Input)
		steps = append(steps, ToolStep{
			ToolName: tc.ToolName,
			Summary:  summary,
		})
	}
	return steps
}

func summarizeToolInput(toolName string, input map[string]interface{}) string {
	switch toolName {
	case "Read":
		if path, ok := input["file_path"]; ok {
			return fmt.Sprintf("read %v", path)
		}
	case "Edit":
		if path, ok := input["file_path"]; ok {
			return fmt.Sprintf("edit %v", path)
		}
	case "Write":
		if path, ok := input["file_path"]; ok {
			return fmt.Sprintf("write %v", path)
		}
	case "Bash":
		if cmd, ok := input["command"]; ok {
			cmdStr := fmt.Sprintf("%v", cmd)
			if len(cmdStr) > 60 {
				cmdStr = cmdStr[:60] + "..."
			}
			return cmdStr
		}
	case "Grep":
		if pattern, ok := input["pattern"]; ok {
			return fmt.Sprintf("grep %v", pattern)
		}
	case "Glob":
		if pattern, ok := input["pattern"]; ok {
			return fmt.Sprintf("glob %v", pattern)
		}
	}
	return toolName
}

func generateTags(userPrompt string, toolCalls []ToolExecution) []string {
	tagSet := make(map[string]bool)

	// Add tool names as tags
	for _, tc := range toolCalls {
		tagSet[strings.ToLower(tc.ToolName)] = true
	}

	// Extract keywords from user prompt
	words := strings.Fields(strings.ToLower(userPrompt))
	stopWords := map[string]bool{
		"the": true, "a": true, "an": true, "is": true, "are": true, "was": true,
		"in": true, "on": true, "at": true, "to": true, "for": true, "of": true,
		"and": true, "or": true, "not": true, "it": true, "this": true, "that": true,
		"with": true, "from": true, "by": true, "as": true, "be": true, "has": true,
		"have": true, "had": true, "do": true, "does": true, "did": true, "will": true,
		"can": true, "could": true, "would": true, "should": true, "may": true,
		"please": true, "help": true, "me": true, "my": true, "i": true, "you": true,
		"的": true, "了": true, "是": true, "在": true, "我": true, "有": true,
		"和": true, "就": true, "不": true, "人": true, "都": true, "一": true,
		"這": true, "中": true, "大": true, "來": true, "上": true, "個": true,
		"幫": true, "請": true, "把": true, "用": true, "要": true, "也": true,
	}

	for _, w := range words {
		if len(w) > 2 && !stopWords[w] {
			tagSet[w] = true
		}
	}

	tags := make([]string, 0, len(tagSet))
	for t := range tagSet {
		tags = append(tags, t)
	}
	return tags
}

func generateSkillName(userPrompt string, toolCalls []ToolExecution) string {
	// Use first 50 chars of user prompt as base
	name := userPrompt
	if len(name) > 50 {
		name = name[:50]
	}
	// Clean up
	name = strings.TrimSpace(name)
	if name == "" {
		name = fmt.Sprintf("skill_%d_tools", len(toolCalls))
	}
	return name
}

func generateSkillDescription(userPrompt string, taskType string) string {
	desc := userPrompt
	if len(desc) > 200 {
		desc = desc[:200] + "..."
	}
	if taskType != "" {
		desc = fmt.Sprintf("[%s] %s", taskType, desc)
	}
	return desc
}

func formatSingleSkill(index int, skill AutoSkill) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%d. **%s** (success: %.0f%%, used: %d times)\n", index, skill.Name, skill.SuccessRate*100, skill.UseCount))

	// Show tool chain (condensed)
	maxSteps := 8
	for i, step := range skill.ToolChain {
		if i >= maxSteps {
			sb.WriteString(fmt.Sprintf("   ... +%d more steps\n", len(skill.ToolChain)-maxSteps))
			break
		}
		sb.WriteString(fmt.Sprintf("   %d) %s → %s\n", i+1, step.ToolName, step.Summary))
	}
	sb.WriteString("\n")
	return sb.String()
}

// MarshalToolChain converts tool chain to JSON string for storage
func MarshalToolChain(chain []ToolStep) string {
	data, err := json.Marshal(chain)
	if err != nil {
		return "[]"
	}
	return string(data)
}

// UnmarshalToolChain parses tool chain from JSON string
func UnmarshalToolChain(data string) []ToolStep {
	var chain []ToolStep
	if err := json.Unmarshal([]byte(data), &chain); err != nil {
		return nil
	}
	return chain
}

// MarshalTags converts tags to JSON string for storage
func MarshalTags(tags []string) string {
	data, err := json.Marshal(tags)
	if err != nil {
		return "[]"
	}
	return string(data)
}

// UnmarshalTags parses tags from JSON string
func UnmarshalTags(data string) []string {
	var tags []string
	if err := json.Unmarshal([]byte(data), &tags); err != nil {
		return nil
	}
	return tags
}
