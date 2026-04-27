import { useEffect, useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { ArrowDown, ArrowUp, ArrowUpDown, Clock, Loader2, Search } from "lucide-react";
import { clsx } from "clsx";
import { api } from "@/lib/api";
import { useAppStore } from "@/stores/appStore";
import type { DateRange } from "@/components/DateRangeFilter";
import DateRangeFilter from "@/components/DateRangeFilter";
import SearchFilter from "@/components/SearchFilter";
import StatusBadge from "@/components/StatusBadge";
import ReviewSummaryPanel from "@/components/ReviewSummaryPanel";
import {
  type ReviewFeedItem,
  normalizeLiveReview,
  normalizeStoredReview,
  mergeReviewItems,
} from "@/lib/reviews";
import type { DecisionLog } from "@/types/alice";

type ReviewVerdictFilter = "all" | "pass" | "partial" | "fail";
type ReviewSortKey = "timestamp" | "project_path" | "verdict" | "overall_score" | "reviewer_model" | "issue_tags" | "goal";
type SortDirection = "asc" | "desc";

interface ReviewFilters {
  verdict: ReviewVerdictFilter;
  projectPath: string;
  reviewerModel: string;
  search: string;
  timeRange: DateRange;
}

interface ReviewsPageViewProps {
  reviews: ReviewFeedItem[];
  loading: boolean;
  liveConnected: boolean;
  filters: ReviewFilters;
  projectOptions: string[];
  reviewerOptions: string[];
  sortKey: ReviewSortKey;
  sortDirection: SortDirection;
  onVerdictChange: (value: ReviewVerdictFilter) => void;
  onProjectChange: (value: string) => void;
  onReviewerModelChange: (value: string) => void;
  onSearchChange: (value: string) => void;
  onTimeRangeChange: (range: DateRange) => void;
  onSortChange: (key: ReviewSortKey) => void;
  onRowClick: (taskId: string) => void;
}

function formatTimestamp(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "—";
  return date.toLocaleString("zh-TW", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  });
}

function truncate(value: string, maxLength: number): string {
  const normalized = value.trim();
  if (normalized.length <= maxLength) return normalized;
  return `${normalized.slice(0, maxLength).trimEnd()}…`;
}

function verdictRank(verdict: string): number {
  if (verdict === "pass") return 2;
  if (verdict === "partial") return 1;
  if (verdict === "fail") return 0;
  return -1;
}

function getSortValue(review: ReviewFeedItem, key: ReviewSortKey): string | number {
  switch (key) {
    case "timestamp":
      return new Date(review.timestamp || 0).getTime();
    case "project_path":
      return review.project_path || "";
    case "verdict":
      return verdictRank(review.verdict);
    case "overall_score":
      return review.overall_score || 0;
    case "reviewer_model":
      return review.reviewer_model || "";
    case "issue_tags":
      return review.issue_tags?.length || 0;
    case "goal":
      return review.goal || review.feedback_text || "";
  }
}

export function filterAndSortReviews(
  reviews: ReviewFeedItem[],
  filters: ReviewFilters,
  sortKey: ReviewSortKey,
  sortDirection: SortDirection,
): ReviewFeedItem[] {
  const query = filters.search.trim().toLowerCase();
  const startTime = filters.timeRange.startTime ? new Date(filters.timeRange.startTime).getTime() : undefined;
  const endTime = filters.timeRange.endTime ? new Date(filters.timeRange.endTime).getTime() : undefined;

  const filtered = reviews.filter((review) => {
    if (filters.verdict !== "all" && review.verdict !== filters.verdict) return false;
    if (filters.projectPath !== "all" && review.project_path !== filters.projectPath) return false;
    if (filters.reviewerModel !== "all" && review.reviewer_model !== filters.reviewerModel) return false;

    const reviewTime = new Date(review.timestamp || 0).getTime();
    if (startTime !== undefined && reviewTime < startTime) return false;
    if (endTime !== undefined && reviewTime > endTime) return false;

    if (query) {
      const goalText = (review.goal || "").toLowerCase();
      const feedbackText = (review.feedback_text || "").toLowerCase();
      if (!goalText.includes(query) && !feedbackText.includes(query)) return false;
    }

    return true;
  });

  return filtered.sort((a, b) => {
    const aValue = getSortValue(a, sortKey);
    const bValue = getSortValue(b, sortKey);
    const multiplier = sortDirection === "asc" ? 1 : -1;

    if (typeof aValue === "number" && typeof bValue === "number") {
      return (aValue - bValue) * multiplier;
    }

    return String(aValue).localeCompare(String(bValue), "zh-TW") * multiplier;
  });
}

export function buildTimelineFocusPath(taskId: string): string {
  return `/timeline?focus=${encodeURIComponent(taskId)}`;
}

