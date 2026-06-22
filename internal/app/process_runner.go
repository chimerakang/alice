package app

import (
	"context"
	"time"

	"claude-tg-agent/internal/app/process"
)

const (
	defaultProcessTimeout      = 30 * time.Second
	defaultAgentProcessTimeout = 15 * time.Minute
)

// ProcessOptions is an alias for process.Options.
// New fields CorrelationID and LogPrefix are available to all callers.
type ProcessOptions = process.Options

func runProcessOutput(ctx context.Context, opts ProcessOptions, name string, args ...string) ([]byte, error) {
	return process.RunOutput(ctx, opts, name, args...)
}

func runProcessCombinedOutput(ctx context.Context, opts ProcessOptions, name string, args ...string) ([]byte, error) {
	return process.RunCombinedOutput(ctx, opts, name, args...)
}

func processCommand(ctx context.Context, opts ProcessOptions, name string, args ...string) (*process.ProcessCmd, context.CancelFunc) {
	return process.Command(ctx, opts, name, args...)
}
