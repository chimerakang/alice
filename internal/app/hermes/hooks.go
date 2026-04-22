// Package hermes provides tool execution hooks for the Hermes Brain-Executor architecture.
// Hooks run at tool boundaries to guard paths and validate results without reimplementing
// the Claude Code CLI toolset.
package hermes

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// PreHook runs before a tool executes. Return an error to block execution.
type PreHook func(ctx context.Context, tool string, args map[string]any) error

// PostHook runs after a tool succeeds. A returned error is attached to the result
// as "validation_error" but does not fail the tool call itself.
type PostHook func(ctx context.Context, tool string, args map[string]any, result map[string]any) error

// HookRegistry holds registered pre/post hooks and executes them around tool calls.
type HookRegistry struct {
	preHooks  []PreHook
	postHooks []PostHook
}

// NewHookRegistry returns an empty registry.
func NewHookRegistry() *HookRegistry {
	return &HookRegistry{}
}

// RegisterPre adds a pre-hook that runs before every tool execution.
func (r *HookRegistry) RegisterPre(h PreHook) {
	r.preHooks = append(r.preHooks, h)
}

// RegisterPost adds a post-hook that runs after a tool succeeds.
func (r *HookRegistry) RegisterPost(h PostHook) {
	r.postHooks = append(r.postHooks, h)
}

// RunPre executes all pre-hooks in registration order. First error stops the chain.
func (r *HookRegistry) RunPre(ctx context.Context, tool string, args map[string]any) error {
	for _, h := range r.preHooks {
		if err := h(ctx, tool, args); err != nil {
			return err
		}
	}
	return nil
}

// RunPost executes all post-hooks and attaches any validation errors to result.
func (r *HookRegistry) RunPost(ctx context.Context, tool string, args map[string]any, result map[string]any) {
	var errs []string
	for _, h := range r.postHooks {
		if err := h(ctx, tool, args, result); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		result["validation_error"] = strings.Join(errs, "; ")
	}
}

// factError formats an error as a plain fact with no blame language.
func factError(format string, args ...any) error {
	return fmt.Errorf("Error: "+format, args...)
}

// ─── PathGuard ────────────────────────────────────────────────────────────────

// defaultDenyPatterns are path patterns that PathGuard blocks writes to.
var defaultDenyPatterns = []string{
	"config.json",
	".git/",
	".git",
	".env",
	".env.local",
	".env.production",
	"*.pem",
	"*.key",
	"id_rsa",
	"id_ed25519",
}

// PathGuard returns a PreHook that blocks write operations to protected paths.
// It checks the "path" and "file_path" arguments used by Edit, Write, and Bash tools.
func PathGuard(extraDenyPatterns ...string) PreHook {
	deny := append(defaultDenyPatterns, extraDenyPatterns...)
	writeTools := map[string]bool{
		"Edit":  true,
		"Write": true,
		"Bash":  true, // checked via heuristic, not guaranteed
	}

	return func(ctx context.Context, tool string, args map[string]any) error {
		if !writeTools[tool] {
			return nil
		}

		// Collect candidate paths from known argument keys
		var candidates []string
		for _, key := range []string{"path", "file_path", "command"} {
			if v, ok := args[key]; ok {
				if s, ok := v.(string); ok && s != "" {
					candidates = append(candidates, s)
				}
			}
		}

		for _, candidate := range candidates {
			if blocked, pattern := matchesDenyList(candidate, deny); blocked {
				return factError("path %q matches denylist pattern %q", candidate, pattern)
			}
		}
		return nil
	}
}

// matchesDenyList returns true if path matches any deny pattern.
func matchesDenyList(path string, patterns []string) (bool, string) {
	clean := filepath.Clean(path)
	base := filepath.Base(clean)

	for _, pattern := range patterns {
		// Exact base name match
		if base == pattern {
			return true, pattern
		}
		// Directory pattern (e.g. ".git/"): match as a path component, not a prefix
		if strings.HasSuffix(pattern, "/") {
			dir := strings.TrimSuffix(pattern, "/")
			if clean == dir ||
				strings.HasPrefix(clean, dir+"/") ||
				strings.Contains(clean, "/"+dir+"/") ||
				strings.HasSuffix(clean, "/"+dir) {
				return true, pattern
			}
			continue
		}
		// Glob match on base name
		if matched, _ := filepath.Match(pattern, base); matched {
			return true, pattern
		}
		// Exact full path match
		if clean == pattern {
			return true, pattern
		}
	}
	return false, ""
}

// ─── GoBuild validator ────────────────────────────────────────────────────────

const goBuildMaxOutputLines = 50
const goBuildTimeout = 2 * time.Minute

// GoBuild returns a PostHook that runs "go build ./..." after any .go file is written.
// workDir is the module root to run the build from.
func GoBuild(workDir string) PostHook {
	return func(ctx context.Context, tool string, args map[string]any, result map[string]any) error {
		if !isGoFileWrite(tool, args) {
			return nil
		}

		buildCtx, cancel := context.WithTimeout(ctx, goBuildTimeout)
		defer cancel()

		cmd := exec.CommandContext(buildCtx, "go", "build", "./...")
		cmd.Dir = workDir
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out

		if err := cmd.Run(); err != nil {
			output := trimLines(out.String(), goBuildMaxOutputLines)
			return factError("go build ./... failed: %s", output)
		}
		return nil
	}
}

