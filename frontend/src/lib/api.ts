import type {
  AgentInfo,
  ToolExecution,
  DecisionLog,
  Pagination,
  PerformanceAnalytics,
  PerformanceMetric,
  SecurityEvent,
  SecurityStats,
  StatsResponse,
  HealthResponse,
  MultiAgentStatus,
  GitState,
  GitDiffResponse,
  Checkpoint,
  UnifiedTask,
  QualityDecompositionStats,
  QualityScoreStats,
  QualityInsight,
  PerformanceTrendsResponse,
  MemoryPreviewQuery,
  MemoryPreviewResponse,
  RuntimeEventRecord,
  HermesActiveTask,
} from "@/types/alice";

const BASE = "";

/** Normalize a DecisionLog: generate id if missing, map nested tokens_used to flat fields */
function normalizeDecision(d: DecisionLog): DecisionLog {
  // BUG #1 fix: API doesn't return 'id' field, causing dedup to collapse all decisions to 1
  if (!d.id) {
    d.id = `${d.session_id || ''}_${d.chat_id || 0}_${d.thread_id || 0}_${d.timestamp || ''}`;
  }
  // Map nested tokens_used (Go struct without json tags) to flat fields
  if (d.tokens_used) {
    d.tokens_input = d.tokens_used.TotalInputTokens || 0;
    d.tokens_output = d.tokens_used.TotalOutputTokens || 0;
    d.cost_usd = d.tokens_used.TotalCostUSD || 0;
  }
  return d;
}

function parseJSONRecord(raw: string): Record<string, string> {
  if (!raw) return {};
  try {
    const parsed = JSON.parse(raw);
    return parsed && typeof parsed === "object" && !Array.isArray(parsed) ? parsed : {};
  } catch {
    return { raw };
  }
}

function isUnifiedSuccess(status: string): boolean {
  return ["done", "success", "completed", "pass"].includes(status);
}

function mapUnifiedStatus(status: string): ToolExecution["status"] {
  if (["done", "success", "executed", "completed"].includes(status)) return "STATUS_SUCCESS";
  if (["failed", "error"].includes(status)) return "STATUS_ERROR";
  if (["cancelled", "interrupted"].includes(status)) return "STATUS_CANCELLED";
  if (["running", "executing", "in_progress"].includes(status)) return "STATUS_RUNNING";
  return "STATUS_PENDING";
}

function durationMs(startedAt?: string, endedAt?: string): number {
  if (!startedAt || !endedAt) return 0;
  const start = new Date(startedAt).getTime();
  const end = new Date(endedAt).getTime();
  return Number.isFinite(start) && Number.isFinite(end) && end >= start ? end - start : 0;
}

export function unifiedTaskToDecision(task: UnifiedTask): DecisionLog {
  const primarySubTask = task.sub_tasks?.[0];
  const toolCalls = (task.sub_tasks || []).flatMap((subTask) =>
    (subTask.tool_events || []).map((event) => ({
      id: String(event.id ?? `${event.sub_task_id}:${event.tool_name}:${event.ts}`),
      timestamp: event.ts,
      tool_name: event.tool_name,
      input: parseJSONRecord(event.input_json),
      status: mapUnifiedStatus(event.status),
      duration_ms: 0,
      chat_id: task.chat_id,
      thread_id: task.thread_id,
      error: event.status === "error" || event.status === "failed" ? event.output_json : "",
    }))
  );

  return normalizeDecision({
    id: task.id,
    timestamp: task.started_at,
    session_id: task.id,
    project_path: task.project_dir,
    user_prompt: task.goal,
    agent_response: primarySubTask?.result_text || "",
    tool_calls: toolCalls,
    task_type: task.engine || "task",
    outcome: {
      success: isUnifiedSuccess(task.status),
      error_message: isUnifiedSuccess(task.status) ? "" : task.status,
      severity: "SEVERITY_UNSPECIFIED",
    },
    duration_ms: durationMs(task.started_at, task.ended_at),
    tokens_input: task.total_input_tokens || 0,
    tokens_output: task.total_output_tokens || 0,
    cost_usd: task.total_cost_usd || 0,
    chat_id: task.chat_id,
    thread_id: task.thread_id,
    source: "unknown",
    model: primarySubTask?.model || task.backend,
    routing_reason: primarySubTask?.routing_reason,
    routing_latency_ms: primarySubTask?.routing_latency_ms,
    unified_task: task,
  });
}

function unifiedTasksToDecisions(tasks?: UnifiedTask[]): DecisionLog[] {
  return (tasks || []).map(unifiedTaskToDecision);
}

async function fetchJson<T>(url: string): Promise<T> {
  const res = await fetch(`${BASE}${url}`);
  if (!res.ok) throw new Error(`API error ${res.status}: ${url}`);
  return res.json() as Promise<T>;
}

