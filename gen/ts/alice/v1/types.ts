/**
 * TypeScript type definitions for Alice API
 * Generated from Proto definitions
 *
 * This file provides type-safe access to the Alice API from TypeScript/JavaScript frontends.
 */

// ========== Common Types ==========

export enum Status {
  STATUS_UNSPECIFIED = "STATUS_UNSPECIFIED",
  STATUS_PENDING = "STATUS_PENDING",
  STATUS_RUNNING = "STATUS_RUNNING",
  STATUS_SUCCESS = "STATUS_SUCCESS",
  STATUS_ERROR = "STATUS_ERROR",
  STATUS_CANCELLED = "STATUS_CANCELLED"
}

export enum Severity {
  SEVERITY_UNSPECIFIED = "SEVERITY_UNSPECIFIED",
  SEVERITY_LOW = "SEVERITY_LOW",
  SEVERITY_MEDIUM = "SEVERITY_MEDIUM",
  SEVERITY_HIGH = "SEVERITY_HIGH",
  SEVERITY_CRITICAL = "SEVERITY_CRITICAL"
}

export enum PrivacyLevel {
  PRIVACY_LEVEL_UNSPECIFIED = "PRIVACY_LEVEL_UNSPECIFIED",
  PRIVACY_LEVEL_PUBLIC = "PRIVACY_LEVEL_PUBLIC",
  PRIVACY_LEVEL_INTERNAL = "PRIVACY_LEVEL_INTERNAL",
  PRIVACY_LEVEL_CONFIDENTIAL = "PRIVACY_LEVEL_CONFIDENTIAL",
  PRIVACY_LEVEL_RESTRICTED = "PRIVACY_LEVEL_RESTRICTED"
}

export enum AgentType {
  AGENT_TYPE_UNSPECIFIED = "AGENT_TYPE_UNSPECIFIED",
  AGENT_TYPE_COORDINATOR = "AGENT_TYPE_COORDINATOR",
  AGENT_TYPE_CODE_GENERATOR = "AGENT_TYPE_CODE_GENERATOR",
  AGENT_TYPE_FILE_MANAGER = "AGENT_TYPE_FILE_MANAGER",
  AGENT_TYPE_ANALYZER = "AGENT_TYPE_ANALYZER",
  AGENT_TYPE_TESTER = "AGENT_TYPE_TESTER",
  AGENT_TYPE_OPTIMIZER = "AGENT_TYPE_OPTIMIZER"
}

export interface Pagination {
  page: number;
  page_size: number;
  total_pages: number;
  total_count: number;
}

export interface Error {
  code: string;
  message: string;
  details: string[];
}

export interface TokenStats {
  total_input_tokens: number;
  total_output_tokens: number;
  total_cost_usd: number;
  api_call_count: number;
}

export interface GitState {
  branch: string;
  commit_hash: string;
  is_dirty: boolean;
  modified_files: string[];
  remote_url: string;
}

// ========== Agent Types ==========

export interface AgentInfo {
  id: string;
  chat_id: number;
  thread_id: number;
  project_dir: string;
  session_id: string;
  status: Status;
  created_at: string; // ISO 8601 timestamp
  last_active: string; // ISO 8601 timestamp
  token_stats?: TokenStats;
  model: string;
  git_state?: GitState;
}

export interface AgentStats {
  total_sessions: number;
  active_sessions: number;
  success_rate: number;
  avg_session_duration_ms: number;
  total_tokens_used: number;
  total_cost_usd: number;
  timestamp: string; // ISO 8601 timestamp
}

export interface ListAgentsResponse {
  agents: AgentInfo[];
  pagination?: Pagination;
  error?: Error;
}

export interface GetAgentResponse {
  agent?: AgentInfo;
  error?: Error;
}

export interface GetAgentStatsResponse {
  stats?: AgentStats;
  error?: Error;
}

// ========== Tool Types ==========

export interface ToolExecution {
  id: string;
  timestamp: string; // ISO 8601 timestamp
  tool_name: string;
  input: Record<string, string>;
  status: Status;
  duration_ms: number;
  chat_id: number;
  thread_id: number;
  error?: string;
}

export interface ToolParameter {
  name: string;
  type: string;
  required: boolean;
  description: string;
  default_value?: string;
  enum_values: string[];
}

export interface ToolDefinition {
  name: string;
  description: string;
  parameters: ToolParameter[];
  is_dangerous: boolean;
  category: string;
}

