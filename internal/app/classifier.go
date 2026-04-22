package app

import "strings"

// Complexity classifies a user prompt into one of three tiers used by the
// Hermes Complexity Gate (#103) and the VS Code UserPromptSubmit observation
// experiment (#104). The classifier is heuristic-only and runs locally with
// zero cost.
type Complexity string

const (
	ComplexityTrivial  Complexity = "trivial"
	ComplexityModerate Complexity = "moderate"
	ComplexityComplex  Complexity = "complex"
)

// ClassificationResult explains why a prompt was placed in a given tier so
// downstream analytics can audit heuristic accuracy.
type ClassificationResult struct {
	Complexity  Complexity
	MatchedRule string
}

var trivialKeywords = []string{
	"commit", "push", "pull", "tag", "merge",
	"git status", "git log", "git diff", "git add",
	"build", "test", "run",
	"show", "list", "check",
	"rebuild", "restart", "stop", "start",
}

var complexKeywords = []string{
	"重構", "重新設計", "整合", "整體", "所有檔案",
	"refactor", "implement", "migrate", "rewrite",
	"build a new", "create a new module", "design",
	"multiple files", "across the codebase",
}

// ClassifyComplexity applies heuristic rules to a prompt. Rules fire in order;
// first match wins. This intentionally biases toward "trivial" for short
// shell-like prompts so that the Complexity Gate does not spin up Hermes for
// routine git operations.
func ClassifyComplexity(prompt string) ClassificationResult {
	trimmed := strings.TrimSpace(prompt)
	lower := strings.ToLower(trimmed)
	length := len([]rune(trimmed))

	if length == 0 {
		return ClassificationResult{Complexity: ComplexityTrivial, MatchedRule: "empty"}
	}

	if length < 50 {
		if kw := firstMatch(lower, trivialKeywords); kw != "" {
			return ClassificationResult{Complexity: ComplexityTrivial, MatchedRule: "short+trivial:" + kw}
		}
	}

	if kw := firstMatch(lower, complexKeywords); kw != "" {
		return ClassificationResult{Complexity: ComplexityComplex, MatchedRule: "complex-verb:" + kw}
	}

	if length > 200 {
		return ClassificationResult{Complexity: ComplexityComplex, MatchedRule: "long-prompt"}
	}

	return ClassificationResult{Complexity: ComplexityModerate, MatchedRule: "default"}
}

func firstMatch(haystack string, needles []string) string {
	for _, n := range needles {
		if strings.Contains(haystack, n) {
			return n
		}
	}
	return ""
}
