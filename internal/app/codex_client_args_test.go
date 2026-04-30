package app

import (
	"slices"
	"testing"
)

// Regression for issue #145: `codex exec resume` (codex-cli >=0.125) rejects
// -C/--cd. If -C leaks back into the resume argv every multi-turn codex call
// will exit 2 in argv parsing and fall back to a fresh session, losing context.
func TestBuildCodexExecArgs_ResumeOmitsCdFlag(t *testing.T) {
	args := buildCodexExecArgs("019dde11-aaaa", "gpt-5.5", "/tmp/project", "hi")
	if slices.Contains(args, "-C") || slices.Contains(args, "--cd") {
		t.Fatalf("resume argv must not contain -C/--cd, got: %v", args)
	}
	if got := args[:3]; got[0] != "exec" || got[1] != "resume" || got[2] != "019dde11-aaaa" {
		t.Fatalf("resume argv prefix = %v, want [exec resume <id>]", got)
	}
	if last := args[len(args)-1]; last != "hi" {
		t.Fatalf("resume argv must end with prompt, got %q", last)
	}
}

func TestBuildCodexExecArgs_FreshKeepsCdFlag(t *testing.T) {
	args := buildCodexExecArgs("", "gpt-5.5", "/tmp/project", "hi")
	if !slices.Contains(args, "-C") {
		t.Fatalf("fresh exec should pass -C for cwd, got: %v", args)
	}
	if args[0] != "exec" || (len(args) > 1 && args[1] == "resume") {
		t.Fatalf("fresh argv must start with bare `exec`, got: %v", args)
	}
}
