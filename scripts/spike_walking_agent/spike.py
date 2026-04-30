"""
Spike for issue #149 — single-session walking agent vs per-sub-task fresh CLI.

Two modes, same N sub-tasks, same model, same workdir:
  A. baseline   = mimic Alice's current Hermes path: subprocess.run(['claude','-p',...])
                  per sub-task, each call rebuilds rules + goal + accumulated + subtask.
  B. walking    = one ClaudeSDKClient session; first turn sends rules + goal + subtask_1,
                  subsequent turns send only "Sub-task k: ..." letting transcript carry history.

Records cost / tokens / cache / latency per turn and reports the diff.

Usage:
  .venv-spike/bin/python3 scripts/spike_walking_agent/spike.py --mode both --workdir /tmp/spike-target

Output: writes results.jsonl + a markdown summary to scripts/spike_walking_agent/.
"""
from __future__ import annotations

import argparse
import asyncio
import json
import re
import subprocess
import sys
import time
from dataclasses import asdict, dataclass, field
from pathlib import Path

from claude_agent_sdk import (
    AssistantMessage,
    ClaudeAgentOptions,
    ClaudeSDKClient,
    ResultMessage,
    TextBlock,
)

REPO_ROOT = Path(__file__).resolve().parents[2]
RULES_PATH = REPO_ROOT / "internal/app/hermes/prompts/executor_rules.md"
OUTPUT_DIR = Path(__file__).resolve().parent

# Five read-only sub-tasks that chain on each other so conversational continuity
# matters. Designed to fit in any Go project workdir.
SUB_TASKS = [
    "List all top-level files and directories in the current working directory. Report just the names, no descriptions.",
    "Read the file you saw named CLAUDE.md (or README.md if CLAUDE.md is not present) and report its purpose in one sentence.",
    "Search for any TODO or FIXME comments in *.go files. Report counts per file (top 3 by count). If there are zero, say so.",
    "Run `git log --oneline -5` and report the most recent commit's purpose based on its message.",
    "Cross-reference your previous answers: was the file you read in step 2 mentioned in any of the recent commits from step 4? Yes/no with brief reasoning.",
]

GOAL = (
    "Briefly survey the repository's recent state and recent commits to produce a "
    "lightweight orientation report. This is a read-only exploration task — do NOT "
    "modify any files."
)

MODEL = "claude-sonnet-4-5-20250929"  # match alice executor default tier


# Sonnet 4.5 pricing per 1M tokens (USD).
SONNET_INPUT_USD_PER_MTOK = 3.00
SONNET_OUTPUT_USD_PER_MTOK = 15.00
SONNET_CACHE_READ_MULT = 0.10
SONNET_CACHE_WRITE_5M_MULT = 1.25  # ephemeral_5m_input_tokens
SONNET_CACHE_WRITE_1H_MULT = 2.00  # ephemeral_1h_input_tokens (CLAUDE SDK default)


@dataclass
class TurnMetrics:
    idx: int
    description_short: str
    duration_ms: int
    input_tokens: int = 0
    cache_read: int = 0
    cache_write: int = 0
    cache_write_1h: int = 0  # ephemeral_1h subset of cache_write (SDK default)
    output_tokens: int = 0
    cost_usd: float = 0.0  # as reported by API/CLI
    text_preview: str = ""

    def total_input(self) -> int:
        return self.input_tokens + self.cache_read + self.cache_write

    def cost_normalized_5m(self) -> float:
        """Recompute cost as if all cache_write was at 5m TTL.
        ClaudeSDKClient defaults to 1h cache (2x rate) which is unfair to compare
        against CLI's 5m-cache (1.25x rate). This normalizes to 5m for apples-to-apples.
        """
        write_5m_only = self.cache_write - self.cache_write_1h
        cost = (
            self.input_tokens * SONNET_INPUT_USD_PER_MTOK
            + self.cache_read * SONNET_INPUT_USD_PER_MTOK * SONNET_CACHE_READ_MULT
            + write_5m_only * SONNET_INPUT_USD_PER_MTOK * SONNET_CACHE_WRITE_5M_MULT
            + self.cache_write_1h * SONNET_INPUT_USD_PER_MTOK * SONNET_CACHE_WRITE_5M_MULT  # treat as 5m
            + self.output_tokens * SONNET_OUTPUT_USD_PER_MTOK
        ) / 1_000_000
        return cost


