package app

import "testing"

func TestCodexWorkingDirFallsBackForEmptyProjectDir(t *testing.T) {
	for _, input := range []string{"", "   "} {
		if got := codexWorkingDir(input); got != "." {
			t.Fatalf("codexWorkingDir(%q) = %q, want .", input, got)
		}
	}

	if got := codexWorkingDir("/repo"); got != "/repo" {
		t.Fatalf("codexWorkingDir(/repo) = %q, want /repo", got)
	}
}
