/**
 * Re-export proto-generated TypeScript types.
 * Source: gen/ts/alice/v1/types.ts
 *
 * These types are the single source of truth, generated from .proto files.
 * When proto definitions change, regenerate with `make proto` and copy here.
 */

// ========== Enums (as const objects for erasableSyntaxOnly) ==========

export const Status = {
  STATUS_UNSPECIFIED: "STATUS_UNSPECIFIED",
  STATUS_PENDING: "STATUS_PENDING",
  STATUS_RUNNING: "STATUS_RUNNING",
  STATUS_SUCCESS: "STATUS_SUCCESS",
  STATUS_ERROR: "STATUS_ERROR",
  STATUS_CANCELLED: "STATUS_CANCELLED",
} as const;
export type Status = (typeof Status)[keyof typeof Status];

export const Severity = {
  SEVERITY_UNSPECIFIED: "SEVERITY_UNSPECIFIED",
  SEVERITY_LOW: "SEVERITY_LOW",
  SEVERITY_MEDIUM: "SEVERITY_MEDIUM",
  SEVERITY_HIGH: "SEVERITY_HIGH",
  SEVERITY_CRITICAL: "SEVERITY_CRITICAL",
} as const;
export type Severity = (typeof Severity)[keyof typeof Severity];

export const AgentType = {
  AGENT_TYPE_UNSPECIFIED: "AGENT_TYPE_UNSPECIFIED",
  AGENT_TYPE_COORDINATOR: "AGENT_TYPE_COORDINATOR",
  AGENT_TYPE_CODE_GENERATOR: "AGENT_TYPE_CODE_GENERATOR",
  AGENT_TYPE_FILE_MANAGER: "AGENT_TYPE_FILE_MANAGER",
  AGENT_TYPE_ANALYZER: "AGENT_TYPE_ANALYZER",
  AGENT_TYPE_TESTER: "AGENT_TYPE_TESTER",
  AGENT_TYPE_OPTIMIZER: "AGENT_TYPE_OPTIMIZER",
} as const;
export type AgentType = (typeof AgentType)[keyof typeof AgentType];

// ========== Common ==========

export interface Pagination {
  page: number;
  page_size: number;
  total_pages: number;
  total_count: number;
}

export interface TokenStats {
  total_input_tokens: number;
  total_output_tokens: number;
  total_tokens: number;
  total_cost_usd: number;
  total_requests: number;
}

export interface GitState {
  commit_hash: string;
  branch: string;
  is_dirty: boolean;
  remote_url: string;
  modified_files: string[];
}

export interface DiffFile {
  path: string;
  status: "added" | "deleted" | "modified";
  additions: number;
  deletions: number;
  chunks: string;
}

export interface GitDiffResponse {
  project_dir: string;
  commit: string;
  against: string;
  raw_diff: string;
  files: DiffFile[];
  file_count: number;
}

// ========== Agent ==========

export interface ExecutionSnapshot {
  state: string;
  since: string;
  terminal: boolean;
  reason: string;
}

export interface AgentInfo {
  chat_id: number;
  thread_id: number;
  project_dir: string;
  session_id: string;
  is_active: boolean;
  is_processing: boolean;
  status?: string;
  execution?: ExecutionSnapshot;
  execution_state?: string;
  created_at: string;
  last_activity: string;
  project_count: number;
  stats: TokenStats;
}

// ========== Tool ==========

export interface ToolExecution {
  id: string;
  timestamp: string;
  tool_name: string;
  input: Record<string, string>;
  status: Status;
  duration_ms: number;
  chat_id: number;
  thread_id: number;
  error: string;
  git_state?: GitState;
}

// ========== Decision ==========

export interface ExecutionOutcome {
  success: boolean;
  error_message: string;
  severity: Severity;
}

export interface DecisionLog {
  id: string;
  timestamp: string;
  session_id: string;
  project_path: string;
  user_prompt: string;
  agent_response: string;
  thinking_content?: string;
  tool_calls: ToolExecution[];
  task_type: string;
  outcome: ExecutionOutcome;
  duration_ms: number;
  // Backend returns nested tokens_used object (Go struct without json tags)
  // Normalized to flat fields by api.ts normalizeDecision()
  tokens_used?: {
    TotalInputTokens: number;
    TotalOutputTokens: number;
    TotalCostUSD: number;
    APICallCount: number;
    Model: string;
  };
  tokens_input: number;
  tokens_output: number;
  cost_usd: number;
  chat_id: number;
  thread_id: number;
  git_state?: GitState;
  source?: string; // "telegram" | "terminal" | "vscode" | "unknown"
  model?: string;
  routing_reason?: string;
  routing_latency_ms?: number;
  unified_task?: UnifiedTask;
}

// ========== Unified Task Graph ==========