@dataclass
class RunReport:
    mode: str
    workdir: str
    model: str
    turns: list[TurnMetrics] = field(default_factory=list)

    def total_cost(self) -> float:
        return sum(t.cost_usd for t in self.turns)

    def total_cost_normalized_5m(self) -> float:
        return sum(t.cost_normalized_5m() for t in self.turns)

    def total_input(self) -> int:
        return sum(t.total_input() for t in self.turns)

    def total_cache_read(self) -> int:
        return sum(t.cache_read for t in self.turns)

    def total_cache_write(self) -> int:
        return sum(t.cache_write for t in self.turns)

    def total_output(self) -> int:
        return sum(t.output_tokens for t in self.turns)

    def total_duration_ms(self) -> int:
        return sum(t.duration_ms for t in self.turns)


def load_rules() -> str:
    return RULES_PATH.read_text(encoding="utf-8")


def build_baseline_prompt(rules: str, goal: str, accumulated: str, idx: int, total: int, subtask: str) -> str:
    parts = [rules.strip(), "", f"Original goal:\n{goal}", ""]
    if accumulated.strip():
        parts.extend([f"Completed sub-task results so far:\n{accumulated}", ""])
    parts.append(f"Current sub-task ({idx + 1}/{total}):\n{subtask}")
    return "\n\n".join(parts)


def parse_baseline_jsonl(stdout: str) -> dict:
    """Pull final usage figures from `claude -p --output-format stream-json` output."""
    in_tokens = cache_read = cache_write = out_tokens = 0
    cost_usd = 0.0
    text = ""
    for line in stdout.splitlines():
        line = line.strip()
        if not line:
            continue
        try:
            ev = json.loads(line)
        except json.JSONDecodeError:
            continue
        if ev.get("type") == "result":
            usage = ev.get("usage", {}) or {}
            in_tokens = int(usage.get("input_tokens", 0) or 0)
            cache_read = int(usage.get("cache_read_input_tokens", 0) or 0)
            cache_write = int(usage.get("cache_creation_input_tokens", 0) or 0)
            out_tokens = int(usage.get("output_tokens", 0) or 0)
            cost_usd = float(ev.get("total_cost_usd", 0.0) or 0.0)
            text = ev.get("result", "") or ""
    return {
        "input_tokens": in_tokens,
        "cache_read": cache_read,
        "cache_write": cache_write,
        "output_tokens": out_tokens,
        "cost_usd": cost_usd,
        "text": text,
    }


def run_baseline(workdir: str) -> RunReport:
    rules = load_rules()
    accumulated = ""
    report = RunReport(mode="baseline", workdir=workdir, model=MODEL)
    for i, subtask in enumerate(SUB_TASKS):
        prompt = build_baseline_prompt(rules, GOAL, accumulated, i, len(SUB_TASKS), subtask)
        args = [
            "claude",
            "-p",
            "--output-format", "stream-json",
            "--verbose",
            "--model", MODEL,
            "--dangerously-skip-permissions",
            "--max-turns", "8",
            prompt,
        ]
        t0 = time.monotonic()
        proc = subprocess.run(args, capture_output=True, text=True, cwd=workdir, timeout=180)
        elapsed = int((time.monotonic() - t0) * 1000)
        if proc.returncode != 0:
            print(f"[baseline] sub-task {i+1}: claude exited {proc.returncode}\n  stderr={proc.stderr[:300]}", file=sys.stderr)
        usage = parse_baseline_jsonl(proc.stdout)
        text = usage["text"]
        accumulated_line = _accumulate_summary(text, i + 1)
        accumulated = (accumulated + "\n" + accumulated_line).strip()
        report.turns.append(TurnMetrics(
            idx=i,
            description_short=subtask[:60],
            duration_ms=elapsed,
            input_tokens=usage["input_tokens"],
            cache_read=usage["cache_read"],
            cache_write=usage["cache_write"],
            output_tokens=usage["output_tokens"],
            cost_usd=usage["cost_usd"],
            text_preview=text[:200],
        ))
        print(f"[baseline] {i+1}/{len(SUB_TASKS)} done elapsed={elapsed}ms cost=${usage['cost_usd']:.4f}")
    return report


