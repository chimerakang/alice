package hermes

import (
	"bytes"
	"context"
	"log"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

const defaultProcessTimeout = 30 * time.Second

// ProcessOptions describes the runtime envelope for an external command.
type ProcessOptions struct {
	Dir         string
	Env         []string
	Timeout     time.Duration
	OutputLimit int
}

func runProcessOutput(ctx context.Context, opts ProcessOptions, name string, args ...string) ([]byte, error) {
	return runProcess(ctx, opts, false, name, args...)
}

func runProcessCombinedOutput(ctx context.Context, opts ProcessOptions, name string, args ...string) ([]byte, error) {
	return runProcess(ctx, opts, true, name, args...)
}

func runProcess(ctx context.Context, opts ProcessOptions, combined bool, name string, args ...string) ([]byte, error) {
	start := time.Now()
	log.Printf("[hermes.process] start cmd=%s dir=%q timeout=%s", processLogName(name, args), opts.Dir, processTimeout(opts.Timeout))

	cmd, cancel := processCommand(ctx, opts, name, args...)
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
	status := "ok"
	if err != nil {
		status = "error"
	}
	log.Printf("[hermes.process] done cmd=%s status=%s duration=%s", processLogName(name, args), status, time.Since(start).Round(time.Millisecond))
	return output, err
}

func processCommand(ctx context.Context, opts ProcessOptions, name string, args ...string) (*exec.Cmd, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	cctx, cancel := context.WithTimeout(ctx, processTimeout(opts.Timeout))
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
	return cmd, cancel
}

func processTimeout(timeout time.Duration) time.Duration {
	if timeout <= 0 {
		return defaultProcessTimeout
	}
	return timeout
}

func processLogName(name string, args []string) string {
	if len(args) == 0 {
		return name
	}
	return name + " " + strings.Join(args, " ")
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

func (b *limitedBuffer) Truncated() bool {
	return b.truncated
}

func (b *limitedBuffer) Bytes() []byte {
	return b.buf.Bytes()
}

func (b *limitedBuffer) Len() int {
	return b.buf.Len()
}