export interface UnifiedToolEvent {
  id?: number;
  sub_task_id: string;
  tool_name: string;
  input_json: string;
  output_json: string;
  ts: string;
  status: string;
}

export interface UnifiedArtifact {
  id?: number;
  sub_task_id: string;
  path: string;
  hash: string;
}

export interface UnifiedSubTask {
  id: string;
  task_id: string;
  idx: number;
  description: string;
  model: string;
  status: string;
  result_text: string;
  input_tokens: number;
  output_tokens: number;
  cost_usd: number;
  started_at: string;
  ended_at?: string;
  routing_reason: string;
  routing_latency_ms: number;
  tool_events: UnifiedToolEvent[];
  artifacts: UnifiedArtifact[];
}

export interface UnifiedReviewSubTaskResult {
  id?: number;
  review_id: number;
  sub_task_id: string;
  score: number;
  feedback: string;
  issue_tags: string[];
}

export interface UnifiedReview {
  id?: number;
  task_id: string;
  reviewer_model: string;
  verdict: string;
  overall_score: number;
  feedback_text: string;
  issue_tags: string[];
  input_tokens: number;
  output_tokens: number;
  cost_usd: number;
  block_count?: number;
  auto_fixed_count?: number;
  source?: "initial" | "retry" | string;
  created_at: string;
  sub_task_results: UnifiedReviewSubTaskResult[];
}

export interface ReviewLiveEvent {
  task_id: string;
  reviewer_model?: string;
  verdict: string;
  overall_score: number;
  issue_tags: string[];
  advisory_retry?: boolean;
  failing_subtasks?: number;
  retry_note?: string;
  feedback_text?: string;
  source?: "initial" | "retry" | string;
  timestamp: string;
  sub_task_results?: UnifiedReviewSubTaskResult[];
}

export interface UnifiedTask {
  id: string;
  chat_id: number;
  thread_id: number;
  project_dir: string;
  goal: string;
  engine: string;
  backend: string;
  status: string;
  started_at: string;
  ended_at?: string;
  total_input_tokens: number;
  total_output_tokens: number;
  total_cost_usd: number;
  sub_tasks: UnifiedSubTask[];
  reviews: UnifiedReview[];
}

// ========== Runtime Events ==========

// HermesInterrupt mirrors hermes.HermesInterrupt for the active-tasks
// API. Only the fields the dashboard consumes are typed; payload is
// open-ended so per-Reason renderers can pick what they need.
export interface HermesInterrupt {
  id?: string;
  source_step?: string;
  resume_step?: string;
  reason?: string;
  payload?: Record<string, unknown>;
  created_at?: string;
  expires_at?: string;
}

// HermesActiveTask is the view returned by /api/hermes/active for the
// dashboard's Hermes Tasks panel. Includes interrupt context so the
// resolve buttons can be rendered correctly.
export interface HermesActiveTask {
  task_id: string;
  chat_id: number;
  thread_id: number;
  goal: string;
  project_dir: string;
  github_issue_number?: number;
  status: string;
  next_step?: string;
  current_idx: number;
  plan_length: number;
  used_tokens: number;
  max_total_tokens?: number;
  updated_at: string;
  interrupt?: HermesInterrupt;
}

export interface RuntimeEventRecord {
  timestamp: string;
  type: string;
  chat_id?: number;
  thread_id?: number;
  task_id?: string;
  issue?: number;
  payload?: Record<string, unknown>;
}

// ========== Quality Analytics ==========

export interface QualityBucket {
  label: string;
  count: number;
  percentage: number;
  avg_score: number;
  partial_rate: number;
  fail_rate: number;
}

export interface QualityTrendPoint {
  period: string;
  task_count: number;
  review_count: number;
  avg_sub_tasks: number;
  pass_rate: number;
  partial_rate: number;
  fail_rate: number;
  avg_score: number;
  avg_sub_score: number;
}

export interface QualityGranularityScore {
  sub_task_count: number;
  task_count: number;
  avg_score: number;
}

export interface QualityToolHintStat {
  tool_hints: string;
  count: number;
  pass_rate: number;
  avg_score: number;
}

export interface QualityDecompositionStats {
  window_start: string;
  window_end: string;
  task_count: number;
  sub_task_count: number;
  avg_sub_tasks: number;
  stddev_sub_tasks: number;
  best_granularity: string;
  granularity_buckets: QualityBucket[];
  granularity_scores: QualityGranularityScore[];
  weekly_trend: QualityTrendPoint[];
  description_buckets: QualityBucket[];
  tool_hint_stats: QualityToolHintStat[];
}

export interface QualityIssueTagStat {
  tag: string;
  count: number;
  previous_count: number;
  delta: number;
  trend: "up" | "down" | "flat" | string;
}

