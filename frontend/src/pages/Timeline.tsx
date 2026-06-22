import { useEffect, useState, useMemo, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate, useSearchParams } from "react-router-dom";
import { api } from "@/lib/api";
import i18n from "@/i18n";
import type { TimeRangeQuery } from "@/lib/api";
import { useAppStore } from "@/stores/appStore";
import type { DecisionLog, DiffFile } from "@/types/alice";
import SearchFilter from "@/components/SearchFilter";
import DateRangeFilter from "@/components/DateRangeFilter";
import type { DateRange } from "@/components/DateRangeFilter";
import TimelineEntry from "@/components/TimelineEntry";
import CollapsiblePanel from "@/components/CollapsiblePanel";
import StatusBadge from "@/components/StatusBadge";
import MarkdownRenderer from "@/components/MarkdownRenderer";
import DiffViewer from "@/components/DiffViewer";
import ToolCallGantt from "@/components/ToolCallGantt";
import ReviewSubTaskTable from "@/components/ReviewSubTaskTable";
import {
  X,
  Clock,
  Terminal,
  MessageSquare,
  Bot,
  Brain,
  ChevronLeft,
  ChevronRight,
  Loader2,
  FolderOpen,
  GitBranch,
  BarChart3,
  Monitor,
  Code2,
  Send,
} from "lucide-react";

const PAGE_SIZE = 15;

const STATUS_OPTIONS = [
  { value: "all", labelKey: "common.all" },
  { value: "success", labelKey: "timeline.filters.success" },
  { value: "error", labelKey: "timeline.filters.has_errors" },
];

const SOURCE_OPTIONS = [
  { value: "all", labelKey: "timeline.sources.all", icon: null },
  { value: "telegram", labelKey: "timeline.sources.telegram", icon: Send },
  { value: "terminal", labelKey: "timeline.sources.terminal", icon: Monitor },
  { value: "vscode", labelKey: "timeline.sources.vscode", icon: Code2 },
];

