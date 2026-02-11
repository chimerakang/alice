import type {
  AgentInfo,
  ToolExecution,
  DecisionLog,
  PerformanceAnalytics,
  PerformanceMetric,
  SecurityEvent,
  SecurityStats,
  StatsResponse,
  HealthResponse,
  MultiAgentStatus,
  GitState,
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
  getRecentTools: (limit = 20) =>
    fetchJson<{ executions?: ToolExecution[]; pagination: unknown }>(
      `/api/tools/recent?limit=${limit}`
    ),
  getToolExecutions: () =>
    fetchJson<{
      total_executions: number;
      tool_counts: Record<string, number>;
      success_rate: number;
    }>("/api/tools/executions"),

  // ========== Decisions ==========
  getRecentDecisions: (limit = 10) =>
    fetchJson<{ decisions?: DecisionLog[]; pagination: unknown }>(
      `/api/decisions/recent?limit=${limit}`
    ),

  // ========== Multi-Agent ==========
  getMultiAgentStatus: () =>
    fetchJson<MultiAgentStatus>("/api/multiagent/status"),

  // ========== Performance ==========
  getPerformanceAnalytics: () =>
    fetchJson<PerformanceAnalytics>("/api/performance/analytics"),
  getPerformanceMetrics: (limit = 100) =>
    fetchJson<{ metrics?: PerformanceMetric[] }>(
      `/api/performance/metrics?limit=${limit}`
    ),
  getPerformanceTrends: (hours = 24) =>
    fetchJson<unknown>(`/api/performance/trends?hours=${hours}`),
  getPerformanceRecommendations: () =>
    fetchJson<{ recommendations?: unknown[] }>(
      "/api/performance/recommendations"
    ),

  // ========== Security ==========
  getSecurityEvents: (limit = 50) =>
    fetchJson<{ events?: SecurityEvent[] }>(
      `/api/security/events?limit=${limit}`
    ),
  getSecurityStats: () => fetchJson<SecurityStats>("/api/security/stats"),

  // ========== Git ==========
  getGitStatus: (projectDir?: string) => {
    const params = projectDir ? `?project_dir=${encodeURIComponent(projectDir)}` : "";
    return fetchJson<GitState>(`/api/git/status${params}`);
  },
  getGitEvents: (limit = 20) =>
    fetchJson<{ events?: unknown[] }>(`/api/git/events?limit=${limit}`),

  // ========== Checkpoints ==========
  getCheckpoints: (projectDir: string) =>
    fetchJson<{ checkpoints?: Checkpoint[] }>(
      `/api/checkpoints?project_dir=${encodeURIComponent(projectDir)}`
    ),
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
