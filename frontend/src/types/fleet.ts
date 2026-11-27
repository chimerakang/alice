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
  github_available: boolean;
}

export interface FleetBlockedAction {
 action: FleetTaskAction;
 reason: string;
 items: string[];
}

export interface FleetTask {
  startup_recovered?: boolean;
  startup_resume_capped?: boolean;
 /** Commands permitted by the backend for the current acceptance state. */
 actions?: FleetTaskAction[];
 blocked_actions?: FleetBlockedAction[];
  id: string;
  project_id: string;
  project_name: string;
  shape: FleetShape;
  goal: string;
  source_kind: "manual" | "issue";
  issue_number: number;
  trust_mode: FleetTrustMode;
  model: string;
  /** Who chose the model; empty when the default is used. */
  model_source?: "manual" | "commander" | "";
  model_tier?: string;
  model_reason?: string;
  model_reason_detail?: string;
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
  /** Set in dispatch response when a GitHub issue was just created. */
  issue_url?: string;
  /** Non-fatal warning from dispatch, e.g. epic update failed. */
  warning?: string;
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

export interface FleetDeliveryItem {
  code: string;
  detail: string;
}

export interface FleetDeliveryCheck {
  name: string;
  command: string;
  passed: boolean;
  exit_code: number;
  duration_ms: number;
  timed_out: boolean;
}

export interface FleetDeliveryRun {
  number: number;
  started_at: string | null;
  outcome: "running" | "passed" | "failed" | "not_verified";
  attempt: number;
  stop_reason: string;
}

/** What a reviewer needs before approving a ship task. */
export interface FleetDelivery {
  task_id: string;
  shape: string;
  status: string;
  approvable: boolean;
  verification: "not_configured" | "not_run" | "failed" | "passed";
  checks_run: number;
  checks_attempt: number;
  checks_previous: boolean;
  checks: FleetDeliveryCheck[];
  diff_known: boolean;
  files: number;
  insertions: number;
  deletions: number;
  untracked: number;
  note: string;
  note_missing: boolean;
  stop_reason: string;
  runs: FleetDeliveryRun[];
  completed: FleetDeliveryItem[];
  pending: FleetDeliveryItem[];
  limits: FleetDeliveryItem[];
  next: "approve" | "decide" | "rerun" | "wait" | "none";
  pr_url: string;
  pr_number: number;
  /** Per-item acceptance; empty when the task has no criteria on record. */
  acceptance_items: FleetAcceptanceItem[];
  acceptance: FleetAcceptanceSummary;
}

export type FleetAcceptanceReport = "evidence_submitted" | "needs_evidence" | "awaiting_external" | "not_reported";
export type FleetAcceptanceDecision = "pending" | "approved" | "needs_evidence" | "awaiting_external";

export interface FleetAcceptanceEvidence {
  type: "commit" | "pr" | "report" | "test" | "file";
  ref: string;
  paths: string[];
  note: string;
}

export interface FleetAcceptanceHistory {
  at: string | null;
  actor: string;
  kind: "report" | "decision" | "reopened";
  status: string;
  note: string;
  run: number;
  round: number;
}

export interface FleetAcceptanceItem {
  id: string;
  text: string;
  version: number;
  report: FleetAcceptanceReport;
  evidence: FleetAcceptanceEvidence[];
  blocker: { missing: string; owner: string } | null;
  round: number;
  decision: FleetAcceptanceDecision;
  note: string;
  decided_by: string;
  reopened: "" | "criteria_changed" | "evidence_changed";
  history: FleetAcceptanceHistory[];
}

export interface FleetCriteriaProposal {
  version: number;
  added: { id: string; text: string }[];
  removed: { id: string; text: string }[];
  changed: { id: string; old: string; text: string }[];
}

export interface FleetAcceptanceSummary {
  structured: boolean;
  version: number;
  approved: number;
  total: number;
  rounds: number;
  external_wait: boolean;
  gate_ok: boolean;
  gate_code: "" | "items_not_approved" | "delivery_incomplete";
  gate_open: string[];
  evidence_only: boolean;
  proposal: FleetCriteriaProposal | null;
  rejected_reports: { run: number; errors: string[] }[];
  verification_skipped: boolean;
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
  create_issue?: boolean;
  milestone?: string;
  epic_issue?: number;
}

/** Body of POST /api/fleet/epics. */
export interface FleetEpicRequest {
  project_id: string;
  description: string;
  milestone?: string;
}

export interface FleetEpicReply {
  number: number;
  title: string;
  url: string;
  label: string;
  label_created: boolean;
  title_pattern_mismatch: boolean;
  title_pattern: string;
}

/** Whether GitHub can be written for a project right now. */
export interface FleetGitHubStatus {
  available: boolean;
  reason: "" | "no_remote" | "auth_required" | "check_failed";
}

export interface FleetMilestone {
  number: number;
  title: string;
}

export interface FleetProjectRequest {
  repo_path?: string;
  name?: string;
  trust_mode?: FleetTrustMode;
  verify_commands?: string[];
  max_concurrent?: number;
  budget?: FleetProjectBudget;
  /** Empty or absent leaves the target branch unchanged. */
  default_branch?: string;
  branch_prefix?: string;
}

export interface FleetIssue {
  number: number;
  title: string;
  labels: string[];
  milestone: string;
  url: string;
  /** The unfinished task already working on this issue, if any. */
  task_id: string;
  /** True when the issue is an epic (by label or title pattern). */
  is_epic: boolean;
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
  | "finish_evidence"
  | "merge"
  | "pass"
  | "discard";

/** The error body of every /api/fleet/* endpoint. */
export interface FleetErrorBody {
  error: string;
  code: string;
}
