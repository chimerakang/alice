import { renderToStaticMarkup } from "react-dom/server";
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import i18n from "@/i18n";
import ReviewSubTaskTable from "./ReviewSubTaskTable";

afterEach(() => {
  expandedSubTaskIds = null;
  vi.restoreAllMocks();
});

let expandedSubTaskIds: string[] | null = null;

beforeAll(async () => {
  await i18n.changeLanguage("zh-TW");
});

vi.mock("react", async () => {
  const actual = await vi.importActual<typeof import("react")>("react");

  return {
    ...actual,
    useState: (((initial: unknown) => [expandedSubTaskIds ?? initial, vi.fn()]) as unknown) as typeof actual.useState,
  };
});

describe("ReviewSubTaskTable", () => {
  it("renders nothing when there are no sub-task results", () => {
    const html = renderToStaticMarkup(<ReviewSubTaskTable subTaskResults={[]} />);
    expect(html).toBe("");
  });

  it("renders an incomplete schema diagnostic when expected subtasks are missing", () => {
    const html = renderToStaticMarkup(
      <ReviewSubTaskTable
        expectedSubTaskIds={["inspect-review-output", "verify-implementation", "capture-evidence"]}
        subTaskResults={[]}
      />
    );

    expect(html).toContain("Review schema 不完整");
    expect(html).toContain("預期 3 筆 sub_task_results，但實際只有 0 筆。");
    expect(html).toContain("缺少 subtask");
    expect(html).toContain("inspect-review-output");
    expect(html).toContain("verify-implementation");
    expect(html).toContain("capture-evidence");
    expect(html).not.toContain("Sub-task 評分");
  });

  it("renders expanded feedback and issue tags for a sub-task row", () => {
    expandedSubTaskIds = ["inspect-review-output-and-verify-implementation-completeness"];

    const html = renderToStaticMarkup(
      <ReviewSubTaskTable
        expectedSubTaskIds={["inspect-review-output-and-verify-implementation-completeness"]}
        subTaskResults={[
          {
            review_id: 7,
            sub_task_id: "inspect-review-output-and-verify-implementation-completeness",
            score: 88,
            feedback: "Looks good overall.",
            issue_tags: ["missing_context", "needs_follow_up"],
          },
        ]}
      />
    );

    expect(html).toContain("Sub-task 評分");
    expect(html).toContain("描述（前 60 字）");
    expect(html).toContain("88/100");
    expect(html).toContain('aria-expanded="true"');
    expect(html).toContain("Looks good overall.");
    expect(html).toContain("missing_context");
    expect(html).toContain("needs_follow_up");
    expect(html).toContain("inspect-review-output-and-verify-implementation-completeness");
  });
});
