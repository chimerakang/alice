// Fleet types. They mirror proto/alice/v1/fleet.proto, which is what
// /api/fleet/* and the fleet.* WebSocket messages carry.

export type FleetStatus =
  | "queued"
  | "running"
  | "verifying"
  | "review_ready"
  | "needs_decision"
  | "done"
  | "failed"
  | "cancelled";

export type FleetLiveness = "unknown" | "busy" | "idle" | "dead";
export type FleetShape = "ship" | "scout";
export type FleetTrustMode = "local-only" | "direct-pr" | "no-mistakes";
export type FleetNotifyMode = "normal" | "quiet" | "afk";

export interface FleetTopic {
  chat_id: number;
  thread_id: number;
}

export interface FleetProjectBudget {
  task_cost_usd: number;
  daily_cost_usd: number;
}

export interface FleetProject {
  id: string;
  name: string;
  repo_path: string;
  remote: string;
  default_branch: string;
  trust_mode: FleetTrustMode;
  verify_commands: string[];
  max_concurrent: number;
  branch_prefix: string;
  budget: FleetProjectBudget | null;
  topics: FleetTopic[];
  created_at: string | null;
  updated_at: string | null;
}

export interface FleetTask {
  id: string;
  project_id: string;
  project_name: string;
  shape: FleetShape;
  goal: string;
  source_kind: "manual" | "issue";
  issue_number: number;
  trust_mode: FleetTrustMode;
  model: string;
  priority: number;
  status: FleetStatus;
  status_reason: string;
  liveness: FleetLiveness;
  branch: string;
  worktree_path: string;
  session_id: string;
  pr_url: string;
  pr_number: number;
  input_tokens: number;
  output_tokens: number;
  cost_usd: number;
  budget_cost_usd: number;
  created_at: string | null;
  updated_at: string | null;
  started_at: string | null;
  finished_at: string | null;
  last_activity_at: string | null;
}

export interface FleetEvent {
  id: number;
  task_id: string;
  project_id: string;
  type: string;
  /** The event's body, as JSON text. */
  payload_json: string;
  created_at: string | null;
}

export interface FleetEventList {
  events: FleetEvent[];
  next_after_id: number;
  has_more: boolean;
}

export interface FleetVerification {
  id: number;
  task_id: string;
  attempt: number;
  step_index: number;
  name: string;
  command: string;
  passed: boolean;
  exit_code: number;
  output_tail: string;
  duration_ms: number;
  timed_out: boolean;
  error: string;
  created_at: string | null;
}

export interface FleetDiff {
  base: string;
  stat: string;
  patch: string;
  untracked: string[];
  truncated: boolean;
}

export interface FleetCounts {
  queued: number;
  running: number;
  review_ready: number;
  needs_decision: number;
  failed_today: number;
  done_today: number;
}

export interface FleetUsage {
  input_tokens: number;
  output_tokens: number;
  cost_usd: number;
}

export interface FleetProjectDigest {
  project_id: string;
  name: string;
  counts: FleetCounts | null;
  today: FleetUsage | null;
}

export interface FleetDigest {
  generated_at: string | null;
  day_start: string | null;
  projects: FleetProjectDigest[];
  totals: FleetCounts | null;
  today: FleetUsage | null;
  pending_wakes: number;
  notify_mode: FleetNotifyMode;
}

export interface FleetAuditEntry {
  id: number;
  actor: string;
  via: string;
  command: string;
  project_id: string;
  task_id: string;
  detail: string;
  ok: boolean;
  error: string;
  created_at: string | null;
}

export interface FleetDispatchRequest {
  project_id: string;
  goal?: string;
  issue?: number;
  shape?: FleetShape;
  model?: string;
  budget_usd?: number;
  priority?: number;
}

export interface FleetProjectRequest {
  repo_path?: string;
  name?: string;
  trust_mode?: FleetTrustMode;
  verify_commands?: string[];
  max_concurrent?: number;
  budget?: FleetProjectBudget;
}

export interface FleetIssue {
  number: number;
  title: string;
  labels: string[];
  milestone: string;
  url: string;
  /** The unfinished task already working on this issue, if any. */
  task_id: string;
}

export interface FleetWorktree {
  path: string;
  branch: string;
  task_id: string;
  task_status: string;
  dirty: boolean;
  orphan: boolean;
  missing: boolean;
}

export interface FleetProjectCost {
  project_id: string;
  name: string;
  today: FleetUsage | null;
  period: FleetUsage | null;
  daily_limit_usd: number;
  task_limit_usd: number;
}

export interface FleetCosts {
  day_start: string | null;
  period_start: string | null;
  today: FleetUsage | null;
  period: FleetUsage | null;
  daily_limit_usd: number;
  projects: FleetProjectCost[];
  tasks: FleetTask[];
  overruns: FleetTask[];
}

export type FleetTaskAction =
  | "cancel"
  | "retry"
  | "reply"
  | "approve"
  | "merge"
  | "pass"
  | "discard";

/** The error body of every /api/fleet/* endpoint. */
export interface FleetErrorBody {
  error: string;
  code: string;
}
