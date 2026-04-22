package security

import (
	"fmt"
	"regexp"
)

// PIIPattern PII 偵測模式
type PIIPattern struct {
	Name        string `json:"name"`
	Pattern     string `json:"pattern"`
	Severity    string `json:"severity"`
	Replacement string `json:"replacement"`
}

// PIIDetectionContext 提供 PII 偵測記錄的上下文資訊
type PIIDetectionContext struct {
	ChatID          int64
	UserID          int64
	MessageType     string
	SourceType      string
	ProjectPath     string
	MessageID       int
	RedactedSnippet string
}

func getDefaultPIIPatterns() []PIIPattern {
	return []PIIPattern{
		{
			Name:        "email",
			Pattern:     `\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Z|a-z]{2,}\b`,
			Severity:    "medium",
			Replacement: "[EMAIL_REDACTED]",
		},
		{
			Name:        "phone",
			Pattern:     `(?:^|[\s,;])(?:\+?\d{1,3}[-.\s]?)?\(?(\d{3})\)?[-.\s]?(\d{3})[-.\s]?(\d{4})(?:[\s,;]|$)`,
			Severity:    "medium",
			Replacement: "[PHONE_REDACTED]",
		},
		{
			Name:        "ssn",
			Pattern:     `\b\d{3}-?\d{2}-?\d{4}\b`,
			Severity:    "high",
			Replacement: "[SSN_REDACTED]",
		},
		{
			Name:        "credit_card",
			Pattern:     `(?i)(?:card\s*(?:number|#)?[:=\s]*)?(?:\b\d{4}[\s-]\d{4}[\s-]\d{4}[\s-]\d{4}\b|\b\d{4}\s\d{4}\s\d{4}\s\d{4}\b)`,
			Severity:    "high",
			Replacement: "[CARD_REDACTED]",
		},
		{
			Name:        "ip_address",
			Pattern:     `\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b`,
			Severity:    "low",
			Replacement: "[IP_REDACTED]",
		},
		{
			Name:        "api_key",
			Pattern:     `(?i)(api[_-]?key|token|secret)["\s:=]+([a-zA-Z0-9_-]{20,})`,
			Severity:    "critical",
			Replacement: "[API_KEY_REDACTED]",
		},
	}
}

// DetectAndFilterPII 偵測和過濾 PII
func (sm *SecurityManager) DetectAndFilterPII(text string, logEvent bool, ctx *PIIDetectionContext) (filtered string, detected []string) {
	if !sm.config.EnablePIIDetection {
		return text, nil
	}

	filtered = text
	detected = []string{}

	for _, pattern := range sm.piiPatterns {
		regex, err := regexp.Compile(pattern.Pattern)
		if err != nil {
			continue
		}

		matches := regex.FindAllString(text, -1)
		if len(matches) > 0 {
			detected = append(detected, pattern.Name)
			filtered = regex.ReplaceAllString(filtered, pattern.Replacement)

			if logEvent {
				details := map[string]interface{}{
					"pii_type":    pattern.Name,
					"matches":     fmt.Sprintf("%d", len(matches)),
					"source_type": "text_processing",
				}

				snippet := filtered
				if len(snippet) > 100 {
					snippet = snippet[:100]
				}
				if snippet != "" {
					details["redacted_snippet"] = snippet
				}

				if ctx != nil {
					if ctx.ChatID > 0 {
						details["chat_id"] = fmt.Sprintf("%d", ctx.ChatID)
					}
					if ctx.UserID > 0 {
						details["user_id"] = fmt.Sprintf("%d", ctx.UserID)
					}
					if ctx.MessageType != "" {
						details["message_type"] = ctx.MessageType
					}
					if ctx.SourceType != "" {
						details["source_type"] = ctx.SourceType
					}
					if ctx.ProjectPath != "" {
						details["project_path"] = ctx.ProjectPath
					}
					if ctx.MessageID > 0 {
						details["message_id"] = fmt.Sprintf("%d", ctx.MessageID)
					}
				}

				userID := int64(0)
				if ctx != nil {
					userID = ctx.UserID
				}

				sm.LogSecurityEvent(SecurityEvent{
					EventType:   "pii_detected",
					Severity:    pattern.Severity,
					Description: fmt.Sprintf("PII detected: %s", pattern.Name),
					Details:     details,
					UserID:      userID,
				})
			}
		}
	}

	return filtered, detected
}