function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`;
  return `${(ms / 60000).toFixed(1)}m`;
}

function formatTimestamp(ts: string | { seconds: number; nanos?: number }): string {
  let date: Date;
  if (typeof ts === "string") {
    date = new Date(ts);
  } else if (ts && typeof ts === "object" && "seconds" in ts) {
    date = new Date(ts.seconds * 1000);
  } else {
    return "—";
  }
  const locale = i18n.language === "zh-TW" ? "zh-TW" : "en-US";
  return date.toLocaleString(locale, {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}

export function findFocusedDecision(
  decisions: DecisionLog[],
  focusTaskId: string
): DecisionLog | null {
  const normalizedFocusTaskId = focusTaskId.trim();
  if (!normalizedFocusTaskId) return null;

  return decisions.find((decision) => decision.id === normalizedFocusTaskId) || null;
}

/** Slide-over panel showing full decision detail with Gantt, Diff, and navigation */
export function DecisionDetail({
  decision,
  decisions,
  onClose,
  onNavigate,
  openReviewsByDefault = false,
}: {
  decision: DecisionLog;
  decisions: DecisionLog[];
  onClose: () => void;
  onNavigate: (d: DecisionLog) => void;
  openReviewsByDefault?: boolean;
}) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [diffFiles, setDiffFiles] = useState<DiffFile[]>([]);
  const [diffLoading, setDiffLoading] = useState(false);

  const toolCount = decision.tool_calls?.length || 0;
  const hasError = decision.tool_calls?.some(
    (t) => String(t.status) === "STATUS_ERROR" || String(t.status) === "4"
  );

  // Find prev/next in the decisions list
  const currentIdx = decisions.findIndex((d) => d.id === decision.id);
  const prevDecision = currentIdx > 0 ? decisions[currentIdx - 1] : null;
  const nextDecision = currentIdx < decisions.length - 1 ? decisions[currentIdx + 1] : null;
  const reviewExpectedSubTaskIds = (decision.unified_task?.sub_tasks || []).map((subTask) => subTask.id);

  // Load git diff when decision has a commit hash
  useEffect(() => {
    const commitHash = decision.git_state?.commit_hash;
    const projectPath = decision.project_path;
    if (!commitHash || !projectPath) {
      setDiffFiles([]);
      return;
    }

    let cancelled = false;
    setDiffLoading(true);
    api
      .getGitDiff({ projectDir: projectPath, commit: commitHash })
      .then((res) => {
        if (!cancelled) setDiffFiles(res.files || []);
      })
      .catch(() => {
        if (!cancelled) setDiffFiles([]);
      })
      .finally(() => {
        if (!cancelled) setDiffLoading(false);
      });

    return () => { cancelled = true; };
  }, [decision.git_state?.commit_hash, decision.project_path]);

  return (
    <div className="fixed inset-0 z-50 flex justify-end">
      {/* Backdrop */}
      <div className="absolute inset-0 bg-black/60" onClick={onClose} />

      {/* Panel */}
      <div className="relative w-full max-w-2xl bg-dark-grey border-l border-gray-800 overflow-y-auto animate-slide-up">
        {/* Header */}
        <div className="sticky top-0 z-10 bg-dark-grey/95 backdrop-blur border-b border-gray-800 px-6 py-4 flex items-center gap-3">
          <button
            onClick={onClose}
            className="p-1 hover:bg-gray-800 rounded transition-colors"
          >
            <X className="w-5 h-5 text-gray-400" />
          </button>

          {/* Prev / Next navigation */}
          <div className="flex items-center gap-1">
            <button
              onClick={() => prevDecision && onNavigate(prevDecision)}
              disabled={!prevDecision}
              className="p-1 hover:bg-gray-800 rounded disabled:opacity-30 disabled:cursor-not-allowed transition-colors"
              title={t("timeline.detail.previous")}
            >
              <ChevronLeft className="w-4 h-4 text-gray-400" />
            </button>
            <span className="text-xs text-gray-500 tabular-nums min-w-[3rem] text-center">
              {currentIdx >= 0 ? `${currentIdx + 1}/${decisions.length}` : ""}
            </span>
            <button
              onClick={() => nextDecision && onNavigate(nextDecision)}
              disabled={!nextDecision}
              className="p-1 hover:bg-gray-800 rounded disabled:opacity-30 disabled:cursor-not-allowed transition-colors"
              title={t("timeline.detail.next")}
            >
              <ChevronRight className="w-4 h-4 text-gray-400" />
            </button>
          </div>

          <h2 className="text-lg font-semibold text-white flex-1">
            {t("timeline.detail.title")}
          </h2>
          <StatusBadge
            variant={!hasError ? "success" : "error"}
            dot
          >
            {!hasError ? t("timeline.detail.success") : t("timeline.detail.has_errors")}
          </StatusBadge>
          <button
            type="button"
            onClick={() => navigate(`/run-inspector?task_id=${encodeURIComponent(decision.id)}`)}
            className="btn btn-secondary text-xs inline-flex items-center gap-1"
          >
            {t("hermes_tasks.open_inspector")}
          </button>
        </div>

        <div className="p-6 space-y-6">
          {/* Meta info */}
          <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
            <div className="bg-gray-900 rounded-lg p-3">
              <div className="text-xs text-gray-500 mb-1">{t("timeline.detail.time")}</div>
              <div className="text-sm text-white font-mono">
                {formatTimestamp(decision.timestamp)}
              </div>
            </div>
            <div className="bg-gray-900 rounded-lg p-3">
              <div className="text-xs text-gray-500 mb-1">{t("timeline.detail.duration")}</div>
              <div className="text-sm text-white font-mono">
                {decision.duration_ms > 0 ? formatDuration(decision.duration_ms) : "—"}
              </div>
            </div>
            <div className="bg-gray-900 rounded-lg p-3">
              <div className="text-xs text-gray-500 mb-1">{t("timeline.detail.tokens")}</div>
              <div className="text-sm text-white font-mono">
                {decision.tokens_input + decision.tokens_output > 0
                  ? `${((decision.tokens_input + decision.tokens_output) / 1000).toFixed(1)}k`
                  : "—"}
              </div>
            </div>
            <div className="bg-gray-900 rounded-lg p-3">
              <div className="text-xs text-gray-500 mb-1">{t("timeline.detail.tools")}</div>
              <div className="text-sm text-white font-mono">{toolCount}</div>
            </div>
          </div>

          {/* Token breakdown */}
          {(decision.tokens_input > 0 || decision.tokens_output > 0) && (
            <div className="flex gap-4 text-xs text-gray-500">
              <span>{t("timeline.detail.input_tokens", { count: decision.tokens_input.toLocaleString() })}</span>
              <span>{t("timeline.detail.output_tokens", { count: decision.tokens_output.toLocaleString() })}</span>
            </div>
          )}

          {/* User Prompt */}
          <CollapsiblePanel
            title={t("timeline.detail.user_prompt")}
            defaultOpen
            badge={
              <MessageSquare className="w-3.5 h-3.5 text-primary" />
            }
          >
            <p className="text-sm text-gray-300 whitespace-pre-wrap leading-relaxed">
              {decision.user_prompt || t("timeline.detail.no_prompt")}
            </p>
          </CollapsiblePanel>

          {/* AI Thinking */}
          {decision.thinking_content && (
            <CollapsiblePanel
              title={t("timeline.detail.ai_thinking")}
              defaultOpen={false}
              badge={
                <Brain className="w-3.5 h-3.5 text-purple-400" />
              }
            >
              <div className="text-sm text-gray-300 whitespace-pre-wrap leading-relaxed font-mono bg-purple-950/20 border border-purple-900/30 rounded-lg p-4 max-h-96 overflow-y-auto">
                {decision.thinking_content}
              </div>
            </CollapsiblePanel>
          )}

          {/* AI Response */}
          <CollapsiblePanel
            title={t("timeline.detail.ai_response")}
            defaultOpen
            badge={
              <Bot className="w-3.5 h-3.5 text-accent" />
            }
            >
              {decision.agent_response ? (
                <MarkdownRenderer content={decision.agent_response} />
              ) : (
                <p className="text-sm text-gray-500">{t("timeline.detail.no_response")}</p>
            )}
          </CollapsiblePanel>

          {/* Unified task graph */}
          {decision.unified_task && decision.unified_task.sub_tasks?.length > 0 && (
            <CollapsiblePanel
              title={t("timeline.detail.sub_tasks", { count: decision.unified_task.sub_tasks.length })}
              defaultOpen={decision.unified_task.engine === "plan-execute"}
              badge={
                <Brain className="w-3.5 h-3.5 text-primary-light" />
              }
            >
              <div className="space-y-2">
                {decision.unified_task.sub_tasks.map((subTask) => (
                  <div key={subTask.id} className="border border-gray-800/60 rounded-md p-3">
                    <div className="flex items-start gap-2">
                      <span className="text-xs text-gray-500 font-mono tabular-nums">
                        #{subTask.idx + 1}
                      </span>
                      <div className="min-w-0 flex-1">
                        <div className="flex items-center gap-2 mb-1">
                          <p className="text-sm text-white leading-snug">{subTask.description || t("timeline.detail.no_description")}</p>
                          <StatusBadge
                            variant={
                              subTask.status === "done"
                                ? "success"
                                : subTask.status === "failed"
                                  ? "error"
                                  : "warning"
                            }
                            size="sm"
                          >
                            {subTask.status === "done"
                              ? t("timeline.status.done")
                              : subTask.status === "failed"
                                ? t("timeline.status.failed")
                                : t("timeline.detail.pending")}
                          </StatusBadge>
                        </div>
                        <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-gray-500">
                          {subTask.model && <span>{t("timeline.detail.model")}: {subTask.model}</span>}
                          {(subTask.input_tokens > 0 || subTask.output_tokens > 0) && (
                            <span>
                              {t("timeline.detail.tokens_colon")} {(subTask.input_tokens + subTask.output_tokens).toLocaleString()}
                            </span>
                          )}
                          {subTask.routing_reason && <span>{t("timeline.detail.route")}: {subTask.routing_reason}</span>}
                        </div>
                        {subTask.result_text && (
                          <pre className="mt-2 bg-gray-900 border border-gray-800 rounded p-2 text-xs text-gray-300 whitespace-pre-wrap max-h-40 overflow-y-auto">
                            {subTask.result_text}
                          </pre>
                        )}
                        {subTask.artifacts?.length > 0 && (
                          <div className="mt-2 text-xs text-gray-500">
                            <div className="mb-1">{t("timeline.detail.artifacts")}</div>
                            {subTask.artifacts.map((artifact) => (
                              <div key={artifact.id || artifact.path} className="font-mono text-gray-400">
                                {artifact.path}{artifact.hash ? ` · ${artifact.hash.slice(0, 8)}` : ""}
                              </div>
                            ))}
                          </div>
                        )}
                      </div>
                    </div>
                  </div>
                ))}
              </div>
            </CollapsiblePanel>
          )}

          {/* Review results */}
          {decision.unified_task && decision.unified_task.reviews?.length > 0 && (
            <CollapsiblePanel
              title={t("timeline.detail.reviews", { count: decision.unified_task.reviews.length })}
              defaultOpen={openReviewsByDefault}
              badge={
                <BarChart3 className="w-3.5 h-3.5 text-accent" />
              }
            >
              <div className="space-y-2">
                {decision.unified_task.reviews.map((review) => (
                  <div key={review.id || review.created_at} className="border border-gray-800/60 rounded-md p-3">
                    <div className="flex items-center gap-2 mb-2">
                      <StatusBadge
                        variant={review.verdict === "pass" ? "success" : review.verdict === "fail" ? "error" : "warning"}
                        size="sm"
                      >
                        {review.verdict
                          ? t(`reviews.verdicts.${review.verdict}`, { defaultValue: t("common.unknown") })
                          : t("timeline.detail.review")}
                      </StatusBadge>
                      <span className="text-xs text-gray-400">{review.reviewer_model || t("timeline.detail.reviewer")}</span>
                      <span className="text-xs text-gray-500 font-mono ml-auto">{review.overall_score}/100</span>
                    </div>
                    {review.issue_tags?.length > 0 && (
                      <div className="flex flex-wrap gap-1 mb-2">
                        {review.issue_tags.map((tag) => (
                          <span key={tag} className="px-1.5 py-0.5 rounded bg-gray-800 text-[10px] text-gray-400">
                            {tag}
                          </span>
                        ))}
                      </div>
                    )}
                    <p className="text-sm text-gray-300 whitespace-pre-wrap">
                      {review.feedback_text || t("timeline.detail.no_feedback")}
                    </p>
                    <ReviewSubTaskTable
                      subTaskResults={review.sub_task_results || []}
                      expectedSubTaskIds={reviewExpectedSubTaskIds}
                    />
                  </div>
                ))}
              </div>
            </CollapsiblePanel>
          )}

          {/* Tool Call Gantt Timeline */}
          {toolCount > 1 && (
            <CollapsiblePanel
              title={t("timeline.detail.tool_execution_timeline")}
              defaultOpen
              badge={
                <BarChart3 className="w-3.5 h-3.5 text-accent" />
              }
            >
              <ToolCallGantt tools={decision.tool_calls} />
            </CollapsiblePanel>
          )}

          {/* Tool Calls detail */}
          {toolCount > 0 && (
            <CollapsiblePanel
              title={t("timeline.detail.tool_calls", { count: toolCount })}
              defaultOpen={toolCount <= 5}
              badge={
                <Terminal className="w-3.5 h-3.5 text-primary-light" />
              }
            >
              <div className="space-y-2">
                {decision.tool_calls.map((tool, i) => (
                  <div
                    key={tool.id || i}
                    className="border border-gray-800/60 rounded-md"
                  >
                    <div className="flex items-center gap-2 px-3 py-2 text-xs">
                      <Terminal className="w-3 h-3 text-gray-500 shrink-0" />
                      <span className="font-mono text-primary-light flex-1 truncate">
                        {tool.tool_name}
                      </span>
                      <StatusBadge
                        variant={
                          String(tool.status) === "STATUS_SUCCESS" || String(tool.status) === "3"
                            ? "success"
                            : String(tool.status) === "STATUS_ERROR" || String(tool.status) === "4"
                              ? "error"
                              : "warning"
                        }
                        size="sm"
                      >
                        {String(tool.status) === "STATUS_SUCCESS" || String(tool.status) === "3"
                          ? t("timeline.detail.ok")
                          : String(tool.status) === "STATUS_ERROR" || String(tool.status) === "4"
                            ? t("timeline.detail.error")
                            : t("timeline.detail.pending")}
                      </StatusBadge>
                      {tool.duration_ms > 0 && (
                        <span className="text-gray-500 tabular-nums">
                          {formatDuration(tool.duration_ms)}
                        </span>
                      )}
                    </div>
                    {tool.input && (
                      <div className="px-3 pb-2 border-t border-gray-800/40">
                        <pre className="mt-2 bg-gray-900 border border-gray-800 rounded p-2 text-xs font-mono text-gray-400 overflow-x-auto max-h-40">
                          {typeof tool.input === "string"
                            ? tool.input
                            : JSON.stringify(tool.input, null, 2)}
                        </pre>
                      </div>
                    )}
                    {tool.error && (
                      <div className="px-3 pb-2">
                        <pre className="bg-error/5 border border-error/20 rounded p-2 text-xs font-mono text-error overflow-x-auto">
                          {tool.error}
                        </pre>
                      </div>
                    )}
                  </div>
                ))}
              </div>
            </CollapsiblePanel>
          )}

          {/* Git Diff Viewer */}
          {decision.git_state?.commit_hash && (
            <CollapsiblePanel
              title={t("timeline.detail.git_changes")}
              defaultOpen={diffFiles.length > 0}
              badge={
                <GitBranch className="w-3.5 h-3.5 text-info" />
              }
            >
              {diffLoading ? (
                <div className="flex items-center gap-2 text-xs text-gray-500">
                  <Loader2 className="w-3 h-3 animate-spin" /> {t("timeline.detail.loading_diff")}
                </div>
              ) : (
                <>
                  <div className="text-xs font-mono space-y-1 mb-3">
                    <div className="text-gray-400">
                      {t("timeline.detail.branch")}: <span className="text-primary-light">{decision.git_state.branch}</span>
                    </div>
                    <div className="text-gray-400">
                      {t("timeline.detail.commit")}: <span className="text-primary-light">{decision.git_state.commit_hash?.slice(0, 8)}</span>
                    </div>
                    {decision.git_state.is_dirty && (
                      <div className="text-warning">{t("timeline.detail.working_tree_dirty")}</div>
                    )}
                  </div>
                  <DiffViewer files={diffFiles} />
                </>
              )}
            </CollapsiblePanel>
          )}

          {/* Git State (fallback when no commit hash for diff) */}
          {decision.git_state && !decision.git_state.commit_hash && (
            <CollapsiblePanel title={t("timeline.detail.git_state")}>
              <div className="text-xs font-mono space-y-1">
                <div className="text-gray-400">
                  {t("timeline.detail.branch")}: <span className="text-primary-light">{decision.git_state.branch}</span>
                </div>
                {decision.git_state.is_dirty && (
                  <div className="text-warning">{t("timeline.detail.working_tree_dirty")}</div>
                )}
                {decision.git_state.modified_files?.length > 0 && (
                  <div className="mt-2">
                    <div className="text-gray-500 mb-1">{t("timeline.detail.modified_files")}</div>
                    {decision.git_state.modified_files.map((f, i) => (
                      <div key={i} className="text-gray-400 pl-2">
                        {f}
                      </div>
                    ))}
                  </div>
                )}
              </div>
            </CollapsiblePanel>
          )}

          {/* Session metadata */}
          <div className="text-xs text-gray-600 space-y-0.5 pt-2 border-t border-gray-800">
            {decision.session_id && (
              <div>{t("timeline.detail.session")}: {decision.session_id}</div>
            )}
            {decision.project_path && (
              <div>{t("timeline.detail.project")}: {decision.project_path}</div>
            )}
            {decision.id && <div>{t("timeline.detail.id")}: {decision.id}</div>}
          </div>
        </div>
      </div>
    </div>
  );
}

export default function Timeline() {
  const { t } = useTranslation();
  const { decisions: liveDecisions, wsConnected } = useAppStore();
  const [searchParams] = useSearchParams();
  const [apiDecisions, setApiDecisions] = useState<DecisionLog[]>([]);
  const [loading, setLoading] = useState(true);
  const [searchQuery, setSearchQuery] = useState("");
  const [statusFilter, setStatusFilter] = useState("all");
  const [projectFilter, setProjectFilter] = useState("all");
  const [page, setPage] = useState(0);
  const [totalCount, setTotalCount] = useState(0);
  const [dateRange, setDateRange] = useState<DateRange>({});
  const [sourceFilter, setSourceFilter] = useState("all");
  const [selectedDecision, setSelectedDecision] = useState<DecisionLog | null>(null);
  const focusTaskId = searchParams.get("focus") || "";

  // Fetch task graphs from the unified #114 API with server-side pagination + time range.
  const fetchDecisions = useCallback(async (pageNum: number, range: DateRange, src: string) => {
    try {
      const params: TimeRangeQuery = {
        limit: PAGE_SIZE,
        offset: pageNum * PAGE_SIZE,
        startTime: range.startTime,
        endTime: range.endTime,
        source: src !== "all" ? src : undefined,
      };
      const res = await api.getTaskDecisions(params);
      setApiDecisions(res.decisions || []);
      setTotalCount(res.total ?? (res.decisions?.length || 0));
    } catch {
      // API not available — keep what we have
    } finally {
      setLoading(false);
    }
  }, []);

  // Initial load + refetch when page, date range, or source filter changes
  useEffect(() => {
    fetchDecisions(page, dateRange, sourceFilter);
  }, [page, dateRange, sourceFilter, fetchDecisions]);

  // Merge live WS decisions (newest) on top of API decisions when on first page with no date filter
  const displayDecisions = useMemo(() => {
    const isFirstPageNoFilter = page === 0 && !dateRange.startTime && !dateRange.endTime;
    if (!isFirstPageNoFilter || liveDecisions.length === 0) {
      return apiDecisions;
    }

    // Deduplicate: live WS first, then API
    const seen = new Set<string>();
    const merged: DecisionLog[] = [];
    for (const d of liveDecisions) {
      if (d.id && seen.has(d.id)) continue;
      if (d.id) seen.add(d.id);
      merged.push(d);
    }
    for (const d of apiDecisions) {
      if (d.id && seen.has(d.id)) continue;
      if (d.id) seen.add(d.id);
      merged.push(d);
    }
    return merged;
  }, [liveDecisions, apiDecisions, page, dateRange]);

  // Extract unique projects from displayed decisions
  const projects = useMemo(() => {
    const paths = new Set<string>();
    for (const d of displayDecisions) {
      if (d.project_path) paths.add(d.project_path);
    }
    return Array.from(paths).sort();
  }, [displayDecisions]);

  // Client-side filter (search, status, project, source) on current page data
  const filtered = useMemo(() => {
    return displayDecisions.filter((d) => {
      if (projectFilter !== "all" && d.project_path !== projectFilter) return false;

      // Source filter — needed because live WS decisions bypass server-side source filter
      if (sourceFilter !== "all" && d.source !== sourceFilter) return false;

      if (statusFilter === "success") {
        const hasError = d.tool_calls?.some(
          (t) => String(t.status) === "STATUS_ERROR" || String(t.status) === "4"
        );
        if (hasError || d.outcome?.success === false) return false;
      }
      if (statusFilter === "error") {
        const hasError = d.tool_calls?.some(
          (t) => String(t.status) === "STATUS_ERROR" || String(t.status) === "4"
        );
        if (!hasError && d.outcome?.success !== false) return false;
      }

      if (searchQuery) {
        const q = searchQuery.toLowerCase();
        const matchPrompt = d.user_prompt?.toLowerCase().includes(q);
        const matchResponse = d.agent_response?.toLowerCase().includes(q);
        const matchTool = d.tool_calls?.some((t) =>
          t.tool_name?.toLowerCase().includes(q)
        );
        const matchType = d.task_type?.toLowerCase().includes(q);
        if (!matchPrompt && !matchResponse && !matchTool && !matchType) return false;
      }

      return true;
    });
  }, [displayDecisions, projectFilter, statusFilter, searchQuery, sourceFilter]);

  useEffect(() => {
    const match = findFocusedDecision(filtered, focusTaskId);
    if (match) {
      setSelectedDecision(match);
    } else if (focusTaskId) {
      setSelectedDecision(null);
    }
  }, [focusTaskId, filtered]);

  // Pagination — use server-side total_count for page calculation
  const totalPages = Math.max(1, Math.ceil(totalCount / PAGE_SIZE));

  const handleSearch = useCallback((q: string) => {
    setSearchQuery(q);
  }, []);

  const handleStatusChange = useCallback((s: string) => {
    setStatusFilter(s);
  }, []);

  const handleDateRangeChange = useCallback((range: DateRange) => {
    setDateRange(range);
    setPage(0);
  }, []);

  return (
    <div className="space-y-4 animate-fade-in">
      {/* Header */}
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold text-white">{t("timeline.title")}</h2>
        <div className="flex items-center gap-2 text-sm text-gray-400">
          <span
            className={`status-dot ${
              wsConnected ? "status-dot-success" : "status-dot-error"
            }`}
          />
          {wsConnected ? t("common.live") : t("common.disconnected")}
          <span className="text-gray-600">|</span>
          <span>{t("timeline.header.decisions_count", { count: totalCount })}</span>
        </div>
      </div>

      {/* Date Range Filter */}
      <DateRangeFilter onChange={handleDateRangeChange} />

      {/* Search & Filter */}
      <div className="space-y-3">
        <SearchFilter
          placeholder={t("timeline.search_placeholder")}
          onSearch={handleSearch}
          statusOptions={STATUS_OPTIONS.map((option) => ({ value: option.value, label: t(option.labelKey) }))}
          onStatusChange={handleStatusChange}
          activeStatus={statusFilter}
        />

        {/* Project filter */}
        {projects.length > 1 && (
          <div className="flex items-center gap-2">
            <FolderOpen className="w-3.5 h-3.5 text-gray-500" />
            <div className="flex items-center gap-1.5 flex-wrap">
              <button
                onClick={() => { setProjectFilter("all"); }}
                className={`px-2.5 py-1 text-xs rounded-full border transition-colors ${
                  projectFilter === "all"
                    ? "bg-primary/15 text-primary border-primary/30"
                    : "text-gray-400 border-gray-700 hover:border-gray-600 hover:text-gray-300"
                }`}
                >
                {t("timeline.sources.all_projects")}
              </button>
              {projects.map((p) => {
                const name = p.split("/").pop() || p;
                return (
                  <button
                    key={p}
                    onClick={() => { setProjectFilter(p); }}
                    title={p}
                    className={`px-2.5 py-1 text-xs rounded-full border transition-colors ${
                      projectFilter === p
                        ? "bg-primary/15 text-primary border-primary/30"
                        : "text-gray-400 border-gray-700 hover:border-gray-600 hover:text-gray-300"
                    }`}
                  >
                    {name}
                  </button>
                );
              })}
            </div>
          </div>
        )}

        {/* Source filter */}
        <div className="flex items-center gap-2">
          <Monitor className="w-3.5 h-3.5 text-gray-500" />
          <div className="flex items-center gap-1.5 flex-wrap">
            {SOURCE_OPTIONS.map((opt) => (
              <button
                key={opt.value}
                onClick={() => { setSourceFilter(opt.value); setPage(0); }}
                className={`px-2.5 py-1 text-xs rounded-full border transition-colors flex items-center gap-1 ${
                  sourceFilter === opt.value
                    ? "bg-primary/15 text-primary border-primary/30"
                    : "text-gray-400 border-gray-700 hover:border-gray-600 hover:text-gray-300"
                }`}
              >
                {opt.icon && <opt.icon className="w-3 h-3" />}
                {t(opt.labelKey)}
              </button>
            ))}
          </div>
        </div>
      </div>

      {/* Timeline */}
      {loading ? (
        <div className="flex items-center justify-center h-64">
          <Loader2 className="w-8 h-8 text-primary animate-spin" />
        </div>
      ) : filtered.length === 0 ? (
        <div className="card p-12 text-center">
          <Clock className="w-12 h-12 text-gray-600 mx-auto mb-4" />
          <p className="text-gray-400">
            {totalCount === 0
              ? t("timeline.empty.no_decisions")
              : t("timeline.empty.no_match")}
          </p>
          <p className="text-gray-600 text-sm mt-1">
            {totalCount === 0
              ? t("timeline.empty.no_decisions_hint")
              : t("timeline.empty.no_match_hint")}
          </p>
        </div>
      ) : (
        <>
          <div className="relative">
            {filtered.map((d, i) => (
              <TimelineEntry
                key={d.id || `${page}-${i}`}
                decision={d}
                onSelect={setSelectedDecision}
              />
            ))}
          </div>

          {/* Pagination */}
          {totalPages > 1 && (
            <div className="flex items-center justify-between pt-2">
              <span className="text-xs text-gray-500">
                {t("timeline.pagination.summary", { count: totalCount, page: page + 1, totalPages })}
              </span>
              <div className="flex items-center gap-1">
                <button
                  onClick={() => setPage((p) => Math.max(0, p - 1))}
                  disabled={page === 0}
                  className="p-1.5 rounded hover:bg-gray-800 disabled:opacity-30 disabled:cursor-not-allowed transition-colors"
                >
                  <ChevronLeft className="w-4 h-4 text-gray-400" />
                </button>
                <button
                  onClick={() =>
                    setPage((p) => Math.min(totalPages - 1, p + 1))
                  }
                  disabled={page >= totalPages - 1}
                  className="p-1.5 rounded hover:bg-gray-800 disabled:opacity-30 disabled:cursor-not-allowed transition-colors"
                >
                  <ChevronRight className="w-4 h-4 text-gray-400" />
                </button>
              </div>
            </div>
          )}
        </>
      )}

      {/* Decision Detail slide-over */}
      {selectedDecision && (
        <DecisionDetail
          key={selectedDecision.id}
          decision={selectedDecision}
          decisions={filtered}
          onClose={() => setSelectedDecision(null)}
          onNavigate={setSelectedDecision}
          openReviewsByDefault={Boolean(focusTaskId)}
        />
      )}
    </div>
  );
}
