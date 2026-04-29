package process

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Runner centralizes command execution policy for the bot runtime.
type Runner struct{}

// NewRunner returns a reusable command runner instance.
func NewRunner() *Runner { return &Runner{} }

// DefaultRunner is the shared runner used by package-level helpers.
var DefaultRunner = NewRunner()

// Per-binary default timeouts; caller-supplied Timeout overrides these.
var binaryTimeouts = map[string]time.Duration{
	"claude": 15 * time.Minute,
	"codex":  10 * time.Minute,
	"gh":     60 * time.Second,
	"git":    30 * time.Second,
	"docker": 30 * time.Second,
	"ssh":    30 * time.Second,
}

const fallbackTimeout = 30 * time.Second

// Options describes the runtime envelope for an external command.
type Options struct {
	Dir           string
	Env           []string
	Timeout       time.Duration
	OutputLimit   int
	CorrelationID string // auto-generated if empty
	LogPrefix     string // e.g. "process" or "hermes.process"
}

// ProcessCmd wraps exec.Cmd so Start/Wait/Run share the same logging envelope.
type ProcessCmd struct {
	*exec.Cmd

	corrID    string
	prefix    string
	label     string
	timeout   time.Duration
	startTime time.Time

	startOnce sync.Once
	doneOnce  sync.Once
}

// RunOutput executes cmd and returns stdout only.
func RunOutput(ctx context.Context, opts Options, name string, args ...string) ([]byte, error) {
	return DefaultRunner.RunOutput(ctx, opts, name, args...)
}

// RunCombinedOutput executes cmd and returns stdout+stderr combined.
func RunCombinedOutput(ctx context.Context, opts Options, name string, args ...string) ([]byte, error) {
	return DefaultRunner.RunCombinedOutput(ctx, opts, name, args...)
}

// Command creates a prepared ProcessCmd with timeout, pgid kill group, and cancel.
// Callers must defer cancel() to release the context.
func Command(ctx context.Context, opts Options, name string, args ...string) (*ProcessCmd, context.CancelFunc) {
	return DefaultRunner.Command(ctx, opts, name, args...)
}

// RunOutput executes cmd and returns stdout only.
func (r *Runner) RunOutput(ctx context.Context, opts Options, name string, args ...string) ([]byte, error) {
	return r.run(ctx, opts, false, name, args...)
}

// RunCombinedOutput executes cmd and returns stdout+stderr combined.
func (r *Runner) RunCombinedOutput(ctx context.Context, opts Options, name string, args ...string) ([]byte, error) {
	return r.run(ctx, opts, true, name, args...)
}

// Command creates a prepared ProcessCmd with timeout, pgid kill group, and cancel.
// Callers must defer cancel() to release the context.
func (r *Runner) Command(ctx context.Context, opts Options, name string, args ...string) (*ProcessCmd, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}

	timeout := resolveTimeout(opts.Timeout, name)
	cctx, cancel := context.WithTimeout(ctx, timeout)
	cmd := exec.CommandContext(cctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
	if opts.Dir != "" {
		cmd.Dir = opts.Dir
	}
	if len(opts.Env) > 0 {
		cmd.Env = opts.Env
	}

	return &ProcessCmd{
		Cmd:     cmd,
		corrID:  correlationID(opts.CorrelationID),
		prefix:  logPrefix(opts.LogPrefix),
		label:   logName(name, args),
		timeout: timeout,
	}, cancel
}

func (r *Runner) run(ctx context.Context, opts Options, combined bool, name string, args ...string) ([]byte, error) {
	cmd, cancel := r.Command(ctx, opts, name, args...)
	defer cancel()

	var stdout limitedBuffer
	var stderr limitedBuffer
	stdout.limit = opts.OutputLimit
	stderr.limit = opts.OutputLimit
	cmd.Stdout = &stdout
	if combined {
		cmd.Stderr = &stdout
	} else {
		cmd.Stderr = &stderr
	}

	err := cmd.Run()
	output := stdout.Bytes()
	if err != nil && !combined && stderr.Len() > 0 {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitErr.Stderr = stderr.Bytes()
		}
	}
	if stdout.Truncated() {
		output = append(output, []byte("\n[process output truncated]\n")...)
	}
	return output, err
}

// Start logs the command envelope before starting the process.
func (c *ProcessCmd) Start() error {
	c.startOnce.Do(func() {
		c.startTime = time.Now()
		log.Printf("[%s] start corr=%s cmd=%s cwd=%q timeout=%s", c.prefix, c.corrID, c.label, c.Cmd.Dir, c.timeout)
	})
	err := c.Cmd.Start()
	if err != nil {
		c.logDone(err)
	}
	return err
}

// Wait waits for the process to exit and logs completion exactly once.
func (c *ProcessCmd) Wait() error {
	err := c.Cmd.Wait()
	c.logDone(err)
	return err
}

// Run starts and waits for the process with unified logging.
func (c *ProcessCmd) Run() error {
	if err := c.Start(); err != nil {
		return err
	}
	return c.Wait()
}

func (c *ProcessCmd) logDone(err error) {
	c.doneOnce.Do(func() {
		exitCode := 0
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			} else {
				exitCode = -1
			}
		}
		start := c.startTime
		if start.IsZero() {
			start = time.Now()
		}
		log.Printf("[%s] done corr=%s cmd=%s exit=%d duration=%s", c.prefix, c.corrID, c.label, exitCode, time.Since(start).Round(time.Millisecond))
	})
}

func resolveTimeout(timeout time.Duration, binary string) time.Duration {
	if timeout > 0 {
		return timeout
	}
	if t, ok := binaryTimeouts[binary]; ok {
		return t
	}
	return fallbackTimeout
}

func logPrefix(prefix string) string {
	if prefix == "" {
		return "process"
	}
	return prefix
}

func logName(name string, args []string) string {
	if len(args) == 0 {
		return name
	}
	return name + " " + strings.Join(args, " ")
}

func correlationID(corrID string) string {
	if corrID != "" {
		return corrID
	}
	return newCorrID()
}

func newCorrID() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err == nil {
		return hex.EncodeToString(b[:])
	}
	return fmt.Sprintf("%08x", time.Now().UnixNano())
}

type limitedBuffer struct {
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.limit <= 0 {
		return b.buf.Write(p)
	}
	remaining := b.limit - b.buf.Len()
	if remaining > 0 {
		if len(p) <= remaining {
			_, _ = b.buf.Write(p)
		} else {
			_, _ = b.buf.Write(p[:remaining])
			b.truncated = true
		}
	} else if len(p) > 0 {
		b.truncated = true
	}
	return len(p), nil
}

func (b *limitedBuffer) Truncated() bool { return b.truncated }
func (b *limitedBuffer) Bytes() []byte   { return b.buf.Bytes() }
func (b *limitedBuffer) Len() int        { return b.buf.Len() }