export interface QualityLowScoringSubTask {
  task_id: string;
  sub_task_id: string;
  description: string;
  score: number;
  issue_tags: string[];
  feedback: string;
  created_at: string;
}

export interface QualityScoreStats {
  window_start: string;
  window_end: string;
  review_count: number;
  reviewed_sub_task_count: number;
  pass_rate: number;
  partial_rate: number;
  fail_rate: number;
  avg_overall_score: number;
  avg_sub_task_score: number;
  verdict_distribution: Record<string, number>;
  trend: QualityTrendPoint[];
  top_issue_tags: QualityIssueTagStat[];
  low_scoring_sub_tasks: QualityLowScoringSubTask[];
}

export interface QualityInsight {
  name: string;
  severity: "warning" | "info" | "success" | string;
  message: string;
  suggestion: string;
  value: number;
  threshold: number;
}

// ========== Performance ==========

export interface PerformanceMetric {
  timestamp: string;
  api_latency_ms: number;
  api_success: boolean;
  tool_execution_time: number;
  tool_execution_type: string;
  tokens_used: number;
  input_tokens?: number;
  cache_read_tokens?: number;
  cache_write_tokens?: number;
  output_tokens?: number;
  estimated_cost: number;
  memory_usage: number;
  error_type: string;
  chat_id: number;
}

export interface PerformanceAnalytics {
  total_operations: number;
  avg_response_time: number;
  error_rate: number;
  throughput: number;
  timestamp: string;
}

export interface CacheBreakdownRow {
  group: string;
  calls: number;
  tokens: number;
  input_tokens: number;
  cache_read_tokens: number;
  cache_write_tokens: number;
  output_tokens: number;
  cost: number;
  cache_read_input_percent: number;
  cache_read_total_percent: number;
}

export interface CacheBreakdown {
  by_provider?: CacheBreakdownRow[];
  by_model?: CacheBreakdownRow[];
  by_project?: CacheBreakdownRow[];
}

export interface PerformanceTrendsResponse {
  enabled: boolean;
  timestamp: string;
  trends?: {
    cache_breakdown?: CacheBreakdown;
    cache_hit_rate?: number;
    cache_read_tokens?: number;
    cache_write_tokens?: number;
    total_tokens?: number;
    total_cost?: number;
    data_points?: number;
    [key: string]: unknown;
  };
}

// ========== Memory ==========

export interface MemoryPreviewSection {
  source: string;
  scope: string;
  priority: number;
  size: number;
  preview: string;
}

export interface MemoryPreviewResponse {
  sections: MemoryPreviewSection[];
  section_count: number;
  rendered_size: number;
  rendered_preview: string;
  timestamp: string;
}

export interface MemoryPreviewQuery {
  chatId: number;
  threadId?: number;
  projectDir?: string;
  issue?: number;
  message?: string;
  mode?: string;
  budget?: number;
}

// ========== Security ==========

export interface SecurityEvent {
  event_id?: string;  // Optional because API might return `id` instead
  id?: string;        // Alternative name from backend
  event_type: string;
  severity: Severity;
  description: string;
  user_id?: string;
  ip?: string;
  mitigated?: boolean;
  timestamp: string;
  details?: Record<string, any>;
}

export interface SecurityStats {
  total_events: number;
  blocked_attempts: number;
  pii_detections: number;
  threat_level: string;
  timestamp: string;
}

// ========== Checkpoint ==========

export interface Checkpoint {
  id: string;
  timestamp: string;
  project_dir: string;
  git_commit_hash: string;
  git_branch: string;
  description: string;
  trigger_type: string;
  session_id: string;
  chat_id: number;
  size: number;
  is_active: boolean;
  decision_log_id?: string;
  dangerous_op?: string;
  pre_condition?: string;
  created_by?: string;
}

// ========== WebSocket ==========

export interface WebSocketEvent {
  type: WebSocketEventType;
  timestamp: string;
  data: unknown;
}

export type WebSocketEventType =
  | "tool_execution_start"
  | "tool_execution"
  | "task_updated"
  | "sub_task_updated"
  | "tool_event"
  | "review_result"
  | "review_complete"
  | "decision_complete"
  | "performance_metric"
  | "security_alert"
  | "agent_status"
  | "hook_session_active"
  | "codex_session_update";

// ========== API Responses ==========

export interface StatsResponse {
  active_sessions: number;
  total_sessions: number;
  tools_executed: number;
  total_projects: number;
  success_rate: number;
  uptime_seconds: number;
  timestamp: string;
  recent_agents?: AgentInfo[];
  total_tokens_used: number;
  total_cost_usd: number;
}

export interface HealthResponse {
  status: string;
  telegram: string;
  timestamp: string;
}

export interface MultiAgentStatus {
  coordinator_status: string;
  active_tasks: number;
  specialized_agents: string[];
  timestamp: string;
}