function SortButton({
  active,
  direction,
  onClick,
  children,
}: {
  active: boolean;
  direction: SortDirection;
  onClick: () => void;
  children: string;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={clsx(
        "inline-flex items-center gap-1.5 transition-colors",
        active ? "text-primary" : "text-gray-400 hover:text-gray-200",
      )}
    >
      <span>{children}</span>
      {active ? (
        direction === "asc" ? <ArrowUp className="w-3 h-3" /> : <ArrowDown className="w-3 h-3" />
      ) : (
        <ArrowUpDown className="w-3 h-3" />
      )}
    </button>
  );
}

function ReviewRow({
  review,
  onClick,
}: {
  review: ReviewFeedItem;
  onClick: (taskId: string) => void;
}) {
  const verdictVariant = review.verdict === "pass" ? "success" : review.verdict === "fail" ? "error" : "warning";
  const tags = review.issue_tags || [];

  return (
    <tr
      className="border-t border-gray-800/60 hover:bg-white/5 cursor-pointer"
      onClick={() => onClick(review.task_id)}
    >
      <td className="px-4 py-3 whitespace-nowrap text-xs text-gray-400 font-mono">
        {formatTimestamp(review.timestamp)}
      </td>
      <td className="px-4 py-3 text-sm text-gray-300">
        <div className="max-w-[240px] truncate" title={review.project_path || "—"}>
          {review.project_path || "—"}
        </div>
      </td>
      <td className="px-4 py-3">
        <div className="flex items-center gap-2">
          <StatusBadge variant={verdictVariant} size="sm">
            {review.verdict}
          </StatusBadge>
          {review.run_source === "retry" && (
            <span className="px-1.5 py-0.5 rounded bg-primary/10 text-[10px] text-primary">
              retry
            </span>
          )}
        </div>
      </td>
      <td className="px-4 py-3 whitespace-nowrap text-sm font-mono text-white">
        {review.overall_score}/100
      </td>
      <td className="px-4 py-3 text-sm text-gray-300">
        {review.reviewer_model || "reviewer"}
      </td>
      <td className="px-4 py-3">
        {tags.length > 0 ? (
          <div className="flex flex-wrap gap-1">
            {tags.slice(0, 3).map((tag) => (
              <span key={tag} className="px-1.5 py-0.5 rounded bg-gray-800 text-[10px] text-gray-300">
                {tag}
              </span>
            ))}
            {tags.length > 3 && (
              <span className="px-1.5 py-0.5 rounded bg-gray-800 text-[10px] text-gray-500">
                +{tags.length - 3}
              </span>
            )}
          </div>
        ) : (
          <span className="text-xs text-gray-500">—</span>
        )}
      </td>
      <td className="px-4 py-3 text-sm text-gray-300">
        <div className="max-w-[320px] truncate" title={review.goal || review.feedback_text || "—"}>
          {truncate(review.goal || review.feedback_text || "—", 84)}
        </div>
      </td>
    </tr>
  );
}

