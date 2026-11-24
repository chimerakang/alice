import { describe, expect, it } from "vitest";
import {
  FLEET_COLUMNS,
  FLEET_STATUSES,
  budgetUse,
  describeEvent,
  formatDuration,
  groupByColumn,
  livenessStyle,
  needsOwner,
  parseBatchGoals,
  parsePatch,
  taskActions,
  taskElapsedMs,
  toolExecutionsFromEvents,
  toolSummary,
} from "./fleet";
import type { FleetEvent, FleetTask } from "@/types/fleet";

function task(over: Partial<FleetTask>): FleetTask {
  return {
    id: "0123456789ab", project_id: "app", project_name: "app", shape: "ship", goal: "do it",
    source_kind: "manual", issue_number: 0, trust_mode: "local-only", model: "", priority: 0,
    status: "queued", status_reason: "", liveness: "unknown", branch: "", worktree_path: "",
    session_id: "", pr_url: "", pr_number: 0, input_tokens: 0, output_tokens: 0, cost_usd: 0,
    budget_cost_usd: 0, created_at: "2026-01-01T10:00:00Z", updated_at: "2026-01-01T10:00:00Z",
    started_at: null, finished_at: null, last_activity_at: null, ...over,
  };
}

function event(id: number, type: string, payload: unknown, at = "2026-01-01T10:00:00Z"): FleetEvent {
  return { id, task_id: "t", project_id: "app", type, payload_json: JSON.stringify(payload), created_at: at };
}

describe("board columns", () => {
  it("place every status in exactly one column", () => {
    for (const status of FLEET_STATUSES) {
      const homes = FLEET_COLUMNS.filter((c) => c.statuses.includes(status));
      expect(homes, status).toHaveLength(1);
    }
  });

  it("groups tasks, newest first", () => {
    const grouped = groupByColumn([
      task({ id: "a", status: "running", created_at: "2026-01-01T10:00:00Z" }),
      task({ id: "b", status: "verifying", created_at: "2026-01-01T11:00:00Z" }),
      task({ id: "c", status: "failed" }),
      task({ id: "d", status: "needs_decision" }),
    ]);
    expect(grouped.active.map((t) => t.id)).toEqual(["b", "a"]);
    expect(grouped.finished.map((t) => t.id)).toEqual(["c"]);
    expect(grouped.needs_decision.map((t) => t.id)).toEqual(["d"]);
    expect(grouped.queued).toEqual([]);
  });
});

describe("taskActions", () => {
  it("offers what each status allows", () => {
    expect(taskActions(task({ status: "queued" }))).toEqual(["cancel"]);
    expect(taskActions(task({ status: "running" }))).toEqual(["cancel"]);
    expect(taskActions(task({ status: "review_ready" }))).toEqual(["approve", "reply", "discard"]);
    expect(taskActions(task({ status: "needs_decision" }))).toEqual(["reply", "retry", "pass", "discard"]);
    expect(taskActions(task({ status: "failed" }))).toEqual(["retry", "reply", "discard"]);
    expect(taskActions(task({ status: "cancelled" }))).toEqual([]);
  });

  it("offers merge only for a finished direct-pr task with a pull request", () => {
    expect(taskActions(task({ status: "done" }))).toEqual([]);
    expect(taskActions(task({ status: "done", trust_mode: "direct-pr" }))).toEqual([]);
    expect(taskActions(task({ status: "done", trust_mode: "direct-pr", pr_number: 7 }))).toEqual(["merge"]);
    expect(taskActions(task({ status: "done", trust_mode: "no-mistakes", pr_number: 7 }))).toEqual([]);
  });

  it("knows which statuses wait on the owner", () => {
    expect(FLEET_STATUSES.filter(needsOwner)).toEqual(["review_ready", "needs_decision"]);
  });
});

describe("liveness", () => {
  it("gives unknown a look of its own, unlike idle", () => {
    const unknown = livenessStyle("unknown");
    const idle = livenessStyle("idle");
    expect(unknown.dot).not.toBe(idle.dot);
    expect(unknown.dot).toContain("dashed");
    expect(livenessStyle("busy").pulse).toBe(true);
    expect(livenessStyle("dead").dot).toContain("error");
  });
});