def _accumulate_summary(text: str, idx: int) -> str:
    text = (text or "").strip()
    first_line = text.splitlines()[0] if text else ""
    return f"  [{idx}] {first_line[:240]}"


async def run_walking(workdir: str) -> RunReport:
    rules = load_rules()
    report = RunReport(mode="walking", workdir=workdir, model=MODEL)

    options = ClaudeAgentOptions(
        model=MODEL,
        cwd=workdir,
        permission_mode="bypassPermissions",
        max_turns=8,
        system_prompt={"type": "preset", "preset": "claude_code"},
    )

    async with ClaudeSDKClient(options=options) as client:
        for i, subtask in enumerate(SUB_TASKS):
            if i == 0:
                prompt = (
                    rules.strip()
                    + "\n\n"
                    + f"Original goal:\n{GOAL}\n\n"
                    + f"Current sub-task ({i+1}/{len(SUB_TASKS)}):\n{subtask}"
                )
            else:
                prompt = f"Now do sub-task ({i+1}/{len(SUB_TASKS)}):\n{subtask}"

            t0 = time.monotonic()
            await client.query(prompt)
            text_pieces: list[str] = []
            metrics = TurnMetrics(idx=i, description_short=subtask[:60], duration_ms=0)
            async for msg in client.receive_response():
                if isinstance(msg, AssistantMessage):
                    for block in msg.content:
                        if isinstance(block, TextBlock):
                            text_pieces.append(block.text)
                elif isinstance(msg, ResultMessage):
                    metrics.duration_ms = int((time.monotonic() - t0) * 1000)
                    usage = msg.usage or {}
                    metrics.input_tokens = int(usage.get("input_tokens", 0) or 0)
                    metrics.cache_read = int(usage.get("cache_read_input_tokens", 0) or 0)
                    metrics.cache_write = int(usage.get("cache_creation_input_tokens", 0) or 0)
                    cache_creation = usage.get("cache_creation", {}) or {}
                    metrics.cache_write_1h = int(cache_creation.get("ephemeral_1h_input_tokens", 0) or 0)
                    metrics.output_tokens = int(usage.get("output_tokens", 0) or 0)
                    metrics.cost_usd = float(msg.total_cost_usd or 0.0)
            text = "\n".join(text_pieces)
            metrics.text_preview = text[:200]
            report.turns.append(metrics)
            print(f"[walking]  {i+1}/{len(SUB_TASKS)} done elapsed={metrics.duration_ms}ms cost=${metrics.cost_usd:.4f}")
    return report


