import { create } from "zustand";
import { devtools } from "zustand/middleware";
import { unifiedTaskToDecision } from "@/lib/api";
import type {
  AgentInfo,
  ToolExecution,
  DecisionLog,
  StatsResponse,
  GitState,
  SecurityEvent,
  WebSocketEvent,
  UnifiedTask,
  ReviewLiveEvent,
  UnifiedReviewSubTaskResult,
} from "@/types/alice";

interface AppState {
  // Connection
  wsConnected: boolean;
  setWsConnected: (v: boolean) => void;

  // Stats
  stats: StatsResponse | null;
  setStats: (s: StatsResponse) => void;

  // Agents
  agents: AgentInfo[];
  setAgents: (a: AgentInfo[]) => void;
  updateAgentStatus: (chatId: number, status: string) => void;

  // Tool Executions (ring buffer, max 100)
  toolExecutions: ToolExecution[];
  addToolExecution: (t: ToolExecution) => void;
  setToolExecutions: (t: ToolExecution[]) => void;

  // Decisions (ring buffer, max 50)
  decisions: DecisionLog[];
  addDecision: (d: DecisionLog) => void;
  setDecisions: (d: DecisionLog[]) => void;

  // Git
  gitState: GitState | null;
  setGitState: (g: GitState) => void;

  // Security events
  securityEvents: SecurityEvent[];
  addSecurityEvent: (e: SecurityEvent) => void;

  // Review events
  reviewEvents: ReviewLiveEvent[];
  addReviewEvent: (r: ReviewLiveEvent) => void;

  // WebSocket event buffer (for timeline)
  wsEvents: WebSocketEvent[];
  addWsEvent: (e: WebSocketEvent) => void;

  // Handle incoming WebSocket event
  handleWsEvent: (event: WebSocketEvent) => void;
}

export const useAppStore = create<AppState>()(
  devtools(
    (set, get) => ({
  // Connection
  wsConnected: false,
  setWsConnected: (v) => set({ wsConnected: v }),

  // Stats
  stats: null,
  setStats: (s) => set({ stats: s }),

  // Agents
  agents: [],
  setAgents: (a) => set({ agents: a }),
  updateAgentStatus: (chatId, status) =>
    set((s) => ({
      agents: s.agents.map((a) =>
        a.chat_id === chatId ? { ...a, status: status as AgentInfo["status"] } : a
      ),
    })),

  // Tools
  toolExecutions: [],
  addToolExecution: (t) =>
    set((s) => ({
      toolExecutions: [t, ...s.toolExecutions].slice(0, 100),
    })),
  setToolExecutions: (t) => set({ toolExecutions: t }),

  // Decisions
  decisions: [],
  addDecision: (d) =>
    set((s) => ({
      decisions: [d, ...s.decisions].slice(0, 50),
    })),
  setDecisions: (d) => set({ decisions: d }),

  // Git
  gitState: null,
  setGitState: (g) => set({ gitState: g }),

  // Security
  securityEvents: [],
  addSecurityEvent: (e) =>
    set((s) => ({
      securityEvents: [e, ...s.securityEvents].slice(0, 100),
    })),

  // Reviews
  reviewEvents: [],
  addReviewEvent: (r) =>
    set((s) => ({
      reviewEvents: [r, ...s.reviewEvents].slice(0, 50),
    })),

  // WS Events
  wsEvents: [],
  addWsEvent: (e) =>
    set((s) => ({
      wsEvents: [e, ...s.wsEvents].slice(0, 200),
    })),

  // Dispatcher
  handleWsEvent: (event) => {
    const state = get();
    state.addWsEvent(event);

    switch (event.type) {
      case "tool_execution_start":
      case "tool_execution":
        state.addToolExecution(event.data as ToolExecution);
        break;
      case "task_updated":
        state.addDecision(unifiedTaskToDecision(event.data as UnifiedTask));
        break;
      case "decision_complete":
        state.addDecision(event.data as DecisionLog);
        break;
      case "agent_status": {
        const d = event.data as { chat_id: number; status: string };
        state.updateAgentStatus(d.chat_id, d.status);
        break;
      }
      case "security_alert":
        state.addSecurityEvent(event.data as SecurityEvent);
        break;
      case "review_result":
      case "review_complete": {
        const data = event.data as Partial<ReviewLiveEvent> & {
          feedback?: string;
          sub_task_results?: UnifiedReviewSubTaskResult[];
        };
        const taskId = String(data.task_id || "").trim();
        if (!taskId) break;
        state.addReviewEvent({
          task_id: taskId,
          reviewer_model: String(data.reviewer_model || "").trim(),
          verdict: String(data.verdict || "partial"),
          overall_score: Number(data.overall_score || 0),
          issue_tags: Array.isArray(data.issue_tags) ? data.issue_tags.map((tag) => String(tag)) : [],
          advisory_retry: Boolean(data.advisory_retry),
          failing_subtasks: Number(data.failing_subtasks || 0),
          retry_note: data.retry_note ? String(data.retry_note) : "",
          feedback_text: data.feedback_text ? String(data.feedback_text) : data.feedback ? String(data.feedback) : "",
          timestamp: event.timestamp,
          sub_task_results: Array.isArray(data.sub_task_results)
            ? (data.sub_task_results as UnifiedReviewSubTaskResult[])
            : [],
        });
        break;
      }
      // performance_metric handled separately if needed
    }
  },
}), {
  name: "alice-app-store",
}));
