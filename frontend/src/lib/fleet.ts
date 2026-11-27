// Pure helpers for the fleet pages: no fetching, no React.

import type { DecisionLog, DiffFile, ToolExecution } from "@/types/alice";
import type {
  FleetEvent,
  FleetLiveness,
  FleetStatus,
  FleetTask,
  FleetTaskAction,
} from "@/types/fleet";

/** Every status, in the order a task moves through them. */
export const FLEET_STATUSES: FleetStatus[] = [
  "queued",
  "running",
  "verifying",
  "review_ready",
  "needs_decision",
  "done",
  "failed",
  "cancelled",
];

/** A column of the board and the statuses it gathers. */
export interface FleetColumn {
  key: string;
  statuses: FleetStatus[];
}

/**
 * The board's columns. Running and verifying share one: both mean the fleet
 * is working on it. Done, failed and cancelled share the last, as finished.
 */
export const FLEET_COLUMNS: FleetColumn[] = [
  { key: "queued", statuses: ["queued"] },
  { key: "active", statuses: ["running", "verifying"] },
  { key: "review_ready", statuses: ["review_ready"] },
  { key: "needs_decision", statuses: ["needs_decision"] },
  { key: "finished", statuses: ["done", "failed", "cancelled"] },
];

export function isFleetStatus(value: string): value is FleetStatus {
  return (FLEET_STATUSES as string[]).includes(value);
}

/** Whether the task is waiting on the owner. */
export function needsOwner(status: FleetStatus): boolean {
  return status === "review_ready" || status === "needs_decision";
}

export function isActive(status: FleetStatus): boolean {
  return status === "running" || status === "verifying";
}

export function isFinished(status: FleetStatus): boolean {
  return status === "done" || status === "failed" || status === "cancelled";
}

export type FleetTone = "success" | "error" | "warning" | "info" | "neutral";

export function statusTone(status: FleetStatus): FleetTone {
  switch (status) {
    case "running":
    case "verifying":
      return "info";
    case "review_ready":
    case "needs_decision":
      return "warning";
    case "done":
      return "success";
    case "failed":
      return "error";
    default:
      return "neutral";
  }
}

/**
 * How a task's liveness is shown. "unknown" has a look of its own: it means
 * there is no evidence either way, which must never read as idle.
 */
export function livenessStyle(liveness: FleetLiveness): { dot: string; ring: string; pulse: boolean } {
  switch (liveness) {
    case "busy":
      return { dot: "bg-success", ring: "border-success/40", pulse: true };
    case "idle":
      return { dot: "bg-gray-500", ring: "border-gray-600/40", pulse: false };
    case "dead":
      return { dot: "bg-error", ring: "border-error/50", pulse: false };
    default:
      // Hollow and dashed: nothing is known.
      return { dot: "bg-transparent border border-dashed border-warning", ring: "border-dashed border-warning/40", pulse: false };
  }
}

/** Backend actions are authoritative; missing data must not invent commands. */
export function taskActions(task: Pick<FleetTask, "actions">): FleetTaskAction[] {
 return task.actions ?? [];
}

/** Actions that destroy something and so ask first. */
export function isDestructive(action: FleetTaskAction): boolean {
  return action === "discard" || action === "cancel";
}

export function firstLine(text: string): string {
  const line = (text || "").trim().split(/\r?\n/, 1)[0] ?? "";
  return line.trim();
}

/** Milliseconds as "42s", "3m05s" or "2h05m". */
export function formatDuration(ms: number): string {
  if (!Number.isFinite(ms) || ms < 0) ms = 0;
  const total = Math.round(ms / 1000);
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = total % 60;
  const pad = (n: number) => String(n).padStart(2, "0");
  if (h > 0) return `${h}h${pad(m)}m`;
  if (m > 0) return `${m}m${pad(s)}s`;
  return `${s}s`;
}

/**
 * How long the task's current run has taken, or took. A running task counts
 * up to now; a waiting one stops at its last update.
 */
export function taskElapsedMs(task: FleetTask, now: number = Date.now()): number {
  if (!task.started_at) return 0;
  const start = Date.parse(task.started_at);
  if (!Number.isFinite(start)) return 0;
  let end = now;
  if (task.finished_at) end = Date.parse(task.finished_at);
  else if (!isActive(task.status) && task.updated_at) end = Date.parse(task.updated_at);
  return Math.max(0, end - start);
}

