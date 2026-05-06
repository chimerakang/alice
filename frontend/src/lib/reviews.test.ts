import { describe, expect, it } from "vitest";
import { buildReviewVerdictChartData, computeReviewSummary, normalizeStoredReview } from "./reviews";
import type { DecisionLog, UnifiedReview } from "@/types/alice";

function baseDecision(): DecisionLog {
  return {
    id: "decision-1",
    timestamp: "2026-04-26T12:00:00Z",
    session_id: "session-1",
    project_path: "/repo",
    user_prompt: "deploy release",
    agent_response: "done",
    tool_calls: [],
    task_type: "direct",
    outcome: {
      success: true,
      error_message: "",
      severity: "SEVERITY_LOW",
    },
    duration_ms: 1000,
    tokens_input: 1,
    tokens_output: 2,
    cost_usd: 0.01,
    chat_id: 1,
    thread_id: 0,
    source: "telegram",
  };
}

describe("review summary metrics", () => {
  it("computes block and post-block auto-fix rates", () => {
    const stats = computeReviewSummary([
      {
        key: "a",
        task_id: "a",
        goal: "deploy",
        project_path: "/repo",
        reviewer_model: "gpt-5.5",
        verdict: "block",
        block_count: 2,
        auto_fixed_count: 0,
        overall_score: 60,
        issue_tags: ["scope_creep"],
        feedback_text: "retry",
        sub_task_results: [],
        timestamp: "2026-04-26T12:00:00Z",
        source: "stored",
        run_source: "initial",
        advisory_retry: true,
        retry_note: "manual_review",
        failing_subtasks: 0,
      },
      {
        key: "b",
        task_id: "b",
        goal: "deploy",
        project_path: "/repo",
        reviewer_model: "gpt-5.5",
        verdict: "pass",
        block_count: 0,
        auto_fixed_count: 2,
        overall_score: 90,
        issue_tags: [],
        feedback_text: "fixed",
        sub_task_results: [],
        timestamp: "2026-04-26T13:00:00Z",
        source: "stored",
        run_source: "retry",
        advisory_retry: false,
        retry_note: "no_retry",
        failing_subtasks: 0,
      },
    ]);

    expect(stats.verdicts.block).toBe(1);
    expect(stats.blockRate).toBe("50");
    expect(stats.postBlockAutoFixRate).toBe("100");
  });

  it("preserves block metrics during normalization", () => {
    const review: UnifiedReview = {
      id: 7,
      task_id: "task-7",
      reviewer_model: "gpt-5.5",
      verdict: "block",
      block_count: 2,
      auto_fixed_count: 1,
      overall_score: 64,
      feedback_text: "needs validation",
      issue_tags: ["scope_creep"],
      input_tokens: 10,
      output_tokens: 5,
      cost_usd: 0.1,
      source: "initial",
      created_at: "2026-04-26T12:00:00Z",
      sub_task_results: [],
    };

    const item = normalizeStoredReview(review, baseDecision());
    expect(item.block_count).toBe(2);
    expect(item.auto_fixed_count).toBe(1);
    expect(item.retry_note).toBe("manual_review");
  });

  it("includes block in verdict chart data", () => {
    const chart = buildReviewVerdictChartData({
      total: 1,
      passRate: "0",
      avgScore: "0",
      blockRate: "100",
      postBlockAutoFixRate: "0",
      liveCount: 0,
      withFeedback: 0,
      verdicts: { allow: 0, pass: 0, partial: 0, fail: 0, block: 1 },
      topIssueTags: [],
    });

    expect(chart.map((entry) => entry.verdict)).toContain("block");
  });
});
