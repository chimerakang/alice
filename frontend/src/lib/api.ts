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
} from "@/types/alice";

const BASE = "";

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

  // ========== Decisions ==========
  /** Get recent decisions from in-memory logger (no time range) */
  getRecentDecisions: (params?: { limit?: number }) => {
    const { limit = 50 } = params || {};
    const qs = buildQuery({ limit });
    return fetchJson<{ decisions?: DecisionLog[]; total?: number; timestamp: string }>(
      `/api/decisions/recent${qs}`
    );
  },

  /** Get decisions within a specific time range from database */
  getDecisionsByRange: (params: TimeRangeQuery) => {
    const { limit = 2000, offset = 0, startTime, endTime, source } = params;
    if (!startTime || !endTime) {
      throw new Error("startTime and endTime are required for getDecisionsByRange");
    }
    const qs = buildQuery({
      limit,
      offset,
      start_time: startTime,
      end_time: endTime,
      source,
    });
    return fetchJson<{ decisions?: DecisionLog[]; total?: number; timestamp: string }>(
      `/api/decisions/range${qs}`
    );
  },

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
  getPerformanceTrends: (hours = 24) =>
    fetchJson<unknown>(`/api/performance/trends?hours=${hours}`),
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
