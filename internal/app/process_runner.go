package app

import (
	"context"
	"os/exec"
	"time"
)

const defaultProcessTimeout = 30 * time.Second
const defaultAgentProcessTimeout = 15 * time.Minute

// ProcessOptions describes the runtime envelope for an external command.
type ProcessOptions struct {
	Dir     string
	Env     []string
	Timeout time.Duration
}

// runProcessOutput executes a command with a mandatory timeout. It is the
// package-level guardrail for short helper commands such as git/gh probes.
func runProcessOutput(ctx context.Context, opts ProcessOptions, name string, args ...string) ([]byte, error) {
	cmd, cancel := processCommand(ctx, opts, name, args...)
	defer cancel()
	return cmd.Output()
}

// runProcessCombinedOutput executes a command with a mandatory timeout and
// returns stdout/stderr combined for user-facing diagnostics.
func runProcessCombinedOutput(ctx context.Context, opts ProcessOptions, name string, args ...string) ([]byte, error) {
	cmd, cancel := processCommand(ctx, opts, name, args...)
	defer cancel()
	return cmd.CombinedOutput()
}

func processCommand(ctx context.Context, opts ProcessOptions, name string, args ...string) (*exec.Cmd, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultProcessTimeout
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	cmd := exec.CommandContext(cctx, name, args...)
	if opts.Dir != "" {
		cmd.Dir = opts.Dir
	}
	if len(opts.Env) > 0 {
		cmd.Env = opts.Env
	}
	return cmd, cancel
}