async function postJson<T>(url: string, body: unknown): Promise<T> {
  const res = await fetch(`${BASE}${url}`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (!res.ok) throw new Error(`API error ${res.status}: ${url}`);
  return res.json() as Promise<T>;
}

/** Build query string from optional params, skipping undefined values */
function buildQuery(params: Record<string, string | number | undefined>): string {
  const qs = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) {
    if (v !== undefined && v !== "") qs.set(k, String(v));
  }
  const s = qs.toString();
  return s ? `?${s}` : "";
}

/** Common time-range + pagination query params */
export interface TimeRangeQuery {
  limit?: number;
  offset?: number;
  startTime?: string; // RFC3339
  endTime?: string;   // RFC3339
  source?: string;    // "telegram" | "terminal" | "vscode" | "all"
}

// ========== Health & Stats ==========

export const api = {
  getHealth: () => fetchJson<HealthResponse>("/api/health"),
  getStats: () => fetchJson<StatsResponse>("/api/stats"),

  // ========== Agents ==========
  getAgents: () =>
    fetchJson<{ agents?: AgentInfo[]; pagination: unknown }>("/api/agents"),
  getAgent: (chatId: number, threadId = 0) =>
    fetchJson<AgentInfo>(`/api/agents/?chat_id=${chatId}&thread_id=${threadId}`),

  // ========== Tools ==========
  getRecentTools: (params: TimeRangeQuery = {}) => {
    const { limit = 50, offset, startTime, endTime } = params;
    const qs = buildQuery({ limit, offset, start_time: startTime, end_time: endTime });
    return fetchJson<{ executions?: ToolExecution[]; pagination: Pagination }>(
      `/api/tools/recent${qs}`
    );
  },
  getToolExecutions: () =>
    fetchJson<{
      total_executions: number;
      tool_counts: Record<string, number>;
      success_rate: number;
    }>("/api/tools/executions"),

  /** Get task-centric execution graphs from the unified #114 schema */
  getUnifiedTasks: (params: TimeRangeQuery & { projectDir?: string; status?: string } = {}) => {
    const { limit = 100, offset = 0, startTime, endTime, projectDir, status } = params;
    const qs = buildQuery({
      limit,
      offset,
      start_time: startTime,
      end_time: endTime,
      project_dir: projectDir,
      status,
    });
    return fetchJson<{ tasks?: UnifiedTask[]; total?: number; timestamp: string }>(
      `/api/tasks${qs}`
    );
  },

  /** Compatibility view for existing dashboard widgets while storage migrates to tasks. */
  getTaskDecisions: async (params: TimeRangeQuery & { projectDir?: string; status?: string } = {}) => {
    const res = await api.getUnifiedTasks(params);
    return {
      decisions: unifiedTasksToDecisions(res.tasks),
      total: res.total,
      timestamp: res.timestamp,
    };
  },

  // ========== Runtime Events ==========
  getRuntimeEvents: (params: { limit?: number; offset?: number; type?: string } = {}) => {
    const { limit = 50, offset = 0, type } = params;
    const qs = buildQuery({ limit, offset, type });
    return fetchJson<{
      events?: RuntimeEventRecord[];
      total?: number;
      limit?: number;
      offset?: number;
      type?: string;
      timestamp?: string;
    }>(`/api/runtime/events${qs}`);
  },

  // ========== Hermes Tasks (#171 Class C UI) ==========
  getHermesActiveTasks: () =>
    fetchJson<{ tasks?: HermesActiveTask[]; total?: number }>("/api/hermes/active"),
  resolveHermesTask: (taskId: string, decision: "retry" | "skip" | "abort") =>
    postJson<{ ok: boolean; task_id: string; decision: string; relaunched: boolean }>(
      "/api/hermes/resolve",
      { task_id: taskId, decision }
    ),

  // ========== Quality Analytics ==========
  getQualityDecomposition: (window = "30d") =>
    fetchJson<QualityDecompositionStats>(`/api/quality/decomposition?window=${encodeURIComponent(window)}`),
  getQualityScores: (window = "30d") =>
    fetchJson<QualityScoreStats>(`/api/quality/scores?window=${encodeURIComponent(window)}`),
  getQualityInsights: (window = "30d") =>
    fetchJson<{ insights?: QualityInsight[]; timestamp?: string }>(
      `/api/quality/insights?window=${encodeURIComponent(window)}`
    ),

  // ========== Multi-Agent ==========
  getMultiAgentStatus: () =>
    fetchJson<MultiAgentStatus>("/api/multiagent/status"),

  // ========== Performance ==========
  getPerformanceAnalytics: (params: TimeRangeQuery = {}) => {
    const { limit, offset, startTime, endTime } = params;
    const qs = buildQuery({ limit, offset, start_time: startTime, end_time: endTime });
    return fetchJson<PerformanceAnalytics>(`/api/performance/analytics${qs}`);
  },
  getPerformanceMetrics: (params: TimeRangeQuery = {}) => {
    const { limit = 100, offset, startTime, endTime } = params;
    const qs = buildQuery({ limit, offset, start_time: startTime, end_time: endTime });
    return fetchJson<{ metrics?: PerformanceMetric[]; total?: number; pagination?: Pagination }>(
      `/api/performance/metrics${qs}`
    );
  },
  getPerformanceTrends: (params: TimeRangeQuery & { hours?: number } = {}) => {
    const { hours = 24, startTime, endTime } = params;
    const qs = buildQuery({ hours, start_time: startTime, end_time: endTime });
    return fetchJson<PerformanceTrendsResponse>(`/api/performance/trends${qs}`);
  },
  getPerformanceRecommendations: () =>
    fetchJson<{ recommendations?: unknown[] }>(
      "/api/performance/recommendations"
    ),
  getToolDistribution: (params: TimeRangeQuery = {}) => {
    const { limit, offset, startTime, endTime } = params;
    const qs = buildQuery({ limit, offset, start_time: startTime, end_time: endTime });
    return fetchJson<{ tool_distribution?: { tool_type: string; avg_execution_time: number; count: number }[]; total?: number }>(
      `/api/performance/tool-distribution${qs}`
    );
  },

  // ========== Memory ==========
  getMemoryPreview: (params: MemoryPreviewQuery) => {
    const qs = buildQuery({
      chat_id: params.chatId,
      thread_id: params.threadId,
      project_dir: params.projectDir,
      issue: params.issue,
      message: params.message,
      mode: params.mode,
      budget: params.budget,
    });
    return fetchJson<MemoryPreviewResponse>(`/api/memory/preview${qs}`);
  },

  // ========== Security ==========
  getSecurityEvents: (params: TimeRangeQuery & { severity?: string } = {}) => {
    const { limit = 50, offset, startTime, endTime, severity } = params;
    const qs = buildQuery({ limit, offset, start_time: startTime, end_time: endTime, severity });
    return fetchJson<{ events?: SecurityEvent[]; total?: number; pagination?: Pagination }>(
      `/api/security/events${qs}`
    );
  },
  getSecurityStats: (params: TimeRangeQuery = {}) => {
    const { limit, offset, startTime, endTime } = params;
    const qs = buildQuery({
      limit,
      offset,
      start_time: startTime,
      end_time: endTime,
    });
    return fetchJson<SecurityStats>(`/api/security/stats${qs}`);
  },

  // ========== Git ==========
  getGitStatus: (projectDir?: string) => {
    const params = projectDir ? `?project_dir=${encodeURIComponent(projectDir)}` : "";
    return fetchJson<GitState>(`/api/git/status${params}`);
  },
  getGitEvents: (limit = 20) =>
    fetchJson<{ events?: unknown[] }>(`/api/git/events?limit=${limit}`),
  getGitDiff: (opts: { projectDir?: string; commit?: string; against?: string } = {}) => {
    const params = new URLSearchParams();
    if (opts.projectDir) params.set("project_dir", opts.projectDir);
    if (opts.commit) params.set("commit", opts.commit);
    if (opts.against) params.set("against", opts.against);
    const qs = params.toString();
    return fetchJson<GitDiffResponse>(`/api/git/diff${qs ? `?${qs}` : ""}`);
  },

  // ========== Checkpoints ==========
  getCheckpoints: (projectDir: string, params: TimeRangeQuery = {}) => {
    const { limit, offset, startTime, endTime } = params;
    const qs = buildQuery({
      project_dir: projectDir,
      limit,
      offset,
      start_time: startTime,
      end_time: endTime
    });
    return fetchJson<{ checkpoints?: Checkpoint[] }>(`/api/checkpoints${qs}`);
  },
  createCheckpoint: (projectDir: string, description: string) =>
    postJson<Checkpoint>("/api/checkpoints/create", {
      project_dir: projectDir,
      description,
    }),
  restoreCheckpoint: (checkpointId: string) =>
    postJson<unknown>("/api/checkpoints/restore", {
      checkpoint_id: checkpointId,
    }),

  // ========== Storage ==========
  getStorageHealth: () => fetchJson<unknown>("/api/storage/health"),
  getStorageStats: () => fetchJson<unknown>("/api/storage/stats"),

  // ========== WebSocket Stats ==========
  getWebSocketStats: () => fetchJson<unknown>("/api/websocket/stats"),
} as const;
