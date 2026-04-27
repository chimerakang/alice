// @vitest-environment jsdom

import type { ReactNode } from "react";
import { act } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { createRoot } from "react-dom/client";
import type { ReviewFeedItem } from "@/lib/reviews";
import { ReviewsPageView } from "./Reviews";

(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

vi.mock("@/components/ReviewSummaryPanel", () => ({
  default: () => <div data-testid="summary" />,
}));

vi.mock("@/components/DateRangeFilter", () => ({
  default: () => <div data-testid="date-range" />,
}));

vi.mock("@/components/SearchFilter", () => ({
  default: () => <div data-testid="search-filter" />,
}));

vi.mock("@/components/StatusBadge", () => ({
  default: ({ children }: { children?: ReactNode }) => <span>{children}</span>,
}));

function review(overrides: Partial<ReviewFeedItem>): ReviewFeedItem {
  return {
    key: "task-1|gpt-5.5|pass|90",
    task_id: "task-1",
    goal: "修正登入流程",
    project_path: "/repo-a",
    reviewer_model: "gpt-5.5",
    verdict: "pass",
    overall_score: 90,
    issue_tags: ["missing_validation"],
    feedback_text: "ok",
    sub_task_results: [],
    timestamp: "2026-04-26T12:00:00Z",
    source: "stored",
    run_source: "initial",
    advisory_retry: false,
    retry_note: "暫無需重跑",
    failing_subtasks: 0,
    ...overrides,
  };
}

async function renderReviewPage(onRowClick = vi.fn(), onSortChange = vi.fn()) {
  const container = document.createElement("div");
  document.body.appendChild(container);
  const root = createRoot(container);

  await act(async () => {
    root.render(
      <ReviewsPageView
        reviews={[
          review({
            key: "task-1",
            task_id: "task-1",
            goal: "修正登入流程",
            feedback_text: "login flow looks good",
            overall_score: 92,
          }),
        ]}
        loading={false}
        liveConnected={false}
        filters={{
          verdict: "all",
          projectPath: "all",
          reviewerModel: "all",
          search: "",
          timeRange: {},
        }}
        projectOptions={["/repo-a"]}
        reviewerOptions={["gpt-5.5"]}
        sortKey="timestamp"
        sortDirection="desc"
        onVerdictChange={() => {}}
        onProjectChange={() => {}}
        onReviewerModelChange={() => {}}
        onSearchChange={() => {}}
        onTimeRangeChange={() => {}}
        onSortChange={onSortChange}
        onRowClick={onRowClick}
      />
    );
  });

  return { container, root };
}

afterEach(() => {
  document.body.innerHTML = "";
  vi.clearAllMocks();
});

describe("ReviewsPageView interactions", () => {
  it("calls onRowClick when a review row is clicked", () => {
    const onRowClick = vi.fn();
    return renderReviewPage(onRowClick).then(({ container, root }) => {
      const row = container.querySelector("tbody tr");
      expect(row).not.toBeNull();

      act(() => {
        row?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
      });

      expect(onRowClick).toHaveBeenCalledTimes(1);
      expect(onRowClick).toHaveBeenCalledWith("task-1");

      act(() => {
        root.unmount();
      });
    });
  });

  it("calls onSortChange when a sortable header is clicked", () => {
    const onSortChange = vi.fn();
    return renderReviewPage(vi.fn(), onSortChange).then(({ container, root }) => {
      const scoreButton = Array.from(container.querySelectorAll("button")).find(
        (button) => button.textContent?.includes("score"),
      );
      expect(scoreButton).toBeDefined();

      act(() => {
        scoreButton?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
      });

      expect(onSortChange).toHaveBeenCalledTimes(1);
      expect(onSortChange).toHaveBeenCalledWith("overall_score");

      act(() => {
        root.unmount();
      });
    });
  });
});