export interface ListToolExecutionsResponse {
  executions: ToolExecution[];
  pagination?: Pagination;
  error?: Error;
}

export interface GetToolExecutionResponse {
  execution?: ToolExecution;
  error?: Error;
}

export interface ListToolsResponse {
  tools: ToolDefinition[];
  pagination?: Pagination;
  error?: Error;
}

// ========== Decision Types ==========

export interface ExecutionOutcome {
  success: boolean;
  task_type: string;
  files_changed: string[];
  summary: string;
  error_message?: string;
  severity: Severity;
}

export interface DecisionLog {
  id: string;
  timestamp: string; // ISO 8601 timestamp
  user_prompt: string;
  agent_response: string;
  tool_calls: ToolExecution[];
  chat_id: number;
  thread_id: number;
  project_path: string;
  duration_ms: number;
  token_stats?: TokenStats;
  outcome?: ExecutionOutcome;
  context: Record<string, string>;
  privacy_level: PrivacyLevel;
}

export interface ListDecisionsResponse {
  decisions: DecisionLog[];
  pagination?: Pagination;
  error?: Error;
}

export interface GetDecisionResponse {
  decision?: DecisionLog;
  error?: Error;
}

export interface SearchDecisionsResponse {
  decisions: DecisionLog[];
  pagination?: Pagination;
  search_query: string;
  error?: Error;
}

// ========== Performance Types ==========

export interface PerformanceMetric {
  id: string;
  timestamp: string; // ISO 8601 timestamp
  metric_type: string;
  value: number;
  unit: string;
  labels: Record<string, string>;
  project_path: string;
  session_id: string;
}

export interface PerformanceAnalytics {
  total_operations: number;
  avg_response_time_ms: number;
  p95_response_time_ms: number;
  error_rate: number;
  throughput_ops_per_sec: number;
  resource_usage: Record<string, number>;
  timestamp: string; // ISO 8601 timestamp
}

export interface PerformanceRecommendation {
  id: string;
  type: string;
  title: string;
  description: string;
  priority: Severity;
  estimated_impact: string;
  implementation_effort: string;
  metrics_affected: string[];
}

// ========== Security Types ==========

export interface SecurityEvent {
  id: string;
  timestamp: string; // ISO 8601 timestamp
  event_type: string;
  severity: Severity;
  description: string;
  source_ip?: string;
  user_agent?: string;
  project_path: string;
  affected_files: string[];
  mitigation_taken: string;
  metadata: Record<string, string>;
}

export interface PIIDetection {
  id: string;
  timestamp: string; // ISO 8601 timestamp
  content_type: string;
  detected_patterns: string[];
  confidence_score: number;
  file_path: string;
  line_number: number;
  context: string;
  severity: Severity;
  auto_redacted: boolean;
}

// ========== MultiAgent Types ==========

export interface SubTask {
  id: string;
  parent_task_id: string;
  agent_type: AgentType;
  description: string;
  status: Status;
  created_at: string; // ISO 8601 timestamp
  completed_at?: string; // ISO 8601 timestamp
  result?: string;
  error?: string;
}

export interface CoordinatedTask {
  id: string;
  description: string;
  requester_chat_id: number;
  status: Status;
  created_at: string; // ISO 8601 timestamp
  completed_at?: string; // ISO 8601 timestamp
  subtasks: SubTask[];
  coordinator_agent_id: string;
  priority: number;
}

// ========== WebSocket Types ==========

export interface WebSocketEvent {
  event_id: string;
  timestamp: string; // ISO 8601 timestamp
  event_type: string;
  chat_id: number;
  thread_id: number;
  session_id: string;
  payload: Record<string, any>;
}

// ========== Dashboard Types ==========

export interface DashboardSummary {
  total_agents: number;
  active_agents: number;
  total_sessions: number;
  total_tool_executions: number;
  total_decisions: number;
  avg_success_rate: number;
  total_tokens_used: number;
  total_cost_usd: number;
  timestamp: string; // ISO 8601 timestamp
}

// ========== API Response Helpers ==========

/**
 * Generic API response wrapper
 */
export interface ApiResponse<T> {
  data?: T;
  error?: Error;
  timestamp: string;
}

/**
 * List response wrapper with pagination
 */
export interface ListResponse<T> {
  items: T[];
  pagination?: Pagination;
  total: number;
  error?: Error;
  timestamp: string;
}

