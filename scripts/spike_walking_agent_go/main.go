// Spike for issue #149 Phase A — Go-side walking-agent validation.
//
// Mirrors scripts/spike_walking_agent/spike.py but built directly on
// github.com/anthropics/anthropic-sdk-go (Path 2 from the architecture doc).
// If this run produces numbers comparable to the Python spike, Path 2 (Go
// directly calling Messages API) is the recommended integration path for
// Hermes Phase 1.
//
// Usage:
//
//	export ANTHROPIC_API_KEY=sk-ant-...
//	go run ./scripts/spike_walking_agent_go --mode both --workdir /tmp/spike-target
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

const (
	model       = anthropic.ModelClaudeSonnet4_5
	maxTurns    = 8
	bashTimeout = 5 * time.Second
	bashOutCap  = 8000

	// Anthropic Sonnet 4.5 pricing (USD per 1M tokens).
	pricInputPerMTok      = 3.00
	pricOutputPerMTok     = 15.00
	pricCacheReadMult     = 0.10
	pricCacheWrite5mMult  = 1.25
	pricCacheWrite1hMult  = 2.00
	cacheBreakpointMaxLen = 4 // anthropic API limit
)

var subTasks = []string{
	"List all top-level files and directories in the current working directory. Report just the names, no descriptions.",
	"Read the file you saw named CLAUDE.md (or README.md if CLAUDE.md is not present) and report its purpose in one sentence.",
	"Search for any TODO or FIXME comments in *.go files. Report counts per file (top 3 by count). If there are zero, say so.",
	"Run `git log --oneline -5` and report the most recent commit's purpose based on its message.",
	"Cross-reference your previous answers: was the file you read in step 2 mentioned in any of the recent commits from step 4? Yes/no with brief reasoning.",
}

const goal = "Briefly survey the repository's recent state and recent commits to produce a " +
	"lightweight orientation report. This is a read-only exploration task — do NOT " +
	"modify any files."

type turnMetrics struct {
	Idx          int     `json:"idx"`
	Description  string  `json:"description"`
	DurationMs   int     `json:"duration_ms"`
	Input        int     `json:"input_tokens"`
	CacheRead    int     `json:"cache_read"`
	CacheWrite5m int     `json:"cache_write_5m"`
	CacheWrite1h int     `json:"cache_write_1h"`
	Output       int     `json:"output_tokens"`
	CostUSD      float64 `json:"cost_usd"`
}

func (t turnMetrics) totalInput() int { return t.Input + t.CacheRead + t.CacheWrite5m + t.CacheWrite1h }

type runReport struct {
	Mode    string        `json:"mode"`
	Workdir string        `json:"workdir"`
	Model   string        `json:"model"`
	Turns   []turnMetrics `json:"turns"`
}

func (r runReport) totalCost() float64 {
	c := 0.0
	for _, t := range r.Turns {
		c += t.CostUSD
	}
	return c
}

func (r runReport) totalInput() int {
	n := 0
	for _, t := range r.Turns {
		n += t.totalInput()
	}
	return n
}

func (r runReport) totalCacheRead() int {
	n := 0
	for _, t := range r.Turns {
		n += t.CacheRead
	}
	return n
}

func (r runReport) totalCacheWrite() int {
	n := 0
	for _, t := range r.Turns {
		n += t.CacheWrite5m + t.CacheWrite1h
	}
	return n
}

func (r runReport) totalOutput() int {
	n := 0
	for _, t := range r.Turns {
		n += t.Output
	}
	return n
}

func (r runReport) totalDurationMs() int {
	n := 0
	for _, t := range r.Turns {
		n += t.DurationMs
	}
	return n
}

func loadRules() string {
	data, err := os.ReadFile(rulesPath())
	if err != nil {
		log.Fatalf("read rules: %v", err)
	}
	return string(data)
}

func rulesPath() string {
	repoRoot := repoRootDir()
	return filepath.Join(repoRoot, "internal/app/hermes/prompts/executor_rules.md")
}

func repoRootDir() string {
	wd, err := os.Getwd()
	if err != nil {
		log.Fatalf("getwd: %v", err)
	}
	dir := wd
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return wd
}

func bashTool() anthropic.ToolUnionParam {
	return anthropic.ToolUnionParam{
		OfTool: &anthropic.ToolParam{
			Name:        "Bash",
			Description: anthropic.String("Execute a shell command. Read-only operations only — do NOT modify any files. Output capped at 8KB; commands time-boxed to 5 seconds."),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]any{
					"command": map[string]any{
						"type":        "string",
						"description": "The shell command to execute.",
					},
				},
				Required: []string{"command"},
			},
		},
	}
}

