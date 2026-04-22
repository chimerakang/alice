package app

import (
	"regexp"
	"strings"
)

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
	// Chinese shell-like verbs
	"看一下", "查一下", "檢視", "顯示", "列出", "確認一下",
	"重啟", "重建", "啟動", "停止",
}

var moderateKeywords = []string{
	// Chinese mid-weight work verbs: investigation, bug fixes, adjustments
	"研究", "分析", "檢查", "確認", "處理", "修正", "修復", "調整",
	"補充", "改進", "優化", "更新", "完善",
	"investigate", "analyze", "fix", "adjust", "improve", "update",
}

// actionVerbs is the subset of moderate verbs that imply doing work rather
// than merely looking at something. When any of these appear alongside an
// issue reference (#123 / ＃１２３) the prompt is promoted to complex — that
// pattern almost always means "do the decomposable work tracked in that
// issue", exactly what Hermes Planner-Executor exists for.
var actionVerbs = []string{
	"處理", "修正", "修復", "修", "實作", "實現", "完成", "做",
	"fix", "implement", "build", "create", "ship", "do",
}

var complexKeywords = []string{
	// Chinese heavyweight work verbs: new construction, refactoring, integration
	"重構", "重新設計", "重寫", "整合", "整體", "所有檔案",
	"建立新", "建立一個新", "新增模組", "實作新功能", "從頭",
	"遷移", "大改", "全面",
	// English equivalents
	"refactor", "implement", "migrate", "rewrite",
	"build a new", "create a new module", "design",
	"multiple files", "across the codebase", "整個",
}

// IDE-injected noise that should be stripped before classification.
// These tags carry no signal about user intent and inflate length artificially.
var ideNoisePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?s)<ide_opened_file>.*?</ide_opened_file>`),
	regexp.MustCompile(`(?s)<ide_selection>.*?</ide_selection>`),
	regexp.MustCompile(`(?s)<system-reminder>.*?</system-reminder>`),
	regexp.MustCompile(`(?s)<command-name>.*?</command-name>`),
	regexp.MustCompile(`(?s)<command-message>.*?</command-message>`),
	regexp.MustCompile(`(?s)<command-args>.*?</command-args>`),
	regexp.MustCompile(`(?s)<local-command-stdout>.*?</local-command-stdout>`),
	regexp.MustCompile(`(?s)<local-command-stderr>.*?</local-command-stderr>`),
}

// issueRefPattern matches GitHub issue references like #123 or ＃１２３
// (full-width). A bare issue reference often precedes a substantive task.
var issueRefPattern = regexp.MustCompile(`[#＃][0-9０-９]+`)

// StripIDENoise removes injected editor/system tags so the classifier scores
// the user's actual text. Exported for reuse and testing.
func StripIDENoise(prompt string) string {
	out := prompt
	for _, re := range ideNoisePatterns {
		out = re.ReplaceAllString(out, "")
	}
	return strings.TrimSpace(out)
}

// ClassifyComplexity applies heuristic rules to a prompt. Rules fire in order;
// first match wins. The classifier strips IDE noise first so long
// ide_opened_file payloads do not inflate the length signal.
func ClassifyComplexity(prompt string) ClassificationResult {
	cleaned := StripIDENoise(prompt)
	lower := strings.ToLower(cleaned)
	length := len([]rune(cleaned))

	if length == 0 {
		return ClassificationResult{Complexity: ComplexityTrivial, MatchedRule: "empty"}
	}

	// Complex verbs beat everything — refactor / migrate / rewrite even in
	// short prompts implies multi-step work.
	if kw := firstMatch(lower, complexKeywords); kw != "" {
		return ClassificationResult{Complexity: ComplexityComplex, MatchedRule: "complex-verb:" + kw}
	}

	// Action verb paired with an issue reference is a strong "do decomposable
	// work" signal ("請處理 #239", "fix #61") — promote to complex so Hermes
	// takes the task.
	if issueRefPattern.MatchString(cleaned) {
		if kw := firstMatch(lower, actionVerbs); kw != "" {
			return ClassificationResult{Complexity: ComplexityComplex, MatchedRule: "action-verb+issue-ref:" + kw}
		}
	}

	// Moderate verbs cover the common "investigate / fix / adjust" middle ground.
	// Checked before trivial because a sentence like "檢查一下 log" contains both
	// a moderate verb (檢查) and a trivial fragment (查一下) — the semantically
	// dominant action is the moderate one.
	if kw := firstMatch(lower, moderateKeywords); kw != "" {
		return ClassificationResult{Complexity: ComplexityModerate, MatchedRule: "moderate-verb:" + kw}
	}

	// Short shell-style prompts stay trivial.
	if length < 50 {
		if kw := firstMatch(lower, trivialKeywords); kw != "" {
			return ClassificationResult{Complexity: ComplexityTrivial, MatchedRule: "short+trivial:" + kw}
		}
	}

	// Bare issue reference ("#61" with minimal context) is typically the start
	// of a substantive task — treat as moderate so it is not mistaken for noise.
	if issueRefPattern.MatchString(cleaned) && length < 30 {
		return ClassificationResult{Complexity: ComplexityModerate, MatchedRule: "issue-ref"}
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