def render_summary(reports: list[RunReport]) -> str:
    lines = []
    lines.append("# Spike #149 Results\n")
    lines.append(f"Model: `{reports[0].model}`  Workdir: `{reports[0].workdir}`  Sub-tasks: {len(SUB_TASKS)}\n")

    lines.append("\n## Per-mode totals\n")
    lines.append("| Mode | Calls | Cost (reported) | Cost (5m-norm) | Total input | cache_read | cache_write (1h share) | Output | Wallclock |")
    lines.append("|---|---|---|---|---|---|---|---|---|")
    for r in reports:
        cw_1h = sum(t.cache_write_1h for t in r.turns)
        lines.append(
            f"| {r.mode} | {len(r.turns)} | ${r.total_cost():.4f} | ${r.total_cost_normalized_5m():.4f} | "
            f"{r.total_input():,} | {r.total_cache_read():,} ({pct(r.total_cache_read(), r.total_input())}) | "
            f"{r.total_cache_write():,} ({cw_1h:,} @1h) | "
            f"{r.total_output():,} | {r.total_duration_ms()/1000:.1f}s |"
        )

    if len(reports) == 2:
        baseline = next((r for r in reports if r.mode == "baseline"), None)
        walking = next((r for r in reports if r.mode == "walking"), None)
        if baseline and walking and baseline.total_cost() > 0:
            cost_reported = (baseline.total_cost() - walking.total_cost()) / baseline.total_cost() * 100
            cost_norm = (baseline.total_cost_normalized_5m() - walking.total_cost_normalized_5m()) / baseline.total_cost_normalized_5m() * 100
            create_delta = (baseline.total_cache_write() - walking.total_cache_write()) / max(baseline.total_cache_write(), 1) * 100
            lat_delta = (baseline.total_duration_ms() - walking.total_duration_ms()) / max(baseline.total_duration_ms(), 1) * 100
            lines.append("\n## Walking vs baseline\n")
            lines.append(f"- Cost reported (SDK 1h cache vs CLI 5m): **{cost_reported:+.1f}%** (walking {'cheaper' if cost_reported > 0 else 'more expensive'} as billed today)")
            lines.append(f"- **Cost 5m-normalized (apples-to-apples)**: **{cost_norm:+.1f}%** (walking {'cheaper' if cost_norm > 0 else 'more expensive'} if SDK cache TTL matched CLI)")
            lines.append(f"- Cache_creation tokens: **{create_delta:+.1f}%**")
            lines.append(f"- Wallclock: **{lat_delta:+.1f}%**")

    lines.append("\n## Per-turn detail\n")
    for r in reports:
        lines.append(f"\n### {r.mode}\n")
        lines.append("| # | Sub-task | Duration | Input | cache_read | cache_write | Output | Cost |")
        lines.append("|---|---|---|---|---|---|---|---|")
        for t in r.turns:
            lines.append(
                f"| {t.idx+1} | {t.description_short} | {t.duration_ms}ms | "
                f"{t.input_tokens} | {t.cache_read} | {t.cache_write} | "
                f"{t.output_tokens} | ${t.cost_usd:.4f} |"
            )
    return "\n".join(lines) + "\n"


def pct(part: int, whole: int) -> str:
    if whole <= 0:
        return "n/a"
    return f"{part * 100 / whole:.1f}%"


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--mode", choices=["baseline", "walking", "both"], default="both")
    parser.add_argument("--workdir", required=True, help="Existing project directory to use as cwd (read-only sub-tasks).")
    parser.add_argument("--out-prefix", default="run")
    args = parser.parse_args()

    workdir = str(Path(args.workdir).resolve())
    if not Path(workdir).is_dir():
        print(f"workdir does not exist: {workdir}", file=sys.stderr)
        return 1

    reports: list[RunReport] = []
    if args.mode in ("baseline", "both"):
        print(f"\n=== baseline (per-sub-task fresh `claude -p`) ===")
        reports.append(run_baseline(workdir))
    if args.mode in ("walking", "both"):
        print(f"\n=== walking (single ClaudeSDKClient session) ===")
        reports.append(asyncio.run(run_walking(workdir)))

    OUTPUT_DIR.mkdir(parents=True, exist_ok=True)
    jsonl_path = OUTPUT_DIR / f"{args.out_prefix}.jsonl"
    md_path = OUTPUT_DIR / f"{args.out_prefix}.md"
    with jsonl_path.open("w", encoding="utf-8") as f:
        for r in reports:
            f.write(json.dumps({**asdict(r), "turns": [asdict(t) for t in r.turns]}, ensure_ascii=False) + "\n")
    md_path.write_text(render_summary(reports), encoding="utf-8")

    print("\n=== summary ===")
    print(md_path.read_text(encoding="utf-8"))
    print(f"\nResults: {jsonl_path}\n         {md_path}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