func runBash(ctx context.Context, workdir, cmdStr string) string {
	ctx2, cancel := context.WithTimeout(ctx, bashTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx2, "/bin/zsh", "-lc", cmdStr)
	cmd.Dir = workdir
	out, err := cmd.CombinedOutput()
	s := string(out)
	if len(s) > bashOutCap {
		s = s[:bashOutCap] + "\n[... output truncated]"
	}
	if err != nil {
		s += fmt.Sprintf("\n[exit error: %v]", err)
	}
	return s
}

// runOneTurn drives a tool-use loop for a single user prompt. Returns the
// updated history slice (caller-visible), aggregate metrics across however
// many internal model↔tool↔model rounds it took, and the final assistant
// message (for accumulated-text extraction in baseline mode).
func runOneTurn(ctx context.Context, client anthropic.Client, workdir string, history []anthropic.MessageParam, userPrompt string, systemBlocks []anthropic.TextBlockParam) (newHistory []anthropic.MessageParam, finalAssistant anthropic.MessageParam, metrics turnMetrics, err error) {
	t0 := time.Now()
	tools := []anthropic.ToolUnionParam{bashTool()}

	// Append the new user prompt to history.
	history = append(history, anthropic.NewUserMessage(anthropic.NewTextBlock(userPrompt)))

	for round := 0; round < maxTurns; round++ {
		params := anthropic.MessageNewParams{
			Model:     model,
			MaxTokens: 8192,
			System:    systemBlocks,
			Messages:  history,
			Tools:     tools,
		}
		resp, callErr := client.Messages.New(ctx, params)
		if callErr != nil {
			err = fmt.Errorf("messages.New: %w", callErr)
			return
		}

		metrics.Input += int(resp.Usage.InputTokens)
		metrics.CacheRead += int(resp.Usage.CacheReadInputTokens)
		metrics.Output += int(resp.Usage.OutputTokens)
		// CacheCreationInputTokens is the legacy single-value field; we prefer
		// the split via CacheCreation when available.
		w5 := int(resp.Usage.CacheCreation.Ephemeral5mInputTokens)
		w1 := int(resp.Usage.CacheCreation.Ephemeral1hInputTokens)
		if w5 == 0 && w1 == 0 {
			// Fallback: legacy field, assume 5m if SDK didn't split it.
			w5 = int(resp.Usage.CacheCreationInputTokens)
		}
		metrics.CacheWrite5m += w5
		metrics.CacheWrite1h += w1

		// Append assistant response to history (for next round / next sub-task).
		history = append(history, resp.ToParam())

		// Stop if model produced no tool_use blocks.
		hasToolUse := false
		var toolResults []anthropic.ContentBlockParamUnion
		for _, block := range resp.Content {
			if tu := block.AsToolUse(); tu.ID != "" {
				hasToolUse = true
				var input map[string]any
				if err := json.Unmarshal(tu.Input, &input); err != nil {
					log.Printf("[spike] tool_use input unmarshal failed: %v", err)
				}
				cmd, _ := input["command"].(string)
				output := runBash(ctx, workdir, cmd)
				toolResults = append(toolResults, anthropic.NewToolResultBlock(tu.ID, output, false))
			}
		}
		if !hasToolUse {
			finalAssistant = history[len(history)-1] // the assistant message we just produced
			break
		}
		history = append(history, anthropic.NewUserMessage(toolResults...))
	}

	metrics.DurationMs = int(time.Since(t0).Milliseconds())
	metrics.CostUSD = computeCost(metrics)
	if finalAssistant.Role == "" && len(history) > 0 {
		finalAssistant = history[len(history)-1]
	}
	newHistory = history
	return
}

// computeCost calculates Sonnet 4.5 cost from metrics.
func computeCost(m turnMetrics) float64 {
	weightedInput := float64(m.Input) +
		float64(m.CacheRead)*pricCacheReadMult +
		float64(m.CacheWrite5m)*pricCacheWrite5mMult +
		float64(m.CacheWrite1h)*pricCacheWrite1hMult
	return (weightedInput*pricInputPerMTok + float64(m.Output)*pricOutputPerMTok) / 1_000_000
}

// systemBlocks builds the system prompt as cacheable blocks. Putting
// cache_control on the LAST block tells Anthropic to cache the entire prefix
// up through that point.
func systemBlocks(rules, goal string) []anthropic.TextBlockParam {
	return []anthropic.TextBlockParam{
		{
			Text: rules + "\n\nOriginal goal:\n" + goal,
			CacheControl: anthropic.CacheControlEphemeralParam{
				TTL: anthropic.CacheControlEphemeralTTLTTL5m,
			},
		},
	}
}

