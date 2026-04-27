import type { DecisionLog, UnifiedReview, ReviewLiveEvent, UnifiedReviewSubTaskResult } from "@/types/alice";

export type ReviewSource = "stored" | "live";
export type ReviewRunSource = "initial" | "retry" | string;

export interface ReviewFeedItem {
  key: string;
  task_id: string;
  goal: string;
  project_path: string;
  reviewer_model: string;
  verdict: string;
  overall_score: number;
  issue_tags: string[];
  feedback_text: string;
  sub_task_results: UnifiedReviewSubTaskResult[];
  timestamp: string;
  source: ReviewSource;
  run_source: ReviewRunSource;
  advisory_retry: boolean;
  retry_note: string;
  failing_subtasks: number;
  block_count: number;
  auto_fixed_count: number;
}

export interface ReviewSummaryStats {
  total: number;
  passRate: string;
  blockRate: string;
  postBlockAutoFixRate: string;
  avgScore: string;
  liveCount: number;
  withFeedback: number;
  verdicts: {
    allow: number;
    pass: number;
    partial: number;
    fail: number;
    block: number;
  };
  topIssueTags: Array<{ tag: string; value: number }>;
}

function normalizeVerdict(verdict?: string): "allow" | "pass" | "partial" | "fail" | "block" {
  if (verdict === "pass" || verdict === "fail" || verdict === "block" || verdict === "allow") return verdict;
  return "partial";
}

function buildReviewKey(review: {
  id?: number;
  task_id?: string;
  reviewer_model?: string;
  verdict?: string;
  overall_score?: number;
  source?: string;
}, fallbackTaskId: string): string {
  if (review.id !== undefined) {
    return `${review.task_id || fallbackTaskId}|review:${review.id}`;
  }
  return [
    review.task_id || fallbackTaskId,
    review.reviewer_model || "reviewer",
    review.verdict || "partial",
    review.overall_score || 0,
    review.source || "live",
  ].join("|");
}

export function normalizeStoredReview(review: UnifiedReview, decision: DecisionLog): ReviewFeedItem {
  return {
    key: buildReviewKey(review, decision.id),
    task_id: review.task_id || decision.id,
    goal: decision.user_prompt || "",
    project_path: decision.project_path || "",
    reviewer_model: review.reviewer_model || "reviewer",
    verdict: review.verdict || "partial",
    overall_score: review.overall_score || 0,
    issue_tags: review.issue_tags || [],
    feedback_text: review.feedback_text || "",
    sub_task_results: review.sub_task_results || [],
    timestamp: review.created_at || decision.timestamp || "",
    source: "stored",
    run_source: review.source || "initial",
    advisory_retry: review.verdict !== "pass",
    retry_note: review.verdict === "pass" ? "暫無需重跑" : "建議人工評估後再決定是否重跑",
    failing_subtasks: review.sub_task_results?.filter((subTask) => subTask.score < 70).length || 0,
    block_count: review.block_count ?? 0,
    auto_fixed_count: review.auto_fixed_count ?? 0,
  };
}

export function normalizeLiveReview(review: ReviewLiveEvent, decision?: DecisionLog): ReviewFeedItem {
  return {
    key: buildReviewKey(review, review.task_id),
    task_id: review.task_id,
    goal: decision?.user_prompt || "",
    project_path: decision?.project_path || "",
    reviewer_model: review.reviewer_model || "reviewer",
    verdict: review.verdict || "partial",
    overall_score: review.overall_score || 0,
    issue_tags: review.issue_tags || [],
    feedback_text: review.feedback_text || "",
    sub_task_results: review.sub_task_results || [],
    timestamp: review.timestamp || "",
    source: "live",
    run_source: review.source || "initial",
    advisory_retry: Boolean(review.advisory_retry),
    retry_note: review.retry_note || "",
    failing_subtasks: review.failing_subtasks || 0,
    block_count: 0,
    auto_fixed_count: 0,
  };
}

export function mergeReviewItems(items: ReviewFeedItem[]): ReviewFeedItem[] {
  const map = new Map<string, ReviewFeedItem>();

  for (const item of items) {
    const existing = map.get(item.key);
    if (!existing) {
      map.set(item.key, item);
      continue;
    }

    if (existing.source === "stored") {
      continue;
    }

    if (item.source === "stored") {
      map.set(item.key, item);
      continue;
    }

    if (
      item.sub_task_results.length > existing.sub_task_results.length ||
      (!existing.feedback_text && item.feedback_text) ||
      (item.timestamp && existing.timestamp && item.timestamp > existing.timestamp)
    ) {
      map.set(item.key, item);
    }
  }

  return Array.from(map.values()).sort((a, b) => {
    const aTs = new Date(a.timestamp || 0).getTime();
    const bTs = new Date(b.timestamp || 0).getTime();
    return bTs - aTs;
  });
}

export function computeReviewSummary(reviews: ReviewFeedItem[]): ReviewSummaryStats {
  const verdicts = { allow: 0, pass: 0, partial: 0, fail: 0, block: 0 };
  const tags = new Map<string, number>();
  let scoreSum = 0;
  let withFeedback = 0;
  let liveCount = 0;
  let totalBlockCount = 0;
  let totalAutoFixedCount = 0;

  for (const review of reviews) {
    const verdict = normalizeVerdict(review.verdict);
    verdicts[verdict] += 1;
    scoreSum += review.overall_score || 0;
    if (review.feedback_text) withFeedback += 1;
    if (review.source === "live") liveCount += 1;
    totalBlockCount += review.block_count || 0;
    totalAutoFixedCount += review.auto_fixed_count || 0;
    for (const tag of review.issue_tags || []) {
      tags.set(tag, (tags.get(tag) || 0) + 1);
    }
  }

  const total = reviews.length;
  return {
    total,
    passRate: total > 0 ? ((verdicts.pass / total) * 100).toFixed(0) : "0",
    blockRate: total > 0 ? ((verdicts.block / total) * 100).toFixed(0) : "0",
    postBlockAutoFixRate: totalBlockCount > 0 ? ((totalAutoFixedCount / totalBlockCount) * 100).toFixed(0) : "0",
    avgScore: total > 0 ? (scoreSum / total).toFixed(1) : "0.0",
    liveCount,
    withFeedback,
    verdicts,
    topIssueTags: Array.from(tags.entries())
      .sort((a, b) => b[1] - a[1])
      .slice(0, 5)
      .map(([tag, value]) => ({ tag, value })),
  };
}

export function buildReviewVerdictChartData(stats: ReviewSummaryStats) {
  return [
    { name: "Pass", value: stats.verdicts.pass, color: "#22c55e" },
    { name: "Partial", value: stats.verdicts.partial, color: "#f59e0b" },
    { name: "Fail", value: stats.verdicts.fail, color: "#ef4444" },
    { name: "Block", value: stats.verdicts.block, color: "#8b5cf6" },
  ].filter((item) => item.value > 0);
}