export function formatCost(usd: number): string {
  return `$${(Number.isFinite(usd) ? usd : 0).toFixed(2)}`;
}

/** Share of a limit used, 0..1, or null when there is no limit. */
export function budgetUse(used: number, limit: number): number | null {
  if (!(limit > 0)) return null;
  return Math.max(0, used / limit);
}

export function parsePayload(event: FleetEvent): Record<string, unknown> {
  if (!event.payload_json) return {};
  try {
    const parsed: unknown = JSON.parse(event.payload_json);
    return parsed && typeof parsed === "object" && !Array.isArray(parsed)
      ? (parsed as Record<string, unknown>)
      : {};
  } catch {
    return {};
  }
}

/** Events that say how a task is doing, as opposed to what its agent says. */
export const LIFECYCLE_EVENTS = ["status", "liveness", "verify.result", "wake"];

/**
 * One line describing an event, for the timeline. Returns "" for events
 * with nothing worth a line (e.g. a tool result, shown with its call).
 */
export function describeEvent(event: FleetEvent): string {
  const p = parsePayload(event);
  const str = (key: string) => (typeof p[key] === "string" ? (p[key] as string) : "");
  switch (event.type) {
    case "status": {
      const reason = str("reason");
      const arrow = str("from") ? `${str("from")} → ${str("to")}` : str("to");
      return reason ? `${arrow} · ${reason}` : arrow;
    }
    case "liveness":
      return `${str("from")} → ${str("to")}`;
    case "agent.text":
      return firstLine(str("text"));
    case "agent.tool_use":
      return toolSummary(p);
    case "agent.tool_result":
      return "";
    case "wake":
      return [str("kind"), str("detail")].filter(Boolean).join(" · ");
    case "verify.result": {
      const passed = p.passed === true;
      return `${str("name") || str("command")} ${passed ? "✓" : "✗"}`.trim();
    }
    default:
      return firstLine(str("detail") || str("reason") || str("text") || "");
  }
}

/** "Edit internal/app/main.go", "Bash go test ./...", or just the tool. */
export function toolSummary(payload: Record<string, unknown>): string {
  const tool = typeof payload.tool === "string" ? payload.tool : "";
  if (!tool) return "";
  let input: unknown = payload.input;
  if (typeof input === "string") {
    // The stored input may be clipped mid-way and so not parse.
    try {
      input = JSON.parse(input);
    } catch {
      input = null;
    }
  }
  if (input && typeof input === "object") {
    const fields = input as Record<string, unknown>;
    for (const key of ["file_path", "path", "command", "pattern", "description", "query", "url"]) {
      const value = fields[key];
      if (typeof value === "string" && value.trim()) return `${tool} ${firstLine(value)}`;
    }
  }
  return tool;
}

/**
 * Pairs each tool call with its result and returns them in the shape the
 * existing Gantt chart draws. A call with no result yet is still running.
 */
export function toolExecutionsFromEvents(events: FleetEvent[]): ToolExecution[] {
  const out: ToolExecution[] = [];
  const open = new Map<string, ToolExecution>();
  for (const event of events) {
    if (event.type === "agent.tool_use") {
      const p = parsePayload(event);
      const id = typeof p.id === "string" ? p.id : `event-${event.id}`;
      const exec = {
        id,
        tool_name: typeof p.tool === "string" ? p.tool : "tool",
        timestamp: event.created_at ?? "",
        duration_ms: 0,
        status: "STATUS_RUNNING",
        input: toolSummary(p),
      } as unknown as ToolExecution;
      out.push(exec);
      open.set(id, exec);
    } else if (event.type === "agent.tool_result") {
      const p = parsePayload(event);
      const id = typeof p.tool_use_id === "string" ? p.tool_use_id : "";
      const exec = open.get(id);
      if (!exec) continue;
      const started = Date.parse(String(exec.timestamp));
      const ended = Date.parse(event.created_at ?? "");
      const mutable = exec as unknown as { duration_ms: number; status: string };
      mutable.duration_ms = Number.isFinite(started) && Number.isFinite(ended) ? Math.max(0, ended - started) : 0;
      mutable.status = p.is_error === true ? "STATUS_ERROR" : "STATUS_SUCCESS";
      open.delete(id);
    }
  }
  return out;
}