// ========== Client Configuration ==========

export interface AliceClientConfig {
  baseUrl: string;
  apiKey?: string;
  timeout?: number;
  retries?: number;
}

/**
 * Alice API Client class for TypeScript applications
 */
export class AliceApiClient {
  private baseUrl: string;
  private config: AliceClientConfig;

  constructor(config: AliceClientConfig) {
    this.baseUrl = config.baseUrl.replace(/\/$/, '');
    this.config = config;
  }

  /**
   * Generic HTTP request method
   */
  private async request<T>(endpoint: string, options: RequestInit = {}): Promise<T> {
    const url = `${this.baseUrl}${endpoint}`;

    const defaultHeaders: Record<string, string> = {
      'Content-Type': 'application/json',
    };

    if (this.config.apiKey) {
      defaultHeaders['Authorization'] = `Bearer ${this.config.apiKey}`;
    }

    const response = await fetch(url, {
      ...options,
      headers: {
        ...defaultHeaders,
        ...(options.headers || {}),
      },
    });

    if (!response.ok) {
      throw new Error(`HTTP ${response.status}: ${response.statusText}`);
    }

    return response.json();
  }

  // ========== Agent API ==========

  async listAgents(): Promise<ListAgentsResponse> {
    return this.request<ListAgentsResponse>('/api/agents');
  }

  async getAgent(chatId: number, threadId?: number): Promise<GetAgentResponse> {
    const params = new URLSearchParams({ chat_id: chatId.toString() });
    if (threadId !== undefined) {
      params.set('thread_id', threadId.toString());
    }
    return this.request<GetAgentResponse>(`/api/agents?${params}`);
  }

  // ========== Tool API ==========

  async getRecentTools(limit = 20): Promise<ListToolExecutionsResponse> {
    return this.request<ListToolExecutionsResponse>(`/api/tools/recent?limit=${limit}`);
  }

  async getToolExecutions(): Promise<ApiResponse<any>> {
    return this.request<ApiResponse<any>>('/api/tools/executions');
  }

  // ========== Decision API ==========

  async getDecisions(): Promise<ApiResponse<any>> {
    return this.request<ApiResponse<any>>('/api/decisions');
  }

  async getRecentDecisions(limit = 10): Promise<ListDecisionsResponse> {
    return this.request<ListDecisionsResponse>(`/api/decisions/recent?limit=${limit}`);
  }

  // ========== MultiAgent API ==========

  async getMultiAgentStatus(): Promise<ApiResponse<any>> {
    return this.request<ApiResponse<any>>('/api/multiagent/status');
  }

  async getMultiAgentStats(): Promise<ApiResponse<any>> {
    return this.request<ApiResponse<any>>('/api/multiagent/stats');
  }

  async getMultiAgentAgents(): Promise<ApiResponse<any>> {
    return this.request<ApiResponse<any>>('/api/multiagent/agents');
  }

  // ========== Performance API ==========

  async getPerformanceAnalytics(): Promise<ApiResponse<any>> {
    return this.request<ApiResponse<any>>('/api/performance/analytics');
  }

  async getPerformanceMetrics(): Promise<ApiResponse<any>> {
    return this.request<ApiResponse<any>>('/api/performance/metrics');
  }

  // ========== Security API ==========

  async getSecurityEvents(): Promise<ApiResponse<any>> {
    return this.request<ApiResponse<any>>('/api/security/events');
  }

  async getSecurityStats(): Promise<ApiResponse<any>> {
    return this.request<ApiResponse<any>>('/api/security/stats');
  }
}

// ========== Utility Functions ==========

/**
 * Create a new Alice API client instance
 */
export function createAliceClient(config: AliceClientConfig): AliceApiClient {
  return new AliceApiClient(config);
}

/**
 * Type guard to check if a response contains an error
 */
export function isErrorResponse<T>(response: ApiResponse<T>): response is ApiResponse<T> & { error: Error } {
  return response.error !== undefined;
}

/**
 * Convert timestamp string to Date object
 */
export function parseTimestamp(timestamp: string): Date {
  return new Date(timestamp);
}

/**
 * Format status enum for display
 */
export function formatStatus(status: Status): string {
  return status.replace('STATUS_', '').toLowerCase().replace('_', ' ');
}

/**
 * Format severity enum for display
 */
export function formatSeverity(severity: Severity): string {
  return severity.replace('SEVERITY_', '').toLowerCase();
}