export function ReviewsPageView({
  reviews,
  loading,
  liveConnected,
  filters,
  projectOptions,
  reviewerOptions,
  sortKey,
  sortDirection,
  onVerdictChange,
  onProjectChange,
  onReviewerModelChange,
  onSearchChange,
  onTimeRangeChange,
  onSortChange,
  onRowClick,
}: ReviewsPageViewProps) {
  const filteredReviews = useMemo(
    () => filterAndSortReviews(reviews, filters, sortKey, sortDirection),
    [reviews, filters, sortKey, sortDirection],
  );
  const verdictOptions: Array<{ value: ReviewVerdictFilter; label: string }> = [
    { value: "all", label: "All" },
    { value: "pass", label: "Pass" },
    { value: "partial", label: "Partial" },
    { value: "fail", label: "Fail" },
  ];

  const emptyMessage = reviews.length === 0
    ? "目前還沒有 review 結果"
    : "沒有符合條件的 review";

  const goToTimeline = (taskId: string) => {
    onRowClick(taskId);
  };

  return (
    <div className="space-y-6 animate-fade-in">
      <div className="flex items-start justify-between gap-4 flex-wrap">
        <div className="space-y-2">
          <div className="flex items-center gap-2 text-gray-400 text-sm">
            <Search className="w-4 h-4 text-accent" />
            <span>Review History</span>
            <StatusBadge variant={liveConnected ? "success" : "neutral"} size="sm" dot>
              {liveConnected ? "Live" : "歷史資料"}
            </StatusBadge>
          </div>
          <h1 className="text-2xl font-semibold tracking-tight text-white">Review History</h1>
          <p className="text-sm text-gray-500">
            集中查看所有 review，依 verdict、project、時間與 reviewer model 篩選。
          </p>
        </div>
        <div className="text-xs text-gray-500 font-mono">
          {filteredReviews.length} / {reviews.length} reviews
        </div>
      </div>

      <ReviewSummaryPanel
        reviews={filteredReviews}
        liveConnected={liveConnected}
        emptyMessage={emptyMessage}
      />

      <div className="card p-4 space-y-4">
        <div className="flex flex-wrap items-start gap-4 justify-between">
          <div className="space-y-3">
            <div className="flex flex-wrap items-center gap-3">
              <div className="flex items-center gap-2 text-sm text-gray-400">
                <Clock className="w-4 h-4" />
                <span>時間範圍</span>
              </div>
              <DateRangeFilter onChange={onTimeRangeChange} />
            </div>
            <SearchFilter
              placeholder="搜尋 goal 或 feedback_text"
              onSearch={onSearchChange}
            />
          </div>

          <div className="flex flex-wrap items-end gap-3">
            <label className="space-y-1">
              <span className="block text-[11px] uppercase tracking-wide text-gray-500">Verdict</span>
              <select
                value={filters.verdict}
                onChange={(e) => onVerdictChange(e.target.value as ReviewVerdictFilter)}
                className="bg-gray-900 border border-gray-700 rounded-lg px-3 py-2 text-sm text-gray-200 focus:outline-none focus:border-primary/50"
              >
                {verdictOptions.map((opt) => (
                  <option key={opt.value} value={opt.value}>{opt.label}</option>
                ))}
              </select>
            </label>

            <label className="space-y-1">
              <span className="block text-[11px] uppercase tracking-wide text-gray-500">Project</span>
              <select
                value={filters.projectPath}
                onChange={(e) => onProjectChange(e.target.value)}
                className="bg-gray-900 border border-gray-700 rounded-lg px-3 py-2 text-sm text-gray-200 focus:outline-none focus:border-primary/50 min-w-[180px]"
              >
                <option value="all">All</option>
                {projectOptions.map((project) => (
                  <option key={project} value={project}>
                    {project}
                  </option>
                ))}
              </select>
            </label>

            <label className="space-y-1">
              <span className="block text-[11px] uppercase tracking-wide text-gray-500">Reviewer model</span>
              <select
                value={filters.reviewerModel}
                onChange={(e) => onReviewerModelChange(e.target.value)}
                className="bg-gray-900 border border-gray-700 rounded-lg px-3 py-2 text-sm text-gray-200 focus:outline-none focus:border-primary/50 min-w-[180px]"
              >
                <option value="all">All</option>
                {reviewerOptions.map((model) => (
                  <option key={model} value={model}>
                    {model}
                  </option>
                ))}
              </select>
            </label>
          </div>
        </div>

        <div className="overflow-x-auto rounded-lg border border-gray-800/60">
          <table className="w-full border-collapse text-sm">
            <thead className="bg-black/20">
              <tr className="text-gray-500 text-left">
                <th className="px-4 py-3 whitespace-nowrap">
                  <SortButton
                    active={sortKey === "timestamp"}
                    direction={sortDirection}
                    onClick={() => onSortChange("timestamp")}
                  >
                    時間
                  </SortButton>
                </th>
                <th className="px-4 py-3 whitespace-nowrap">
                  <SortButton
                    active={sortKey === "project_path"}
                    direction={sortDirection}
                    onClick={() => onSortChange("project_path")}
                  >
                    project
                  </SortButton>
                </th>
                <th className="px-4 py-3 whitespace-nowrap">
                  <SortButton
                    active={sortKey === "verdict"}
                    direction={sortDirection}
                    onClick={() => onSortChange("verdict")}
                  >
                    verdict
                  </SortButton>
                </th>
                <th className="px-4 py-3 whitespace-nowrap">
                  <SortButton
                    active={sortKey === "overall_score"}
                    direction={sortDirection}
                    onClick={() => onSortChange("overall_score")}
                  >
                    score
                  </SortButton>
                </th>
                <th className="px-4 py-3 whitespace-nowrap">
                  <SortButton
                    active={sortKey === "reviewer_model"}
                    direction={sortDirection}
                    onClick={() => onSortChange("reviewer_model")}
                  >
                    reviewer model
                  </SortButton>
                </th>
                <th className="px-4 py-3 whitespace-nowrap">
                  <SortButton
                    active={sortKey === "issue_tags"}
                    direction={sortDirection}
                    onClick={() => onSortChange("issue_tags")}
                  >
                    tags
                  </SortButton>
                </th>
                <th className="px-4 py-3 whitespace-nowrap">
                  <SortButton
                    active={sortKey === "goal"}
                    direction={sortDirection}
                    onClick={() => onSortChange("goal")}
                  >
                    goal 摘要
                  </SortButton>
                </th>
              </tr>
            </thead>
            <tbody>
              {loading ? (
                <tr>
                  <td colSpan={7} className="px-4 py-14 text-center text-gray-500">
                    <Loader2 className="w-6 h-6 mx-auto mb-2 animate-spin text-primary" />
                    Loading reviews...
                  </td>
                </tr>
              ) : filteredReviews.length === 0 ? (
                <tr>
                  <td colSpan={7} className="px-4 py-14 text-center text-gray-500">
                    <div className="space-y-2">
                      <p className="text-base text-gray-300">{emptyMessage}</p>
                      <p className="text-sm text-gray-600">
                        可調整 verdict、project、時間範圍或搜尋條件來縮小結果。
                      </p>
                    </div>
                  </td>
                </tr>
              ) : (
                filteredReviews.map((review) => (
                  <ReviewRow key={review.key} review={review} onClick={goToTimeline} />
                ))
              )}
            </tbody>
          </table>
        </div>
        <div className="text-xs text-gray-500">
          預設依時間由新到舊排序。點列會跳到 Timeline 並展開對應 decision。
        </div>
      </div>
    </div>
  );
}

