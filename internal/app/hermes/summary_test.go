package hermes

import "testing"

func TestFormatModelLabel(t *testing.T) {
	cases := []struct {
		model string
		want  string
	}{
		{"claude-opus-4-6", "Opus"},
		{"claude-sonnet-4-5", "Sonnet"},
		{"claude-haiku-4-5-20251001", "Haiku"},
		{"gpt-5.5", "GPT-5.5"},
		{"gpt-5.5-pro", "GPT-5.5"},
		{"gpt-5.4-mini", "GPT-5.4"},
		{"gpt-5.3-codex", "Codex"},
		{"gpt-4o", "GPT-4o"},
		{"gpt-4o-mini", "GPT-4o"},
		{"gpt-4.1", "GPT-4.1"},
		{"gpt-3.5-turbo", "GPT"},
		{"o3", "o3"},
		{"o4-mini", "o4-mini"},
		{"", "Unknown"},
		{"some-future-model", "some-future-model"},
	}
	for _, tc := range cases {
		t.Run(tc.model, func(t *testing.T) {
			if got := formatModelLabel(tc.model); got != tc.want {
				t.Errorf("formatModelLabel(%q) = %q, want %q", tc.model, got, tc.want)
			}
		})
	}
}