// runWalking — single conversation, every sub-task is a new user turn that
// shares the same system prompt + cumulative history.
func runWalking(ctx context.Context, client anthropic.Client, workdir string) runReport {
	rules := loadRules()
	sys := systemBlocks(rules, goal)
	r := runReport{Mode: "walking", Workdir: workdir, Model: string(model)}
	var history []anthropic.MessageParam

	for i, st := range subTasks {
		var prompt string
		if i == 0 {
			prompt = fmt.Sprintf("Current sub-task (%d/%d):\n%s", i+1, len(subTasks), st)
		} else {
			prompt = fmt.Sprintf("Now do sub-task (%d/%d):\n%s", i+1, len(subTasks), st)
		}
		newHist, _, m, err := runOneTurn(ctx, client, workdir, history, prompt, sys)
		if err != nil {
			log.Printf("[walking] sub-task %d failed: %v", i+1, err)
			break
		}
		m.Idx = i
		m.Description = truncate(st, 60)
		r.Turns = append(r.Turns, m)
		history = newHist
		fmt.Printf("[walking]  %d/%d done elapsed=%dms cost=$%.4f\n", i+1, len(subTasks), m.DurationMs, m.CostUSD)
	}
	return r
}

// runBaseline — every sub-task is a fresh API call, mimicking Alice's current
// `claude -p` per-sub-task pattern. Each call carries rules+goal+accumulated+subtask.
func runBaseline(ctx context.Context, client anthropic.Client, workdir string) runReport {
	rules := loadRules()
	r := runReport{Mode: "baseline", Workdir: workdir, Model: string(model)}
	accumulated := ""
	for i, st := range subTasks {
		// Each call is fresh, build full prompt and zero history.
		sys := systemBlocks(rules, goal)
		var prompt string
		if accumulated != "" {
			prompt = fmt.Sprintf("Completed sub-task results so far:\n%s\n\nCurrent sub-task (%d/%d):\n%s", accumulated, i+1, len(subTasks), st)
		} else {
			prompt = fmt.Sprintf("Current sub-task (%d/%d):\n%s", i+1, len(subTasks), st)
		}
		var emptyHistory []anthropic.MessageParam
		_, final, m, err := runOneTurn(ctx, client, workdir, emptyHistory, prompt, sys)
		if err != nil {
			log.Printf("[baseline] sub-task %d failed: %v", i+1, err)
			break
		}
		m.Idx = i
		m.Description = truncate(st, 60)
		r.Turns = append(r.Turns, m)
		accumulated = strings.TrimSpace(accumulated + "\n  [" + fmt.Sprintf("%d", i+1) + "] " + truncate(extractText(final), 240))
		fmt.Printf("[baseline] %d/%d done elapsed=%dms cost=$%.4f\n", i+1, len(subTasks), m.DurationMs, m.CostUSD)
	}
	return r
}