describe("time and money", () => {
  it("formats durations", () => {
    expect(formatDuration(0)).toBe("0s");
    expect(formatDuration(42_000)).toBe("42s");
    expect(formatDuration(95_000)).toBe("1m35s");
    expect(formatDuration(2 * 3600_000 + 5 * 60_000)).toBe("2h05m");
    expect(formatDuration(-5)).toBe("0s");
    expect(formatDuration(Number.NaN)).toBe("0s");
  });

  it("measures the current run", () => {
    const now = Date.parse("2026-01-01T12:05:00Z");
    expect(taskElapsedMs(task({}), now)).toBe(0);
    expect(taskElapsedMs(task({ status: "running", started_at: "2026-01-01T10:00:00Z" }), now)).toBe(125 * 60_000);
    expect(
      taskElapsedMs(task({ status: "done", started_at: "2026-01-01T10:00:00Z", finished_at: "2026-01-01T10:01:35Z" }), now),
    ).toBe(95_000);
    // Waiting on the owner: the clock stopped at the last update.
    expect(
      taskElapsedMs(task({ status: "review_ready", started_at: "2026-01-01T10:00:00Z", updated_at: "2026-01-01T10:00:42Z" }), now),
    ).toBe(42_000);
  });

  it("reports budget use only when there is a limit", () => {
    expect(budgetUse(1, 0)).toBeNull();
    expect(budgetUse(1, 4)).toBe(0.25);
    expect(budgetUse(6, 4)).toBe(1.5);
  });
});

describe("events", () => {
  it("describes them in one line", () => {
    expect(describeEvent(event(1, "status", { from: "running", to: "verifying", reason: "agent finished" }))).toBe(
      "running → verifying · agent finished",
    );
    expect(describeEvent(event(2, "status", { to: "queued", reason: "created" }))).toBe("queued · created");
    expect(describeEvent(event(3, "liveness", { from: "unknown", to: "busy" }))).toBe("unknown → busy");
    expect(describeEvent(event(4, "agent.text", { text: "First line\nsecond" }))).toBe("First line");
    expect(describeEvent(event(5, "agent.tool_use", { tool: "Bash", input: { command: "go test ./...\necho" } }))).toBe(
      "Bash go test ./...",
    );
    expect(describeEvent(event(6, "agent.tool_result", { tool_use_id: "x" }))).toBe("");
    expect(describeEvent(event(7, "wake", { kind: "question", detail: "agent is waiting" }))).toBe("question · agent is waiting");
    expect(describeEvent({ ...event(8, "status", {}), payload_json: "not json" })).toBe("");
  });

  it("summarises a tool call even when its input was clipped", () => {
    expect(toolSummary({ tool: "Read", input: '{"file_path":"clipped mid-w' })).toBe("Read");
    expect(toolSummary({ tool: "Edit", input: '{"file_path":"a.go"}' })).toBe("Edit a.go");
    expect(toolSummary({ tool: "TodoWrite", input: {} })).toBe("TodoWrite");
    expect(toolSummary({})).toBe("");
  });

  it("pairs tool calls with their results for the Gantt chart", () => {
    const execs = toolExecutionsFromEvents([
      event(1, "agent.tool_use", { id: "a", tool: "Read", input: { file_path: "x.go" } }, "2026-01-01T10:00:00Z"),
      event(2, "agent.tool_use", { id: "b", tool: "Bash", input: { command: "go test" } }, "2026-01-01T10:00:01Z"),
      event(3, "agent.tool_result", { tool_use_id: "a", is_error: false }, "2026-01-01T10:00:02Z"),
      event(4, "agent.tool_result", { tool_use_id: "b", is_error: true }, "2026-01-01T10:00:31Z"),
      event(5, "agent.tool_use", { id: "c", tool: "Edit", input: {} }, "2026-01-01T10:00:40Z"),
      event(6, "agent.tool_result", { tool_use_id: "zzz" }, "2026-01-01T10:00:41Z"),
    ]);
    expect(execs.map((e) => [e.tool_name, e.duration_ms, String(e.status)])).toEqual([
      ["Read", 2000, "STATUS_SUCCESS"],
      ["Bash", 30000, "STATUS_ERROR"],
      ["Edit", 0, "STATUS_RUNNING"],
    ]);
  });
});

