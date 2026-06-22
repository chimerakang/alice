package app

import "testing"

// TestClassifyRecord5 verifies that the classifier correctly handles record
// #5 from the real prompt_classifications history — a real user question
// prefixed by an <ide_opened_file> tag. Under the old classifier this was
// misclassified as complex (long-prompt); the upgrade should now strip the
// tag and recognise "研究" as a moderate work verb.
func TestClassifyRecord5(t *testing.T) {
	prompt := `<ide_opened_file>The user opened the file /Volumes/eclipse/projects/alice/.claude/settings.local.json in the IDE. This may or may not be related to the current task.</ide_opened_file>
請幫我研究一下之前issue中有關於程式碼追蹤分析的功能有哪些已經處理完畢，哪些是還沒實現的功能`

	got := ClassifyComplexity(prompt)
	if got.Complexity != ComplexityModerate {
		t.Fatalf("got=%s (%s), want=moderate", got.Complexity, got.MatchedRule)
	}
	if got.MatchedRule != "moderate-verb:研究" {
		t.Fatalf("matched rule=%s, want moderate-verb:研究", got.MatchedRule)
	}
}