function toAllTimeRange(): DateRange {
  return {};
}

export default function Reviews() {
  const { decisions, reviewEvents, wsConnected } = useAppStore();
  const navigate = useNavigate();
  const [apiDecisions, setApiDecisions] = useState<DecisionLog[]>([]);
  const [loading, setLoading] = useState(true);
  const [filters, setFilters] = useState<ReviewFilters>({
    verdict: "all",
    projectPath: "all",
    reviewerModel: "all",
    search: "",
    timeRange: toAllTimeRange(),
  });
  const [sortKey, setSortKey] = useState<ReviewSortKey>("timestamp");
  const [sortDirection, setSortDirection] = useState<SortDirection>("desc");

  useEffect(() => {
    let cancelled = false;

    const load = async () => {
      try {
        const now = new Date();
        const res = await api.getTaskDecisions({
          limit: 2000,
          startTime: "2020-01-01T00:00:00Z",
          endTime: now.toISOString(),
        });

        if (!cancelled) {
          setApiDecisions(res.decisions || []);
        }
      } catch {
        if (!cancelled) setApiDecisions([]);
      } finally {
        if (!cancelled) setLoading(false);
      }
    };

    load();
    const interval = setInterval(load, 60000);

    return () => {
      cancelled = true;
      clearInterval(interval);
    };
  }, []);

  const allDecisions = useMemo(() => {
    const seen = new Set<string>();
    const merged: DecisionLog[] = [];
    for (const d of [...decisions, ...apiDecisions]) {
      if (!seen.has(d.id)) {
        seen.add(d.id);
        merged.push(d);
      }
    }
    return merged;
  }, [decisions, apiDecisions]);

  const reviewItems = useMemo(() => {
    const decisionById = new Map(allDecisions.map((decision) => [decision.id, decision] as const));
    const storedReviews = allDecisions.flatMap((decision) =>
      (decision.unified_task?.reviews || []).map((review) => normalizeStoredReview(review, decision))
    );
    const liveReviews = reviewEvents.map((review) =>
      normalizeLiveReview(review, decisionById.get(review.task_id))
    );

    return mergeReviewItems([...storedReviews, ...liveReviews]);
  }, [allDecisions, reviewEvents]);

  const projectOptions = useMemo(() => {
    return Array.from(new Set(reviewItems.map((review) => review.project_path).filter(Boolean))).sort();
  }, [reviewItems]);

  const reviewerOptions = useMemo(() => {
    return Array.from(new Set(reviewItems.map((review) => review.reviewer_model).filter(Boolean))).sort();
  }, [reviewItems]);

  const handleSortChange = (key: ReviewSortKey) => {
    setSortDirection((currentDirection) =>
      sortKey === key ? (currentDirection === "asc" ? "desc" : "asc") : "desc",
    );
    setSortKey(key);
  };

  return (
    <ReviewsPageView
      reviews={reviewItems}
      loading={loading}
      liveConnected={wsConnected}
      filters={filters}
      projectOptions={projectOptions}
      reviewerOptions={reviewerOptions}
      sortKey={sortKey}
      sortDirection={sortDirection}
      onVerdictChange={(verdict) => setFilters((current) => ({ ...current, verdict }))}
      onProjectChange={(projectPath) => setFilters((current) => ({ ...current, projectPath }))}
      onReviewerModelChange={(reviewerModel) => setFilters((current) => ({ ...current, reviewerModel }))}
      onSearchChange={(search) => setFilters((current) => ({ ...current, search }))}
      onTimeRangeChange={(timeRange) => setFilters((current) => ({ ...current, timeRange }))}
      onSortChange={handleSortChange}
      onRowClick={(taskId) => {
        navigate(buildTimelineFocusPath(taskId));
      }}
    />
  );
}
