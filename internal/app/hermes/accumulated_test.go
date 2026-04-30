package hermes

import (
	"strings"
	"testing"
)

func TestExtractConclusion(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{
			name: "structured 結論 + evidence both lifted (fullwidth colon)",
			in:   "**結論**：在 telegram.go:42 加了 oneShot 清 session 邏輯\n\n**證據**：\n- internal/app/telegram.go:42 oneShot toggle inserted\n- go test ./internal/app PASS\n\n**未驗證**：無\n\n**下一步**：無",
			want: "結論：在 telegram.go:42 加了 oneShot 清 session 邏輯\n證據：internal/app/telegram.go:42 oneShot toggle inserted; go test ./internal/app PASS",
		},
		{
			name: "structured 結論 ascii colon",
			in:   "**結論**: All tests passed.\n\n**證據**:\n- go test ok",
			want: "結論：All tests passed.\n證據：go test ok",
		},
		{
			name: "結論 alone keeps just the conclusion",
			in:   "**結論**：完成驗證",
			want: "結論：完成驗證",
		},
		{
			name: "no structured marker falls back to first line",
			in:   "Plain output line one\nline two\nline three",
			want: "Plain output line one",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractConclusion(tc.in)
			if got != tc.want {
				t.Fatalf("got=%q want=%q", got, tc.want)
			}
		})
	}
}

func TestAppendResultKeepsOnlyConclusionAndEvidence(t *testing.T) {
	long := "**結論**：完成 X\n\n**證據**：\n- foo.go:1 baseline\n- " + repeatStr("a", 4000) + "\n\n**未驗證**：無\n\n**下一步**：無"
	updated, _ := AppendResult("", long, 1, AccumulatedConfig{})
	if got := []rune(updated); len(got) > perSubtaskConclusionMaxBytes+10 {
		t.Fatalf("accumulated unexpectedly long: len=%d head=%q", len(got), string(got[:60]))
	}
	if !strings.Contains(updated, "結論：完成 X") {
		t.Fatalf("missing conclusion: %q", updated)
	}
	if !strings.Contains(updated, "證據：foo.go:1 baseline") {
		t.Fatalf("missing first evidence bullet: %q", updated)
	}
}

func TestClassifyFailure(t *testing.T) {
	cases := []struct {
		in   string
		want FailureKind
	}{
		{"", FailureUnknown},
		{"Prompt is too long: 200000 tokens", FailureEnv},
		{"context length exceeded", FailureEnv},
		{"context deadline exceeded", FailureEnv},
		{"dial tcp: i/o timeout", FailureEnv},
		{"503 Service Unavailable", FailureEnv},
		{"signal: killed", FailureEnv},
		{"file_patch failed: old_text not unique", FailureContent},
		{"go test ./... FAIL", FailureContent},
	}
	for _, tc := range cases {
		got := ClassifyFailure(tc.in)
		if got != tc.want {
			t.Errorf("ClassifyFailure(%q)=%v, want %v", tc.in, got, tc.want)
		}
	}
}

func repeatStr(s string, n int) string {
	out := ""
	for i := 0; i < n; i++ {
		out += s
	}
	return out
}
