// @vitest-environment jsdom

import type { ReactNode } from "react";
import { act } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { createRoot } from "react-dom/client";
import { MemoryRouter } from "react-router-dom";
import type { DecisionLog, UnifiedReview } from "@/types/alice";
import Timeline from "./Timeline";

(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const getTaskDecisions = vi.fn();

vi.mock("@/stores/appStore", () => ({
  useAppStore: () => ({
    decisions: [],
    wsConnected: false,
  }),
}));

vi.mock("@/lib/api", () => ({
  api: {
    getTaskDecisions: (...args: unknown[]) => getTaskDecisions(...args),
  },
}));

vi.mock("@/components/TimelineEntry", () => ({
  default: ({
    decision,
    onSelect,
  }: {
    decision: DecisionLog;
    onSelect: (value: DecisionLog) => void;
  }) => (
    <button
      type="button"
      data-testid="timeline-entry"
      onClick={() => onSelect(decision)}
    >
      Open {decision.id}
    </button>
  ),
}));

vi.mock("@/components/SearchFilter", () => ({
  default: () => <div data-testid="search-filter" />,
}));

vi.mock("@/components/DateRangeFilter", () => ({
  default: () => <div data-testid="date-range-filter" />,
}));

vi.mock("@/components/DiffViewer", () => ({
  default: () => null,
}));

vi.mock("@/components/MarkdownRenderer", () => ({
  default: ({ content }: { content: string }) => <div>{content}</div>,
}));

vi.mock("@/components/ToolCallGantt", () => ({
  default: () => null,
}));

vi.mock("@/components/StatusBadge", () => ({
  default: ({ children }: { children?: ReactNode }) => <span>{children}</span>,
}));

function buildDecision(reviews: UnifiedReview[]): DecisionLog {
  return {
    id: "decision-1",
    timestamp: "2026-04-26T12:00:00Z",
    session_id: "session-1",
    project_path: "/repo",
    user_prompt: "Review the timeline entry",
    agent_response: "ok",
    tool_calls: [],
    task_type: "review",
    outcome: {
      success: true,
      error_message: "",
      severity: "SEVERITY_LOW",
    },
    duration_ms: 1200,
    tokens_input: 10,
    tokens_output: 20,
    cost_usd: 0.01,
    chat_id: 1,
    thread_id: 2,
    unified_task: {
      id: "task-1",
      chat_id: 1,
      thread_id: 2,
      project_dir: "/repo",
      goal: "Verify review output",
      engine: "plan-execute",
      backend: "local",
      status: "done",
      started_at: "2026-04-26T11:50:00Z",
      ended_at: "2026-04-26T12:00:00Z",
      total_input_tokens: 10,
      total_output_tokens: 20,
      total_cost_usd: 0.01,
      sub_tasks: [],
      reviews,
    },
  } as DecisionLog;
}

async function renderTimeline(initialEntry: string) {
  const container = document.createElement("div");
  document.body.appendChild(container);
  const root = createRoot(container);

  await act(async () => {
    root.render(
      <MemoryRouter initialEntries={[initialEntry]}>
        <Timeline />
      </MemoryRouter>,
    );
  });

  return { container, root };
}

async function flushEffects() {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}

afterEach(() => {
  document.body.innerHTML = "";
  getTaskDecisions.mockReset();
  vi.clearAllMocks();
});

describe("Timeline focus navigation", () => {
  it("opens the reviews panel by default when focus is present", async () => {
    getTaskDecisions.mockResolvedValue({
      decisions: [
        buildDecision([
          {
            task_id: "task-1",
            reviewer_model: "gpt-5.5",
            verdict: "pass",
            overall_score: 92,
            feedback_text: "Strong result.",
            issue_tags: ["missing_context"],
            input_tokens: 0,
            output_tokens: 0,
            cost_usd: 0,
            created_at: "2026-04-26T12:00:00Z",
            sub_task_results: [],
          } as UnifiedReview,
        ]),
      ],
      total: 1,
    });

    const { container, root } = await renderTimeline("/timeline?focus=decision-1");
    await flushEffects();

    expect(container.textContent).toContain("Strong result.");

    act(() => {
      root.unmount();
    });
  });

  it("keeps the reviews panel collapsed when focus is absent until the decision is selected", async () => {
    getTaskDecisions.mockResolvedValue({
      decisions: [
        buildDecision([
          {
            task_id: "task-1",
            reviewer_model: "gpt-5.5",
            verdict: "pass",
            overall_score: 92,
            feedback_text: "Strong result.",
            issue_tags: ["missing_context"],
            input_tokens: 0,
            output_tokens: 0,
            cost_usd: 0,
            created_at: "2026-04-26T12:00:00Z",
            sub_task_results: [],
          } as UnifiedReview,
        ]),
      ],
      total: 1,
    });

    const { container, root } = await renderTimeline("/timeline");
    await flushEffects();

    const entry = container.querySelector("[data-testid='timeline-entry']");
    expect(entry).not.toBeNull();

    act(() => {
      entry?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });

    expect(container.textContent).toContain("Reviews (1)");
    expect(container.textContent).not.toContain("Strong result.");

    act(() => {
      root.unmount();
    });
  });
});
