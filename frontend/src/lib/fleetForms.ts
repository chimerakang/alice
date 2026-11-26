// Turning the fleet's forms into requests. Pure, so it can be tested without
// rendering anything.

import { parseBatchGoals } from "@/lib/fleet";
import type { FleetDispatchRequest, FleetProject, FleetProjectRequest, FleetShape, FleetTrustMode } from "@/types/fleet";

export type DispatchMode = "single" | "batch" | "issues";

/** The most tasks one submission dispatches. */
export const MAX_BATCH = 20;

export type DispatchError = "" | "project" | "empty" | "budget" | "too_many";

/** The "create GitHub issue" box starts checked. */
export const DEFAULT_CREATE_ISSUE = true;

export interface GitHubDispatchOptions {
  createIssue: boolean;
  milestone: string;
  epicIssue: number | null;
}

/** What the dispatch form would send, one request per task. */
export function buildDispatchRequests(input: {
  mode: DispatchMode;
  project: string;
  shape: FleetShape;
  model: string;
  budget: string;
  goal: string;
  batch: string;
  issues: number[];
  github?: GitHubDispatchOptions;
}): { requests: FleetDispatchRequest[]; error: DispatchError } {
  if (!input.project) return { requests: [], error: "project" };
  const base: FleetDispatchRequest = { project_id: input.project, shape: input.shape };
  if (input.model.trim()) base.model = input.model.trim();
  if (input.budget.trim()) {
    const usd = Number(input.budget.trim().replace(/^\$/, ""));
    if (!Number.isFinite(usd) || usd <= 0) return { requests: [], error: "budget" };
    base.budget_usd = usd;
  }
  // GitHub issue creation is only supported for single-task mode.
  if (input.github?.createIssue && input.mode === "single") {
    base.create_issue = true;
    if (input.github.milestone.trim()) base.milestone = input.github.milestone.trim();
    if (input.github.epicIssue != null && input.github.epicIssue > 0) base.epic_issue = input.github.epicIssue;
  }
  let requests: FleetDispatchRequest[] = [];
  if (input.mode === "single") {
    if (input.goal.trim()) requests = [{ ...base, goal: input.goal.trim() }];
  } else if (input.mode === "batch") {
    requests = parseBatchGoals(input.batch).map((goal) => ({ ...base, goal }));
  } else {
    requests = input.issues.map((issue) => ({ ...base, issue }));
  }
  if (requests.length === 0) return { requests: [], error: "empty" };
  if (requests.length > MAX_BATCH) return { requests: [], error: "too_many" };
  return { requests, error: "" };
}

/** The project form's fields, as typed. */
export interface ProjectDraft {
  name: string;
  default_branch: string;
  branch_prefix: string;
  trust_mode: FleetTrustMode;
  verify: string;
  max_concurrent: string;
  task_budget: string;
  daily_budget: string;
}

export function projectDraft(p: FleetProject): ProjectDraft {
  return {
    name: p.name,
    default_branch: p.default_branch ?? "",
    branch_prefix: p.branch_prefix ?? "",
    trust_mode: p.trust_mode,
    verify: (p.verify_commands ?? []).join("\n"),
    max_concurrent: String(p.max_concurrent || 1),
    task_budget: p.budget?.task_cost_usd ? String(p.budget.task_cost_usd) : "",
    daily_budget: p.budget?.daily_cost_usd ? String(p.budget.daily_cost_usd) : "",
  };
}

/** Turns the project form into a request, or names the field that is wrong. */
export function projectRequest(d: ProjectDraft): { req: FleetProjectRequest | null; error: "" | "concurrent" | "budget" } {
  const concurrent = Number(d.max_concurrent);
  if (d.max_concurrent.trim() === "" || !Number.isInteger(concurrent) || concurrent < 1) return { req: null, error: "concurrent" };
  const money = (raw: string) => (raw.trim() === "" ? 0 : Number(raw.trim().replace(/^\$/, "")));
  const task = money(d.task_budget);
  const daily = money(d.daily_budget);
  if (!Number.isFinite(task) || !Number.isFinite(daily) || task < 0 || daily < 0) return { req: null, error: "budget" };
  return {
    req: {
      name: d.name.trim(),
      default_branch: d.default_branch.trim(),
      branch_prefix: d.branch_prefix.trim(),
      trust_mode: d.trust_mode,
      verify_commands: d.verify.split("\n").map((l) => l.trim()).filter(Boolean),
      max_concurrent: concurrent,
      budget: { task_cost_usd: task, daily_cost_usd: daily },
    },
    error: "",
  };
}
