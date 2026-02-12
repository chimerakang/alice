import { useEffect, useState, useMemo, useCallback } from "react";
import { api } from "@/lib/api";
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
} from "lucide-react";

const PAGE_SIZE = 15;

const STATUS_OPTIONS = [
  { value: "all", label: "All" },
  { value: "success", label: "Success" },
  { value: "error", label: "Has Errors" },
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
  return date.toLocaleString("zh-TW", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}

/** Slide-over panel showing full decision detail with Gantt, Diff, and navigation */
function DecisionDetail({
  decision,
  decisions,
  onClose,
  onNavigate,
}: {
  decision: DecisionLog;
  decisions: DecisionLog[];
  onClose: () => void;
  onNavigate: (d: DecisionLog) => void;
}) {
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
              title="Previous decision"
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
              title="Next decision"
            >
              <ChevronRight className="w-4 h-4 text-gray-400" />
            </button>
          </div>

          <h2 className="text-lg font-semibold text-white flex-1">
            Decision Detail
          </h2>
          <StatusBadge
            variant={!hasError ? "success" : "error"}
            dot
          >
            {!hasError ? "Success" : "Has Errors"}
          </StatusBadge>
        </div>

        <div className="p-6 space-y-6">
          {/* Meta info */}
          <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
            <div className="bg-gray-900 rounded-lg p-3">
              <div className="text-xs text-gray-500 mb-1">Time</div>
              <div className="text-sm text-white font-mono">
                {formatTimestamp(decision.timestamp)}
              </div>
            </div>
            <div className="bg-gray-900 rounded-lg p-3">
              <div className="text-xs text-gray-500 mb-1">Duration</div>
              <div className="text-sm text-white font-mono">
                {decision.duration_ms > 0 ? formatDuration(decision.duration_ms) : "—"}
              </div>
            </div>
            <div className="bg-gray-900 rounded-lg p-3">
              <div className="text-xs text-gray-500 mb-1">Tokens</div>
              <div className="text-sm text-white font-mono">
                {decision.tokens_input + decision.tokens_output > 0
                  ? `${((decision.tokens_input + decision.tokens_output) / 1000).toFixed(1)}k`
                  : "—"}
              </div>
            </div>
            <div className="bg-gray-900 rounded-lg p-3">
              <div className="text-xs text-gray-500 mb-1">Tools</div>
              <div className="text-sm text-white font-mono">{toolCount}</div>
            </div>
          </div>

          {/* Token breakdown */}
          {(decision.tokens_input > 0 || decision.tokens_output > 0) && (
            <div className="flex gap-4 text-xs text-gray-500">
              <span>Input: {decision.tokens_input.toLocaleString()} tokens</span>
              <span>Output: {decision.tokens_output.toLocaleString()} tokens</span>
            </div>
          )}

          {/* User Prompt */}
          <CollapsiblePanel
            title="User Prompt"
            defaultOpen
            badge={
              <MessageSquare className="w-3.5 h-3.5 text-primary" />
            }
          >
            <p className="text-sm text-gray-300 whitespace-pre-wrap leading-relaxed">
              {decision.user_prompt || "—"}
            </p>
          </CollapsiblePanel>

          {/* AI Thinking */}
          {decision.thinking_content && (
            <CollapsiblePanel
              title="AI Thinking"
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
            title="AI Response"
            defaultOpen
            badge={
              <Bot className="w-3.5 h-3.5 text-accent" />
            }
          >
            {decision.agent_response ? (
              <MarkdownRenderer content={decision.agent_response} />
            ) : (
              <p className="text-sm text-gray-500">No response recorded</p>
            )}
          </CollapsiblePanel>

          {/* Tool Call Gantt Timeline */}
          {toolCount > 1 && (
            <CollapsiblePanel
              title="Tool Execution Timeline"
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
              title={`Tool Calls (${toolCount})`}
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
                          String(tool.status) === "STATUS_SUCCESS" || String(tool.status) === "2"
                            ? "success"
                            : String(tool.status) === "STATUS_ERROR" || String(tool.status) === "4"
                              ? "error"
                              : "warning"
                        }
                        size="sm"
                      >
                        {String(tool.status) === "STATUS_SUCCESS" || String(tool.status) === "2"
                          ? "OK"
                          : String(tool.status) === "STATUS_ERROR" || String(tool.status) === "4"
                            ? "Error"
                            : "Pending"}
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
              title="Git Changes"
              defaultOpen={diffFiles.length > 0}
              badge={
                <GitBranch className="w-3.5 h-3.5 text-info" />
              }
            >
              {diffLoading ? (
                <div className="flex items-center gap-2 text-xs text-gray-500">
                  <Loader2 className="w-3 h-3 animate-spin" /> Loading diff...
                </div>
              ) : (
                <>
                  <div className="text-xs font-mono space-y-1 mb-3">
                    <div className="text-gray-400">
                      Branch: <span className="text-primary-light">{decision.git_state.branch}</span>
                    </div>
                    <div className="text-gray-400">
                      Commit: <span className="text-primary-light">{decision.git_state.commit_hash?.slice(0, 8)}</span>
                    </div>
                    {decision.git_state.is_dirty && (
                      <div className="text-warning">Working tree has uncommitted changes</div>
                    )}
                  </div>
                  <DiffViewer files={diffFiles} />
                </>
              )}
            </CollapsiblePanel>
          )}

          {/* Git State (fallback when no commit hash for diff) */}
          {decision.git_state && !decision.git_state.commit_hash && (
            <CollapsiblePanel title="Git State">
              <div className="text-xs font-mono space-y-1">
                <div className="text-gray-400">
                  Branch: <span className="text-primary-light">{decision.git_state.branch}</span>
                </div>
                {decision.git_state.is_dirty && (
                  <div className="text-warning">Working tree has uncommitted changes</div>
                )}
                {decision.git_state.modified_files?.length > 0 && (
                  <div className="mt-2">
                    <div className="text-gray-500 mb-1">Modified files:</div>
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
              <div>Session: {decision.session_id}</div>
            )}
            {decision.project_path && (
              <div>Project: {decision.project_path}</div>
            )}
            {decision.id && <div>ID: {decision.id}</div>}
          </div>
        </div>
      </div>
    </div>
  );
}

export default function Timeline() {
  const { decisions: liveDecisions, wsConnected } = useAppStore();
  const [apiDecisions, setApiDecisions] = useState<DecisionLog[]>([]);
  const [loading, setLoading] = useState(true);
  const [searchQuery, setSearchQuery] = useState("");
  const [statusFilter, setStatusFilter] = useState("all");
  const [projectFilter, setProjectFilter] = useState("all");
  const [page, setPage] = useState(0);
  const [totalCount, setTotalCount] = useState(0);
  const [dateRange, setDateRange] = useState<DateRange>({});
  const [selectedDecision, setSelectedDecision] = useState<DecisionLog | null>(null);

  // Fetch decisions from API with server-side pagination + time range
  const fetchDecisions = useCallback(async (pageNum: number, range: DateRange) => {
    setLoading(true);
    try {
      const params: TimeRangeQuery = {
        limit: PAGE_SIZE,
        offset: pageNum * PAGE_SIZE,
        startTime: range.startTime,
        endTime: range.endTime,
      };
      const res = await api.getRecentDecisions(params);
      setApiDecisions(res.decisions || []);
      setTotalCount(res.pagination?.total_count ?? (res.decisions?.length || 0));
    } catch {
      // API not available — keep what we have
    } finally {
      setLoading(false);
    }
  }, []);

  // Initial load + refetch when page or date range changes
  useEffect(() => {
    fetchDecisions(page, dateRange);
  }, [page, dateRange, fetchDecisions]);

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

  // Client-side filter (search, status, project) on current page data
  const filtered = useMemo(() => {
    return displayDecisions.filter((d) => {
      if (projectFilter !== "all" && d.project_path !== projectFilter) return false;

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
  }, [displayDecisions, projectFilter, statusFilter, searchQuery]);

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
        <h2 className="text-lg font-semibold text-white">
          AI Decision Timeline
        </h2>
        <div className="flex items-center gap-2 text-sm text-gray-400">
          <span
            className={`status-dot ${
              wsConnected ? "status-dot-success" : "status-dot-error"
            }`}
          />
          {wsConnected ? "Live" : "Disconnected"}
          <span className="text-gray-600">|</span>
          <span>{totalCount} decisions</span>
        </div>
      </div>

      {/* Date Range Filter */}
      <DateRangeFilter onChange={handleDateRangeChange} />

      {/* Search & Filter */}
      <div className="space-y-3">
        <SearchFilter
          placeholder="Search prompts, responses, tools..."
          onSearch={handleSearch}
          statusOptions={STATUS_OPTIONS}
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
                All Projects
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
              ? "No decisions yet"
              : "No decisions match your filter"}
          </p>
          <p className="text-gray-600 text-sm mt-1">
            {totalCount === 0
              ? "Decisions will appear here as the AI agent processes tasks."
              : "Try adjusting your search or filter criteria."}
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
                {totalCount} decision{totalCount !== 1 ? "s" : ""}{" "}
                · Page {page + 1} of {totalPages}
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
          decision={selectedDecision}
          decisions={filtered}
          onClose={() => setSelectedDecision(null)}
          onNavigate={setSelectedDecision}
        />
      )}
    </div>
  );
}
