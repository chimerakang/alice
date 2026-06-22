import { renderToStaticMarkup } from "react-dom/server";
import type { ReactNode } from "react";
import { beforeAll, describe, expect, it, vi } from "vitest";
import i18n from "@/i18n";
import { ReviewsPageView, buildTimelineFocusPath, filterAndSortReviews } from "./Reviews";
import type { ReviewFeedItem } from "@/lib/reviews";

vi.mock("@/components/ReviewSummaryPanel", () => ({
  default: ({
    reviews,
    emptyMessage,
  }: {
    reviews: ReviewFeedItem[];
    emptyMessage?: string;
  }) => (
    <div data-testid="summary">
      {reviews.length}:{emptyMessage}
    </div>
  ),
}));

vi.mock("@/components/DateRangeFilter", () => ({
  default: () => <div data-testid="date-range" />,
}));

vi.mock("@/components/SearchFilter", () => ({
  default: () => <div data-testid="search-filter" />,
}));

vi.mock("@/components/StatusBadge", () => ({
  default: ({ children, title }: { children?: ReactNode; title?: string }) => (
    <span data-testid="status-badge" title={title}>
      {children}
    </span>
  ),
}));

beforeAll(async () => {
  await i18n.changeLanguage("zh-TW");
});

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
    retry_note: "no_retry",
    failing_subtasks: 0,
    ...overrides,
  };
}

describe("filterAndSortReviews", () => {
  it("filters by verdict, project, reviewer, search, and time range", () => {
    const result = filterAndSortReviews(
      [
        review({
          key: "a",
          task_id: "a",
          goal: "修正登入流程",
          feedback_text: "login flow looks good",
          project_path: "/repo-a",
          reviewer_model: "gpt-5.5",
          verdict: "pass",
          timestamp: "2026-04-26T13:00:00Z",
        }),
        review({
          key: "b",
          task_id: "b",
          goal: "修正 webhook",
          feedback_text: "needs more context",
          project_path: "/repo-b",
          reviewer_model: "gpt-4.1",
          verdict: "fail",
          timestamp: "2026-04-26T14:00:00Z",
        }),
      ],
      {
        verdict: "fail",
        projectPath: "/repo-b",
        reviewerModel: "gpt-4.1",
        search: "context",
        timeRange: {
          startTime: "2026-04-26T13:30:00Z",
          endTime: "2026-04-26T14:30:00Z",
        },
      },
      "timestamp",
      "desc",
    );

    expect(result).toHaveLength(1);
    expect(result[0].task_id).toBe("b");
  });

  it("matches search terms in goal text", () => {
    const result = filterAndSortReviews(
      [
        review({
          key: "goal-match",
          task_id: "goal-match",
          goal: "修正登入流程",
          feedback_text: "irrelevant",
        }),
        review({
          key: "feedback-match",
          task_id: "feedback-match",
          goal: "其他任務",
          feedback_text: "looks good",
        }),
      ],
      {
        verdict: "all",
        projectPath: "all",
        reviewerModel: "all",
        search: "登入",
        timeRange: {},
      },
      "timestamp",
      "desc",
    );

    expect(result.map((item) => item.task_id)).toEqual(["goal-match"]);
  });

  it("sorts by timestamp desc by default", () => {
    const result = filterAndSortReviews(
      [
        review({
          key: "older",
          task_id: "older",
          goal: "舊 review",
          timestamp: "2026-04-26T11:00:00Z",
        }),
        review({
          key: "newer",
          task_id: "newer",
          goal: "新 review",
          timestamp: "2026-04-26T15:00:00Z",
        }),
      ],
      {
        verdict: "all",
        projectPath: "all",
        reviewerModel: "all",
        search: "",
        timeRange: {},
      },
      "timestamp",
      "desc",
    );

    expect(result.map((item) => item.task_id)).toEqual(["newer", "older"]);
  });

  it("sorts by score in ascending order when requested", () => {
    const result = filterAndSortReviews(
      [
        review({
          key: "higher",
          task_id: "higher",
          overall_score: 95,
        }),
        review({
          key: "lower",
          task_id: "lower",
          overall_score: 70,
        }),
      ],
      {
        verdict: "all",
        projectPath: "all",
        reviewerModel: "all",
        search: "",
        timeRange: {},
      },
      "overall_score",
      "asc",
    );

    expect(result.map((item) => item.task_id)).toEqual(["lower", "higher"]);
  });
});

describe("buildTimelineFocusPath", () => {
  it("encodes the task id in the focus query", () => {
    expect(buildTimelineFocusPath("task 1/alpha")).toBe("/timeline?focus=task%201%2Falpha");
  });
});

describe("ReviewsPageView", () => {
  it("renders empty guidance when there are no reviews", () => {
    const html = renderToStaticMarkup(
      <ReviewsPageView
        reviews={[]}
        loading={false}
        liveConnected={false}
        filters={{
          verdict: "all",
          projectPath: "all",
          reviewerModel: "all",
          search: "",
          timeRange: {},
        }}
        projectOptions={[]}
        reviewerOptions={[]}
        sortKey="timestamp"
        sortDirection="desc"
        onVerdictChange={() => {}}
        onProjectChange={() => {}}
        onReviewerModelChange={() => {}}
        onSearchChange={() => {}}
        onTimeRangeChange={() => {}}
        onSortChange={() => {}}
        onRowClick={() => {}}
      />
    );

    expect(html).toContain("目前還沒有 review 結果");
    expect(html).toContain("Review 紀錄");
  });

  it("renders translated retry note labels from retry_note codes", () => {
    const html = renderToStaticMarkup(
      <ReviewsPageView
        reviews={[
          review({
            key: "retry",
            task_id: "retry",
            run_source: "retry",
            advisory_retry: true,
            retry_note: "manual_review",
            goal: "需要重跑的 review",
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
        onSortChange={() => {}}
        onRowClick={() => {}}
      />
    );

    expect(html).toContain("建議人工評估後再決定是否重跑");
    expect(html).toContain("重跑");
  });

  it("renders filtered rows in timestamp order", () => {
    const html = renderToStaticMarkup(
      <ReviewsPageView
        reviews={[
          review({
            key: "older",
            task_id: "older",
            goal: "舊 review",
            feedback_text: "older feedback",
            timestamp: "2026-04-26T11:00:00Z",
          }),
          review({
            key: "newer",
            task_id: "newer",
            goal: "新 review",
            feedback_text: "newer feedback",
            timestamp: "2026-04-26T15:00:00Z",
          }),
        ]}
        loading={false}
        liveConnected={true}
        filters={{
          verdict: "all",
          projectPath: "all",
          reviewerModel: "all",
          search: "newer",
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
        onSortChange={() => {}}
        onRowClick={() => {}}
      />
    );

    expect(html).toContain("新 review");
    expect(html).not.toContain("舊 review");
  });

  it("renders the filtered empty-state guidance when no reviews match", () => {
    const html = renderToStaticMarkup(
      <ReviewsPageView
        reviews={[
          review({
            key: "visible",
            task_id: "visible",
            goal: "可見 review",
            feedback_text: "visible feedback",
          }),
        ]}
        loading={false}
        liveConnected={false}
        filters={{
          verdict: "fail",
          projectPath: "all",
          reviewerModel: "all",
          search: "missing",
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
        onSortChange={() => {}}
        onRowClick={() => {}}
      />
    );

    expect(html).toContain("沒有符合條件的 review");
    expect(html).toContain("可調整 verdict、project、時間範圍或搜尋條件來縮小結果。");
  });
});