// isGoFileWrite returns true when the tool is writing a .go file.
func isGoFileWrite(tool string, args map[string]any) bool {
	if tool != "Edit" && tool != "Write" {
		return false
	}
	for _, key := range []string{"path", "file_path"} {
		if v, ok := args[key]; ok {
			if s, ok := v.(string); ok && strings.HasSuffix(s, ".go") {
				return true
			}
		}
	}
	return false
}

// ─── TscCheck validator ───────────────────────────────────────────────────────

const tscTimeout = 3 * time.Minute

// TscCheck returns a PostHook that runs "tsc --noEmit" after any .ts/.tsx file is written.
// workDir is the directory containing tsconfig.json.
func TscCheck(workDir string) PostHook {
	return func(ctx context.Context, tool string, args map[string]any, result map[string]any) error {
		if !isTsFileWrite(tool, args) {
			return nil
		}

		tscCtx, cancel := context.WithTimeout(ctx, tscTimeout)
		defer cancel()

		cmd := exec.CommandContext(tscCtx, "tsc", "--noEmit")
		cmd.Dir = workDir
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out

		if err := cmd.Run(); err != nil {
			output := trimLines(out.String(), goBuildMaxOutputLines)
			return factError("tsc --noEmit failed: %s", output)
		}
		return nil
	}
}

// isTsFileWrite returns true when the tool is writing a .ts or .tsx file.
func isTsFileWrite(tool string, args map[string]any) bool {
	if tool != "Edit" && tool != "Write" {
		return false
	}
	for _, key := range []string{"path", "file_path"} {
		if v, ok := args[key]; ok {
			if s, ok := v.(string); ok {
				if strings.HasSuffix(s, ".ts") || strings.HasSuffix(s, ".tsx") {
					return true
				}
			}
		}
	}
	return false
}

// ─── JsonLint validator ───────────────────────────────────────────────────────

const jsonLintTimeout = 30 * time.Second

// JsonLint returns a PostHook that runs "jq empty" after any .json file is written.
func JsonLint(workDir string) PostHook {
	return func(ctx context.Context, tool string, args map[string]any, result map[string]any) error {
		if !isJsonFileWrite(tool, args) {
			return nil
		}

		path := extractFilePath(args)
		if path == "" {
			return nil
		}

		jqCtx, cancel := context.WithTimeout(ctx, jsonLintTimeout)
		defer cancel()

		cmd := exec.CommandContext(jqCtx, "jq", "empty", path)
		cmd.Dir = workDir
		var out bytes.Buffer
		cmd.Stderr = &out

		if err := cmd.Run(); err != nil {
			return factError("jq empty %s failed: %s", path, strings.TrimSpace(out.String()))
		}
		return nil
	}
}

// isJsonFileWrite returns true when the tool is writing a .json file.
func isJsonFileWrite(tool string, args map[string]any) bool {
	if tool != "Edit" && tool != "Write" {
		return false
	}
	return strings.HasSuffix(extractFilePath(args), ".json")
}

// ─── helpers ──────────────────────────────────────────────────────────────────

// extractFilePath returns the first non-empty path argument from known keys.
func extractFilePath(args map[string]any) string {
	for _, key := range []string{"path", "file_path"} {
		if v, ok := args[key]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

// trimLines returns at most n lines of s, with a trailing note if truncated.
func trimLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) <= n {
		return s
	}
	truncated := lines[:n]
	truncated = append(truncated, fmt.Sprintf("... (%d more lines omitted)", len(lines)-n))
	return strings.Join(truncated, "\n")
}

// ─── Builder ──────────────────────────────────────────────────────────────────

// HooksConfig controls which hooks are enabled.
type HooksConfig struct {
	PathGuard      bool     `json:"path_guard"`
	ExtraDenyPaths []string `json:"extra_deny_paths"`
	PostValidators []string `json:"post_validators"` // "go_build", "tsc_check", "json_lint"
	WorkDir        string   `json:"work_dir"`        // root for build validators; defaults to project dir
}

// BuildRegistry constructs a HookRegistry from config.
// projectDir is used as the default WorkDir when config.WorkDir is empty.
func BuildRegistry(cfg HooksConfig, projectDir string) *HookRegistry {
	reg := NewHookRegistry()
	workDir := cfg.WorkDir
	if workDir == "" {
		workDir = projectDir
	}

	if cfg.PathGuard {
		reg.RegisterPre(PathGuard(cfg.ExtraDenyPaths...))
	}

	validatorSet := make(map[string]bool, len(cfg.PostValidators))
	for _, v := range cfg.PostValidators {
		validatorSet[v] = true
	}

	if validatorSet["go_build"] {
		reg.RegisterPost(GoBuild(workDir))
	}
	if validatorSet["tsc_check"] {
		reg.RegisterPost(TscCheck(workDir))
	}
	if validatorSet["json_lint"] {
		reg.RegisterPost(JsonLint(workDir))
	}

	return reg
}