/** Splits a unified diff into the per-file pieces the diff viewer shows. */
export function parsePatch(patch: string): DiffFile[] {
  if (!patch.trim()) return [];
  const files: DiffFile[] = [];
  // Each file starts at a "diff --git" line.
  const parts = patch.split(/^(?=diff --git )/m);
  for (const part of parts) {
    if (!part.startsWith("diff --git ")) continue;
    const lines = part.split("\n");
    const header = lines[0];
    const match = /^diff --git a\/(.+) b\/(.+)$/.exec(header);
    let path = match ? match[2] : header.replace("diff --git ", "");
    let status: DiffFile["status"] = "modified";
    let additions = 0;
    let deletions = 0;
    let inHunk = false;
    for (const line of lines.slice(1)) {
      if (!inHunk) {
        if (line.startsWith("new file")) status = "added";
        else if (line.startsWith("deleted file")) status = "deleted";
        else if (line.startsWith("rename to ")) path = line.slice("rename to ".length);
        else if (line.startsWith("@@")) inHunk = true;
        continue;
      }
      if (line.startsWith("+")) additions++;
      else if (line.startsWith("-")) deletions++;
    }
    files.push({ path, status, additions, deletions, chunks: lines.slice(1).join("\n") });
  }
  return files;
}

/** Splits batch input into goals: one per non-empty line, list marks removed. */
export function parseBatchGoals(text: string): string[] {
  return text
    .split(/\r?\n/)
    .map((line) => line.replace(/^\s*(?:[-*•]|\d+[.)])\s+/, "").trim())
    .filter((line) => line !== "");
}

/** Groups tasks into the board's columns, newest first within each. */
export function groupByColumn(tasks: FleetTask[]): Record<string, FleetTask[]> {
  const out: Record<string, FleetTask[]> = {};
  for (const column of FLEET_COLUMNS) out[column.key] = [];
  const sorted = [...tasks].sort((a, b) => (b.created_at ?? "").localeCompare(a.created_at ?? ""));
  for (const task of sorted) {
    const column = FLEET_COLUMNS.find((c) => c.statuses.includes(task.status));
    if (column) out[column.key].push(task);
  }
  return out;
}

/**
 * Splits a task's history into its runs, each in the shape the existing
 * timeline entry draws: when it started, what it was told, the tools it
 * called, how long it took and what it cost.
 */
export function runsFromEvents(task: FleetTask, events: FleetEvent[]): DecisionLog[] {
  const runs: { started: FleetEvent; events: FleetEvent[] }[] = [];
  for (const event of events) {
    if (event.type === "run.started") runs.push({ started: event, events: [] });
    else if (runs.length > 0) runs[runs.length - 1].events.push(event);
  }
  return runs.map((run, index) => {
    const start = parsePayload(run.started);
    const result = [...run.events].reverse().find((e) => e.type === "agent.result");
    const totals = result ? parsePayload(result) : {};
    const num = (v: unknown) => (typeof v === "number" && Number.isFinite(v) ? v : 0);
    // A run ends at its result, or at the status change that follows it.
    const last = result ?? [...run.events].reverse().find((e) => e.type === "status") ?? run.events[run.events.length - 1];
    const startedMs = Date.parse(run.started.created_at ?? "");
    const endedMs = Date.parse(last?.created_at ?? "");
    const instruction = typeof start.instruction === "string" ? start.instruction.trim() : "";
    const texts = run.events.filter((e) => e.type === "agent.text");
    const lastText = texts.length > 0 ? parsePayload(texts[texts.length - 1]).text : "";
    return {
      id: `${task.id}-run-${index + 1}`,
      timestamp: run.started.created_at ?? "",
      session_id: task.session_id,
      project_path: task.worktree_path,
      // The first run works on the goal; later ones on what the owner said.
      user_prompt: instruction || (index === 0 ? firstLine(task.goal) : "…"),
      agent_response: typeof lastText === "string" ? lastText : "",
      tool_calls: toolExecutionsFromEvents(run.events),
      task_type: task.shape,
      outcome: { success: totals.is_error !== true } as DecisionLog["outcome"],
      duration_ms:
        num(totals.duration_ms) ||
        (Number.isFinite(startedMs) && Number.isFinite(endedMs) ? Math.max(0, endedMs - startedMs) : 0),
      tokens_input: num(totals.input_tokens),
      tokens_output: num(totals.output_tokens),
      cost_usd: num(totals.cost_usd),
      chat_id: 0,
      thread_id: 0,
      source: "fleet",
      model: task.model,
    } as DecisionLog;
  });
}
