package hermes

import (
	"context"

	"claude-tg-agent/internal/app/process"
)

// ProcessOptions is an alias for process.Options.
// New fields CorrelationID and LogPrefix are available to all callers.
type ProcessOptions = process.Options

func runProcessOutput(ctx context.Context, opts ProcessOptions, name string, args ...string) ([]byte, error) {
	if opts.LogPrefix == "" {
		opts.LogPrefix = "hermes.process"
	}
	return process.RunOutput(ctx, opts, name, args...)
}

func runProcessCombinedOutput(ctx context.Context, opts ProcessOptions, name string, args ...string) ([]byte, error) {
	if opts.LogPrefix == "" {
		opts.LogPrefix = "hermes.process"
	}
	return process.RunCombinedOutput(ctx, opts, name, args...)
}
