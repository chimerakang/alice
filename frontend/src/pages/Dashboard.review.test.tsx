import { renderToStaticMarkup } from "react-dom/server";
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import { ReviewPanel } from "./Dashboard";

vi.mock("@/components/StatusBadge", () => ({
  default: ({ children }: { children?: string }) => <span data-testid="status-badge">{children}</span>,
}));

vi.mock("@/components/DateRangeFilter", () => ({
  default: () => null,
}));

vi.mock("@/components/SourceDistributionChart", () => ({
  default: () => null,
}));

vi.mock("@/components/SourcePerformanceChart", () => ({
  default: () => null,
}));

vi.mock("@/components/SavingsBanner", () => ({
  SavingsBanner: () => null,
}));

vi.mock("@/components/ModelDistributionChart", () => ({
  ModelDistributionChart: () => null,
}));

vi.mock("@/components/CostTrendChart", () => ({
  CostTrendChart: () => null,
}));

vi.mock("recharts", () => {
  const passthrough = (name: string) =>
    function MockChart({ children }: { children?: ReactNode }) {
      return (
        <div data-testid={name}>
          {children}
        </div>
      );
    };

  return {
    ResponsiveContainer: passthrough("ResponsiveContainer"),
    PieChart: passthrough("PieChart"),
    Pie: passthrough("Pie"),
    BarChart: passthrough("BarChart"),
    Bar: passthrough("Bar"),
    Cell: () => null,
    XAxis: () => null,
    YAxis: () => null,
    Tooltip: () => null,
    CartesianGrid: () => null,
  };
});

describe("ReviewPanel", () => {
  it("renders review summary, tags, and recent review details", () => {
    const html = renderToStaticMarkup(
      <ReviewPanel
        liveConnected={true}
        reviews={[
          {
            key: "task-1|gpt-5.5|pass|90",
            task_id: "task-1",
            goal: "修正登入流程",
            project_path: "/repo",
            reviewer_model: "gpt-5.5",
            verdict: "pass",
            overall_score: 90,
            issue_tags: ["missing_validation"],
            feedback_text: "ok",
            sub_task_results: [],
            timestamp: "2026-04-26T12:00:00Z",
            source: "stored",
            advisory_retry: false,
            retry_note: "暫無需重跑",
            failing_subtasks: 0,
          },
          {
            key: "task-2|gpt-5.5|partial|70",
            task_id: "task-2",
            goal: "補 review 迴路",
            project_path: "/repo",
            reviewer_model: "gpt-5.5",
            verdict: "partial",
            overall_score: 70,
            issue_tags: ["missing_context", "missing_validation"],
            feedback_text: "needs follow-up",
            sub_task_results: [],
            timestamp: "2026-04-26T13:00:00Z",
            source: "live",
            advisory_retry: true,
            retry_note: "建議人工評估後再決定是否重跑",
            failing_subtasks: 1,
          },
          {
            key: "task-3|gpt-5.5|fail|50",
            task_id: "task-3",
            goal: "修正 webhook",
            project_path: "/repo",
            reviewer_model: "gpt-5.5",
            verdict: "fail",
            overall_score: 50,
            issue_tags: ["missing_context"],
            feedback_text: "not enough context",
            sub_task_results: [
              {
                review_id: 3,
                sub_task_id: "task-3:s1",
                score: 60,
                feedback: "needs more context",
                issue_tags: ["missing_context"],
              },
            ],
            timestamp: "2026-04-26T14:00:00Z",
            source: "stored",
            advisory_retry: true,
            retry_note: "建議人工評估後再決定是否重跑",
            failing_subtasks: 1,
          },
        ]}
      />
    );

    expect(html).toContain("Review Panel");
    expect(html).toContain("3 reviews");
    expect(html).toContain("Pass Rate");
    expect(html).toContain("33%");
    expect(html).toContain("Avg Score");
    expect(html).toContain("70.0");
    expect(html).toContain("Live");
    expect(html).toContain("missing_context");
    expect(html).toContain("task-3:s1");
    expect(html).toContain("gpt-5.5");
  });
});
