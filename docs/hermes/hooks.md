# Hermes Tool Execution Hooks

Package `internal/app/hermes` provides lightweight hook infrastructure that runs at tool boundaries during Hermes Brain-Executor mode.

Alice delegates actual tool execution to the Claude Code CLI. Hooks do **not** reimplement CLI tools — they add two things the CLI cannot provide on its own:

| What hooks add | Why CLI alone can't do it |
|---|---|
| Path guard (pre-execution) | CLI trusts the agent; unattended Hermes mode needs its own safety layer |
| Build/type validators (post-execution) | Catches compile errors immediately after a write, before the next agent turn |

---

## Concepts

### PreHook

```go
type PreHook func(ctx context.Context, tool string, args map[string]any) error
```

Runs before tool execution. Return a non-nil error to **block** the tool. The error is returned to the caller as-is.

### PostHook

```go
type PostHook func(ctx context.Context, tool string, args map[string]any, result map[string]any) error
```

Runs after a tool succeeds. A returned error is attached to `result["validation_error"]` and does **not** fail the tool call — the agent sees the error inline and can decide what to do.

### HookRegistry

Holds registered hooks and provides `RunPre` / `RunPost` execution. Pre-hooks stop at the first error.

---

## Built-in Hooks

### PathGuard (pre-hook)

Blocks write operations (`Edit`, `Write`, `Bash`) to protected paths.

**Default deny list:**

| Pattern | Reason |
|---|---|
| `config.json` | Runtime secrets (tokens, API keys) |
| `.git/` | Git internals |
| `.env`, `.env.*` | Environment secrets |
| `*.pem`, `*.key` | TLS certificates and private keys |
| `id_rsa`, `id_ed25519` | SSH private keys |

Extra patterns can be added via config (`extra_deny_paths`).

**Error format** (plain fact, no blame language):

```
Error: path "config.json" matches denylist pattern "config.json"
```

### GoBuild (post-hook)

After any `.go` file is written, runs `go build ./...` from `work_dir`. On failure, returns the first 50 lines of stderr.

### TscCheck (post-hook)

After any `.ts` or `.tsx` file is written, runs `tsc --noEmit` from `work_dir`.

### JsonLint (post-hook)

After any `.json` file is written, runs `jq empty <path>` to validate JSON syntax.

---

## Configuration

Add to `config.json` under the `hermes` key:

```json
{
  "hermes": {
    "enabled": false,
    "hooks": {
      "path_guard": true,
      "extra_deny_paths": ["secrets.yaml"],
      "post_validators": ["go_build", "json_lint"],
      "work_dir": ""
    }
  }
}
```

| Field | Default | Description |
|---|---|---|
| `enabled` | `false` | Enable Hermes mode |
| `hooks.path_guard` | `false` | Enable PathGuard pre-hook |
| `hooks.extra_deny_paths` | `[]` | Additional path patterns to block |
| `hooks.post_validators` | `[]` | Active validators: `"go_build"`, `"tsc_check"`, `"json_lint"` |
| `hooks.work_dir` | `""` | Working directory for validators; defaults to `default_project_dir` |

**Hermes mode default**: `path_guard` is automatically enabled when `hermes.enabled = true`.

---

## Usage

```go
import "claude-tg-agent/internal/app/hermes"

// Build registry from config
cfg := hermes.HooksConfig{
    PathGuard:      true,
    PostValidators: []string{"go_build"},
    WorkDir:        projectDir,
}
reg := hermes.BuildRegistry(cfg, projectDir)

// Around a tool call:
if err := reg.RunPre(ctx, toolName, args); err != nil {
    return err  // blocked — return factual error to agent
}
result, err := executeTool(ctx, toolName, args)
if err == nil {
    reg.RunPost(ctx, toolName, args, result)
    // result["validation_error"] is set if any post-hook failed
}
```

---

## Error Message Convention

All hook errors use plain-fact language — no blame, no "you made a mistake":

```
Error: path "config.json" matches denylist pattern "config.json"
Error: go build ./... failed: ./main.go:42:5: undefined: Foo
```

This keeps error messages machine-parseable and reduces noise for Haiku, which is sensitive to emotional framing.

---

## Related

- Issue [#96](https://github.com/chimerakang/alice/issues/96) — this implementation
- Issue [#95](https://github.com/chimerakang/alice/issues/95) — Hermes epic (Brain-Executor architecture)
- Issue [#97](https://github.com/chimerakang/alice/issues/97) — Task State persistence (next step)
- Issue [#98](https://github.com/chimerakang/alice/issues/98) — Planner-Executor dual CLI sessions
