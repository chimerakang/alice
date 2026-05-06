import { renderToStaticMarkup } from "react-dom/server";
import type { ReactNode } from "react";
import { beforeAll, describe, expect, it, vi } from "vitest";
import i18n from "@/i18n";
import { DecisionDetail, findFocusedDecision } from "./Timeline";
import type { DecisionLog, UnifiedReview } from "@/types/alice";

vi.mock("@/components/CollapsiblePanel", () => ({
  default: ({
    children,
    title,
    defaultOpen,
  }: {
    children: ReactNode;
    title: ReactNode;
    defaultOpen?: boolean;
  }) => (
    <div data-testid="collapsible" data-title={typeof title === "string" ? title : ""} data-default-open={String(Boolean(defaultOpen))}>
      {children}
    </div>
  ),
}));

vi.mock("@/components/DiffViewer", () => ({
  default: () => null,
}));

vi.mock("@/components/ToolCallGantt", () => ({
  default: () => null,
}));

beforeAll(async () => {
  await i18n.changeLanguage("zh-TW");
});

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

describe("DecisionDetail", () => {
  it("renders the review sub-task table when sub-task results are present", () => {
    const html = renderToStaticMarkup(
      <DecisionDetail
        decision={buildDecision([
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
            sub_task_results: [
              {
                review_id: 11,
                sub_task_id: "inspect-review-output-and-verify-implementation-completeness",
                score: 88,
                feedback: "Looks good overall.",
                issue_tags: ["missing_context", "needs_follow_up"],
              },
            ],
          } as UnifiedReview,
        ])}
        decisions={[buildDecision([])]}
        onClose={() => {}}
        onNavigate={() => {}}
      />
    );

    expect(html).toContain("Sub-task 評分");
    expect(html).toContain("88/100");
    expect(html).toContain("Looks good overall.");
    expect(html).toContain("missing_context");
  });

  it("does not render an empty sub-task table when results are absent", () => {
    const html = renderToStaticMarkup(
      <DecisionDetail
        decision={buildDecision([
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
        ])}
        decisions={[buildDecision([])]}
        onClose={() => {}}
        onNavigate={() => {}}
      />
    );

    expect(html).not.toContain("Sub-task 評分");
  });

  it("defaults the reviews panel open when focus navigation requests it", () => {
    const html = renderToStaticMarkup(
      <DecisionDetail
        decision={buildDecision([
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
        ])}
        decisions={[buildDecision([])]}
        onClose={() => {}}
        onNavigate={() => {}}
        openReviewsByDefault={true}
      />
    );

    expect(html).toContain('data-title="Reviews (1)"');
    expect(html).toContain('data-default-open="true"');
  });
});

describe("findFocusedDecision", () => {
  it("returns the matching decision for a focus id", () => {
    const target = buildDecision([]);
    const other = {
      ...buildDecision([]),
      id: "decision-2",
    };

    expect(findFocusedDecision([other, target], "decision-1")).toBe(target);
  });

  it("ignores blank focus ids", () => {
    expect(findFocusedDecision([buildDecision([])], "   ")).toBeNull();
  });
});