describe("parsePatch", () => {
  const patch = [
    "diff --git a/src/a.go b/src/a.go",
    "index 111..222 100644",
    "--- a/src/a.go",
    "+++ b/src/a.go",
    "@@ -1,3 +1,4 @@",
    " package a",
    "-old",
    "+new",
    "+added",
    "diff --git a/new.txt b/new.txt",
    "new file mode 100644",
    "index 000..333",
    "--- /dev/null",
    "+++ b/new.txt",
    "@@ -0,0 +1 @@",
    "+hello",
    "diff --git a/gone.txt b/gone.txt",
    "deleted file mode 100644",
    "--- a/gone.txt",
    "+++ /dev/null",
    "@@ -1,2 +0,0 @@",
    "-one",
    "-two",
    "diff --git a/old name.md b/new name.md",
    "similarity index 100%",
    "rename from old name.md",
    "rename to new name.md",
  ].join("\n");

  it("splits a unified diff into files with their counts", () => {
    const files = parsePatch(patch);
    expect(files.map((f) => [f.path, f.status, f.additions, f.deletions])).toEqual([
      ["src/a.go", "modified", 2, 1],
      ["new.txt", "added", 1, 0],
      ["gone.txt", "deleted", 0, 2],
      ["new name.md", "modified", 0, 0],
    ]);
    // File headers are not counted as changes.
    expect(files[0].chunks).toContain("@@ -1,3 +1,4 @@");
  });

  it("returns nothing for an empty diff", () => {
    expect(parsePatch("")).toEqual([]);
    expect(parsePatch("  \n")).toEqual([]);
  });
});

describe("parseBatchGoals", () => {
  it("takes one goal per line and drops list marks", () => {
    expect(parseBatchGoals("1. 更新 README\n- 補上測試\n\n* fix lint\n2) last one\n  plain line  ")).toEqual([
      "更新 README",
      "補上測試",
      "fix lint",
      "last one",
      "plain line",
    ]);
    expect(parseBatchGoals("\n  \n")).toEqual([]);
    // A leading number that is part of the goal stays.
    expect(parseBatchGoals("3 retries are enough")).toEqual(["3 retries are enough"]);
  });
});

describe("runsFromEvents", () => {
  it("splits a task's history into runs for the timeline", async () => {
    const { runsFromEvents } = await import("./fleet");
    const t = task({ id: "abc", goal: "Fix the login\nmore detail", session_id: "s1", model: "opus" });
    const runs = runsFromEvents(t, [
      event(1, "status", { to: "queued" }, "2026-01-01T10:00:00Z"),
      event(2, "run.started", {}, "2026-01-01T10:00:05Z"),
      event(3, "agent.tool_use", { id: "a", tool: "Read", input: { file_path: "x.go" } }, "2026-01-01T10:00:06Z"),
      event(4, "agent.tool_result", { tool_use_id: "a" }, "2026-01-01T10:00:07Z"),
      event(5, "agent.text", { text: "Which approach, A or B?" }, "2026-01-01T10:00:08Z"),
      event(6, "agent.result", { is_error: false, duration_ms: 3000, input_tokens: 100, output_tokens: 40, cost_usd: 0.02 }, "2026-01-01T10:00:08Z"),
      event(7, "status", { from: "running", to: "needs_decision" }, "2026-01-01T10:00:09Z"),
      event(8, "run.started", { instruction: "Use B" }, "2026-01-01T10:05:00Z"),
      event(9, "agent.tool_use", { id: "b", tool: "Edit", input: { file_path: "y.go" } }, "2026-01-01T10:05:01Z"),
      event(10, "status", { from: "running", to: "failed" }, "2026-01-01T10:05:31Z"),
    ]);
    expect(runs).toHaveLength(2);
    expect(runs[0]).toMatchObject({
      id: "abc-run-1", timestamp: "2026-01-01T10:00:05Z", user_prompt: "Fix the login",
      agent_response: "Which approach, A or B?", duration_ms: 3000, tokens_input: 100, tokens_output: 40,
      cost_usd: 0.02, source: "fleet", model: "opus",
    });
    expect(runs[0].outcome.success).toBe(true);
    expect(runs[0].tool_calls.map((c) => c.tool_name)).toEqual(["Read"]);
    // The second run is about what the owner said, and has no result: it
    // ended at the status change, 31 seconds after it started.
    expect(runs[1].user_prompt).toBe("Use B");
    expect(runs[1].duration_ms).toBe(31000);
    expect(runs[1].tool_calls.map((c) => [c.tool_name, String(c.status)])).toEqual([["Edit", "STATUS_RUNNING"]]);
  });

  it("returns nothing for a task that has not run", async () => {
    const { runsFromEvents } = await import("./fleet");
    expect(runsFromEvents(task({}), [event(1, "status", { to: "queued" })])).toEqual([]);
  });
});