// readAPIKeyFromConfig is a convenience for running the spike without
// requiring the operator to export ANTHROPIC_API_KEY separately. It reads
// the same `anthropic_key` field Alice's main config uses. NEVER write
// the value out — only return it.
func readAPIKeyFromConfig() string {
	path := filepath.Join(repoRootDir(), "config.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var cfg struct {
		AnthropicKey string `json:"anthropic_key"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return ""
	}
	return strings.TrimSpace(cfg.AnthropicKey)
}

func extractText(msg anthropic.MessageParam) string {
	for _, b := range msg.Content {
		if b.OfText != nil {
			return b.OfText.Text
		}
	}
	return ""
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func renderSummary(reports []runReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Spike #149 Go Results\n\nModel: `%s`  Workdir: `%s`  Sub-tasks: %d\n\n", reports[0].Model, reports[0].Workdir, len(subTasks))
	fmt.Fprintln(&b, "\n## Per-mode totals\n")
	fmt.Fprintln(&b, "| Mode | Calls | Total cost | Total input | cache_read | cache_write (5m / 1h) | Output | Wallclock |")
	fmt.Fprintln(&b, "|---|---|---|---|---|---|---|---|")
	for _, r := range reports {
		w5 := 0
		w1 := 0
		for _, t := range r.Turns {
			w5 += t.CacheWrite5m
			w1 += t.CacheWrite1h
		}
		fmt.Fprintf(&b, "| %s | %d | $%.4f | %d | %d (%s) | %d / %d | %d | %.1fs |\n",
			r.Mode, len(r.Turns), r.totalCost(), r.totalInput(),
			r.totalCacheRead(), pct(r.totalCacheRead(), r.totalInput()),
			w5, w1, r.totalOutput(), float64(r.totalDurationMs())/1000)
	}

	if len(reports) == 2 {
		var baseline, walking *runReport
		for i := range reports {
			r := &reports[i]
			switch r.Mode {
			case "baseline":
				baseline = r
			case "walking":
				walking = r
			}
		}
		if baseline != nil && walking != nil && baseline.totalCost() > 0 {
			cd := (baseline.totalCost() - walking.totalCost()) / baseline.totalCost() * 100
			ld := float64(baseline.totalDurationMs()-walking.totalDurationMs()) / float64(maxInt(baseline.totalDurationMs(), 1)) * 100
			fmt.Fprintln(&b, "\n## Walking vs baseline\n")
			fmt.Fprintf(&b, "- Cost: **%+.1f%%** (walking %s)\n", cd, ifThen(cd > 0, "cheaper", "more expensive"))
			fmt.Fprintf(&b, "- Wallclock: **%+.1f%%**\n", ld)
		}
	}

	for _, r := range reports {
		fmt.Fprintf(&b, "\n### %s\n\n", r.Mode)
		fmt.Fprintln(&b, "| # | Sub-task | Duration | Input | cache_read | cache_write_5m | cache_write_1h | Output | Cost |")
		fmt.Fprintln(&b, "|---|---|---|---|---|---|---|---|---|")
		for _, t := range r.Turns {
			fmt.Fprintf(&b, "| %d | %s | %dms | %d | %d | %d | %d | %d | $%.4f |\n",
				t.Idx+1, t.Description, t.DurationMs, t.Input, t.CacheRead, t.CacheWrite5m, t.CacheWrite1h, t.Output, t.CostUSD)
		}
	}

	return b.String()
}

func pct(part, whole int) string {
	if whole == 0 {
		return "n/a"
	}
	return fmt.Sprintf("%.1f%%", float64(part)*100/float64(whole))
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func ifThen(cond bool, a, c string) string {
	if cond {
		return a
	}
	return c
}

func main() {
	mode := flag.String("mode", "both", "baseline | walking | both")
	workdir := flag.String("workdir", "", "Project directory to use as cwd for sub-tasks")
	outPrefix := flag.String("out-prefix", "go_run", "Output file prefix")
	flag.Parse()

	if *workdir == "" {
		log.Fatal("--workdir is required")
	}
	abs, err := filepath.Abs(*workdir)
	if err != nil {
		log.Fatalf("workdir abs: %v", err)
	}
	if info, err := os.Stat(abs); err != nil || !info.IsDir() {
		log.Fatalf("workdir is not a directory: %s", abs)
	}

	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		apiKey = readAPIKeyFromConfig()
	}
	if apiKey == "" {
		log.Fatal("ANTHROPIC_API_KEY env var or config.json `anthropic_key` is required")
	}
	client := anthropic.NewClient(option.WithAPIKey(apiKey))
	ctx := context.Background()

	var reports []runReport
	switch *mode {
	case "baseline":
		fmt.Println("=== baseline (separate Messages.New per sub-task) ===")
		reports = append(reports, runBaseline(ctx, client, abs))
	case "walking":
		fmt.Println("=== walking (single conversation history) ===")
		reports = append(reports, runWalking(ctx, client, abs))
	case "both":
		fmt.Println("=== baseline (separate Messages.New per sub-task) ===")
		reports = append(reports, runBaseline(ctx, client, abs))
		fmt.Println("\n=== walking (single conversation history) ===")
		reports = append(reports, runWalking(ctx, client, abs))
	default:
		log.Fatalf("unknown mode: %s", *mode)
	}

	outDir := filepath.Join(repoRootDir(), "scripts/spike_walking_agent_go")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		log.Fatalf("mkdir output: %v", err)
	}
	jsonlPath := filepath.Join(outDir, *outPrefix+".jsonl")
	mdPath := filepath.Join(outDir, *outPrefix+".md")

	jf, err := os.Create(jsonlPath)
	if err != nil {
		log.Fatalf("create jsonl: %v", err)
	}
	defer jf.Close()
	enc := json.NewEncoder(jf)
	for _, r := range reports {
		_ = enc.Encode(r)
	}

	summary := renderSummary(reports)
	if err := os.WriteFile(mdPath, []byte(summary), 0o644); err != nil {
		log.Fatalf("write md: %v", err)
	}

	fmt.Println("\n=== summary ===")
	fmt.Println(summary)
	fmt.Printf("\nResults: %s\n         %s\n", jsonlPath, mdPath)
}
