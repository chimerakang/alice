import { renderToStaticMarkup } from "react-dom/server";
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import ReviewSummaryPanel from "./ReviewSummaryPanel";

vi.mock("@/components/StatusBadge", () => ({
  default: ({ children }: { children?: string }) => <span data-testid="status-badge">{children}</span>,
}));

vi.mock("recharts", () => {
  const passthrough = (name: string) =>
    function MockChart({ children }: { children?: ReactNode }) {
      return <div data-testid={name}>{children}</div>;
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

describe("ReviewSummaryPanel", () => {
  it("renders empty state when there are no reviews", () => {
    const html = renderToStaticMarkup(<ReviewSummaryPanel reviews={[]} />);

    expect(html).toContain("Review Summary");
    expect(html).toContain("目前還沒有 review 結果");
  });

  it("renders summary stats and top 5 issue tags", () => {
    const html = renderToStaticMarkup(
      <ReviewSummaryPanel
        liveConnected={true}
        reviews={[
          {
            key: "review-1",
            task_id: "task-1",
            goal: "改善登入",
            project_path: "/repo",
            reviewer_model: "gpt-5.5",
            verdict: "pass",
            overall_score: 90,
            issue_tags: ["tag-a", "tag-b", "tag-c"],
            feedback_text: "ok",
            sub_task_results: [],
            timestamp: "2026-04-26T10:00:00Z",
            source: "stored",
            run_source: "initial",
            advisory_retry: false,
            retry_note: "暫無需重跑",
            failing_subtasks: 0,
          },
          {
            key: "review-2",
            task_id: "task-2",
            goal: "修補 webhook",
            project_path: "/repo",
            reviewer_model: "gpt-5.5",
            verdict: "fail",
            overall_score: 70,
            issue_tags: ["tag-a", "tag-d", "tag-e", "tag-f", "tag-g", "tag-h"],
            feedback_text: "needs more work",
            sub_task_results: [],
            timestamp: "2026-04-26T11:00:00Z",
            source: "live",
            run_source: "initial",
            advisory_retry: true,
            retry_note: "建議人工評估後再決定是否重跑",
            failing_subtasks: 1,
          },
        ]}
      />
    );

    expect(html).toContain("Reviews");
    expect(html).toContain("2");
    expect(html).toContain("Pass Rate");
    expect(html).toContain("50%");
    expect(html).toContain("Avg Score");
    expect(html).toContain("80.0");
    expect(html).toContain("Live");
    expect(html).toContain("Verdict Distribution");
    expect(html).toContain("Top Issue Tags");
    expect(html).toContain("tag-a");
    expect(html).toContain("tag-e");
    expect(html).not.toContain("tag-f");
    expect(html).not.toContain("tag-g");
    expect(html).not.toContain("tag-h");
  });
});
