import { useEffect, useState, useMemo, useCallback } from "react";
// import { useNavigate } from "react-router-dom"; // Not needed in this refactor
import { api } from "@/lib/api";
import type { TimeRangeQuery } from "@/lib/api";
import { useAppStore } from "@/stores/appStore";
import type { Checkpoint, DecisionLog, DiffFile } from "@/types/alice";
import SearchFilter from "@/components/SearchFilter";
import DateRangeFilter from "@/components/DateRangeFilter";
import type { DateRange } from "@/components/DateRangeFilter";
// import TimelineEntry from "@/components/TimelineEntry"; // Not used in this refactor
import CollapsiblePanel from "@/components/CollapsiblePanel";
import ConfirmDialog from "@/components/ConfirmDialog";
import StatusBadge from "@/components/StatusBadge";
import MarkdownRenderer from "@/components/MarkdownRenderer";
import DiffViewer from "@/components/DiffViewer";
import ToolCallGantt from "@/components/ToolCallGantt";
import {
  Camera,
  RotateCcw,
  // Plus, // Removed in refactor
  Clock,
  GitBranch,
  HardDrive,
  Loader2,
  FolderOpen,
  Zap,
  User,
  // ChevronDown, // Removed in refactor
  ChevronRight,
  MessageSquare,
  Bot,
  Terminal,
  ExternalLink,
  X,
  Brain,
  ChevronLeft,
  BarChart3,
  Filter,
} from "lucide-react";

// ─── Constants ───────────────────────────────────────

const PAGE_SIZE = 15;

const STATUS_OPTIONS = [
  { value: "all", label: "All" },
  { value: "success", label: "Success" },
  { value: "error", label: "Has Errors" },
  { value: "with_checkpoint", label: "With Checkpoint" },
];

const TRIGGER_TYPE_OPTIONS = [
  { value: "all", label: "All" },
  { value: "manual", label: "Manual" },
  { value: "pre_danger", label: "Pre-Danger" },
  { value: "auto", label: "Auto" },
];

// ─── Helpers ─────────────────────────────────────────

function toMs(ts: string | { seconds: number; nanos?: number }): number {
  if (typeof ts === "string") return new Date(ts).getTime();
  if (ts && typeof ts === "object" && "seconds" in ts) return ts.seconds * 1000;
  return 0;
}

function formatTimestamp(ts: string | { seconds: number; nanos?: number }): string {
  const ms = toMs(ts);
  if (!ms) return "—";
  return new Date(ms).toLocaleString("zh-TW", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}

function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`;
  return `${(ms / 60000).toFixed(1)}m`;
}

function formatSize(bytes: number): string {
  if (bytes === 0) return "—";
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

function triggerIcon(triggerType: string) {
  if (triggerType === "manual" || triggerType === "user") {
    return <User className="w-3 h-3" />;
  }
  return <Zap className="w-3 h-3" />;
}

function triggerLabel(triggerType: string): string {
  switch (triggerType) {
    case "manual":
    case "user":
      return "Manual";
    case "auto":
    case "automatic":
      return "Auto";
    case "pre_danger":
    case "pre_dangerous":
      return "Pre-Danger";
    default:
      return triggerType || "Unknown";
  }
}

function triggerVariant(triggerType: string): "info" | "neutral" | "warning" {
  if (triggerType === "pre_danger" || triggerType === "pre_dangerous") return "warning";
  if (triggerType === "manual" || triggerType === "user") return "info";
  return "neutral";
}

/** Find linked checkpoint for a decision */
function findLinkedCheckpoint(
  decision: DecisionLog,
  checkpoints: Checkpoint[],
): Checkpoint | null {
  // Priority 1: Direct link via decision_log_id
  const direct = checkpoints.find((cp) => cp.decision_log_id === decision.id);
  if (direct) return direct;

  // Priority 2: Fallback to timestamp proximity (for old checkpoints without decision_log_id)
  const dTime = toMs(decision.timestamp);
  if (!dTime) return null;

  let best: Checkpoint | null = null;
  let bestDist = Infinity;

  for (const cp of checkpoints) {
    if (cp.chat_id !== decision.chat_id) continue;

    const cpTime = toMs(cp.timestamp);
    const dist = Math.abs(dTime - cpTime);
    if (dist < 300_000 && dist < bestDist) {
      bestDist = dist;
      best = cp;
    }
  }

  return best;
}

// ─── Decision Entry Card ─────────────────────────────

function DecisionEntryCard({
  decision,
  linkedCheckpoint,
  onViewDetail,
  onRestoreCheckpoint,
}: {
  decision: DecisionLog;
  linkedCheckpoint: Checkpoint | null;
  onViewDetail: () => void;
  onRestoreCheckpoint?: () => void;
}) {
  const [checkpointExpanded] = useState(false);
  const toolCount = decision.tool_calls?.length || 0;
  const hasError = decision.tool_calls?.some(
    (t) => String(t.status) === "STATUS_ERROR" || String(t.status) === "4",
  );

  const successfulTools = decision.tool_calls?.filter(
    (t) => String(t.status) !== "STATUS_ERROR" && String(t.status) !== "4"
  ).length || 0;

  return (
    <div className="card p-4 hover:border-gray-700/60 transition-colors">
      {/* Main content: User prompt → Tool chain → Outcome */}
      <div className="space-y-3">
        {/* User Prompt */}
        <div className="flex items-start gap-3">
          <MessageSquare className="w-4 h-4 text-primary mt-0.5 shrink-0" />
          <div className="min-w-0 flex-1">
            <p className="text-sm text-gray-200 leading-relaxed break-all overflow-hidden">
              {decision.user_prompt?.slice(0, 200) || "No prompt"}
              {(decision.user_prompt?.length || 0) > 200 ? "…" : ""}
            </p>
            <div className="flex items-center gap-3 mt-1.5 text-xs text-gray-500">
              <span className="flex items-center gap-1">
                <Clock className="w-3 h-3" />
                {formatTimestamp(decision.timestamp)}
              </span>
              {decision.project_path && (
                <span className="flex items-center gap-1">
                  <FolderOpen className="w-3 h-3" />
                  {decision.project_path.split("/").pop() || decision.project_path}
                </span>
              )}
            </div>
          </div>
        </div>

        {/* Tool Chain & Outcome */}
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-4">
            {/* Tool summary */}
            <div className="flex items-center gap-2 text-xs">
              <Terminal className="w-3.5 h-3.5 text-gray-500" />
              <span className="text-gray-400">
                {toolCount > 0 ? `${successfulTools}/${toolCount} tools` : "No tools"}
              </span>
              {decision.duration_ms > 0 && (
                <span className="text-gray-600">
                  {formatDuration(decision.duration_ms)}
                </span>
              )}
              {(decision.tokens_input > 0 || decision.tokens_output > 0) && (
                <span className="font-mono text-gray-600">
                  {decision.tokens_input + decision.tokens_output}t
                </span>
              )}
            </div>

            {/* Outcome */}
            <StatusBadge variant={hasError ? "error" : "success"} size="sm" dot>
              {hasError ? "Error" : "Success"}
            </StatusBadge>
          </div>

          {/* Detail button */}
          <button
            onClick={onViewDetail}
            className="flex items-center gap-1.5 px-3 py-1.5 text-xs text-primary border border-primary/30 rounded-lg hover:bg-primary/10 transition-colors"
          >
            <ExternalLink className="w-3 h-3" />
            Detail
          </button>
        </div>

        {/* Checkpoint attachment (if exists) */}
        {linkedCheckpoint && (
          <CollapsiblePanel
            title={
              <div className="flex items-center gap-2">
                <Camera className="w-3.5 h-3.5 text-accent" />
                <span className="text-sm font-medium text-gray-300">Safety Checkpoint</span>
                <StatusBadge variant={triggerVariant(linkedCheckpoint.trigger_type)} size="sm">
                  {triggerIcon(linkedCheckpoint.trigger_type)}
                  {triggerLabel(linkedCheckpoint.trigger_type)}
                </StatusBadge>
              </div>
            }
            defaultOpen={checkpointExpanded}
            className="border-t border-gray-800/60 pt-3 mt-3"
          >
            <div className="space-y-2 text-xs">
              {/* Description */}
              <p className="text-gray-300 leading-relaxed">
                {linkedCheckpoint.description || "Untitled checkpoint"}
              </p>

              {/* Dangerous operation */}
              {linkedCheckpoint.dangerous_op && (
                <div className="flex items-center gap-1.5 text-warning/80">
                  <Zap className="w-3 h-3" />
                  <span className="font-mono">{linkedCheckpoint.dangerous_op}</span>
                </div>
              )}

              {/* Git + storage info */}
              <div className="flex items-center gap-4 text-gray-500">
                {linkedCheckpoint.git_branch && (
                  <span className="flex items-center gap-1">
                    <GitBranch className="w-3 h-3" />
                    <span className="font-mono">{linkedCheckpoint.git_branch}</span>
                  </span>
                )}
                {linkedCheckpoint.git_commit_hash && (
                  <span className="font-mono">
                    {linkedCheckpoint.git_commit_hash.slice(0, 8)}
                  </span>
                )}
                {linkedCheckpoint.size > 0 && (
                  <span className="flex items-center gap-1">
                    <HardDrive className="w-3 h-3" />
                    {formatSize(linkedCheckpoint.size)}
                  </span>
                )}
                {onRestoreCheckpoint && !linkedCheckpoint.is_active && (
                  <button
                    onClick={onRestoreCheckpoint}
                    className="ml-auto flex items-center gap-1 px-2 py-1 text-warning border border-warning/30 rounded hover:bg-warning/10 transition-colors"
                  >
                    <RotateCcw className="w-3 h-3" />
                    Restore
                  </button>
                )}
              </div>
            </div>
          </CollapsiblePanel>
        )}
      </div>
    </div>
  );
}

// ─── Decision Detail Panel ───────────────────────────

function DecisionDetailPanel({
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
        if (!cancelled && res.files) setDiffFiles(res.files);
      })
      .catch(() => {
        if (!cancelled) setDiffFiles([]);
      })
      .finally(() => {
        if (!cancelled) setDiffLoading(false);
      });

    return () => {
      cancelled = true;
    };
  }, [decision.git_state?.commit_hash, decision.project_path]);

  return (
    <div className="fixed inset-y-0 right-0 w-2/3 bg-gray-950 border-l border-gray-800 z-50 overflow-hidden flex flex-col">
      {/* Header */}
      <div className="flex items-center justify-between p-4 border-b border-gray-800">
        <div className="flex items-center gap-3">
          <h3 className="text-lg font-semibold text-white">Decision Detail</h3>
          <StatusBadge variant={hasError ? "error" : "success"} size="sm" dot>
            {hasError ? "Error" : "Success"}
          </StatusBadge>
        </div>
        <div className="flex items-center gap-2">
          {/* Navigation */}
          <button
            onClick={() => prevDecision && onNavigate(prevDecision)}
            disabled={!prevDecision}
            className="p-2 text-gray-400 hover:text-gray-300 disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
            title="Previous decision"
          >
            <ChevronLeft className="w-4 h-4" />
          </button>
          <button
            onClick={() => nextDecision && onNavigate(nextDecision)}
            disabled={!nextDecision}
            className="p-2 text-gray-400 hover:text-gray-300 disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
            title="Next decision"
          >
            <ChevronRight className="w-4 h-4" />
          </button>
          <button
            onClick={onClose}
            className="p-2 text-gray-400 hover:text-gray-300 transition-colors"
          >
            <X className="w-4 h-4" />
          </button>
        </div>
      </div>

      {/* Content */}
      <div className="flex-1 overflow-y-auto p-4 space-y-6">
        {/* Meta info */}
        <div className="grid grid-cols-2 gap-4 text-sm">
          <div>
            <span className="text-gray-500">Timestamp:</span>
            <span className="ml-2 font-mono">{formatTimestamp(decision.timestamp)}</span>
          </div>
          <div>
            <span className="text-gray-500">Duration:</span>
            <span className="ml-2">{formatDuration(decision.duration_ms)}</span>
          </div>
          <div>
            <span className="text-gray-500">Tokens:</span>
            <span className="ml-2 font-mono">
              {decision.tokens_input}↗ + {decision.tokens_output}↙ = {decision.tokens_input + decision.tokens_output}
            </span>
          </div>
          <div>
            <span className="text-gray-500">Tools:</span>
            <span className="ml-2">{toolCount} executed</span>
          </div>
        </div>

        {/* User Prompt */}
        <div>
          <h4 className="text-sm font-semibold text-gray-300 mb-2 flex items-center gap-2">
            <MessageSquare className="w-4 h-4 text-primary" />
            User Prompt
          </h4>
          <div className="bg-gray-900 rounded-lg p-3 text-sm text-gray-200">
            {decision.user_prompt || "No prompt"}
          </div>
        </div>

        {/* AI Thinking */}
        {decision.thinking_content && (
          <div>
            <h4 className="text-sm font-semibold text-gray-300 mb-2 flex items-center gap-2">
              <Brain className="w-4 h-4 text-accent" />
              AI Thinking Process
            </h4>
            <div className="bg-gray-900 rounded-lg p-3 max-h-[300px] overflow-y-auto">
              <MarkdownRenderer content={decision.thinking_content} />
            </div>
          </div>
        )}

        {/* Tool Calls */}
        {toolCount > 0 && (
          <div>
            <h4 className="text-sm font-semibold text-gray-300 mb-2 flex items-center gap-2">
              <Terminal className="w-4 h-4 text-primary" />
              Tool Execution Timeline
            </h4>
            <ToolCallGantt tools={decision.tool_calls || []} />
          </div>
        )}

        {/* AI Response */}
        {decision.agent_response && (
          <div>
            <h4 className="text-sm font-semibold text-gray-300 mb-2 flex items-center gap-2">
              <Bot className="w-4 h-4 text-primary" />
              AI Response
            </h4>
            <div className="bg-gray-900 rounded-lg p-3 max-h-[400px] overflow-y-auto">
              <MarkdownRenderer content={decision.agent_response} />
            </div>
          </div>
        )}

        {/* Git Diff */}
        {diffFiles.length > 0 && (
          <div>
            <h4 className="text-sm font-semibold text-gray-300 mb-2 flex items-center gap-2">
              <GitBranch className="w-4 h-4 text-primary" />
              Code Changes
            </h4>
            <DiffViewer files={diffFiles} />
          </div>
        )}
        {diffLoading && (
          <div className="flex items-center justify-center py-8">
            <Loader2 className="w-6 h-6 text-primary animate-spin" />
          </div>
        )}
      </div>
    </div>
  );
}

// ─── Legacy AI Context Panel (removed in refactor) ─────────────────────────────────
// AIContextPanel was removed as it's replaced by DecisionEntryCard + DecisionDetailPanel

// ─── Legacy components (removed in refactor) ─────────────────────────────────
// CheckpointCard and CreateCheckpointForm were removed as this page now focuses on DecisionLog

// ─── Main Page ───────────────────────────────────────

export default function Checkpoints() {
  const { decisions: storeDecisions } = useAppStore();
  const [checkpoints, setCheckpoints] = useState<Checkpoint[]>([]);
  const [allDecisions, setAllDecisions] = useState<DecisionLog[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [hasMoreData, setHasMoreData] = useState(true);
  const [currentPage, setCurrentPage] = useState(0);

  // Filters
  const [searchQuery, setSearchQuery] = useState("");
  const [statusFilter, setStatusFilter] = useState("all");
  const [projectFilter, setProjectFilter] = useState("all");
  const [triggerTypeFilter, setTriggerTypeFilter] = useState("all");
  const [dateRange, setDateRange] = useState<DateRange>({});

  // Detail panel
  const [selectedDecision, setSelectedDecision] = useState<DecisionLog | null>(null);

  // Restore dialog
  const [restoreTarget, setRestoreTarget] = useState<Checkpoint | null>(null);
  const [restoring, setRestoring] = useState(false);

  // Build query parameters
  const buildQuery = useCallback((): TimeRangeQuery => {
    return {
      limit: PAGE_SIZE,
      offset: currentPage * PAGE_SIZE,
      startTime: dateRange.startTime,
      endTime: dateRange.endTime,
    };
  }, [currentPage, dateRange]);

  // Merge store decisions with API-fetched ones
  const mergedDecisions = useMemo(() => {
    const seen = new Set<string>();
    const merged: DecisionLog[] = [];
    for (const d of [...(storeDecisions || []), ...allDecisions]) {
      if (!seen.has(d.id)) {
        seen.add(d.id);
        merged.push(d);
      }
    }
    return merged;
  }, [storeDecisions, allDecisions]);

  // Apply filters to decisions
  const filteredDecisions = useMemo(() => {
    let filtered = [...mergedDecisions];

    // Date range filter - filter out WebSocket data outside time range
    if (dateRange.startTime || dateRange.endTime) {
      filtered = filtered.filter((d) => {
        const timestamp = toMs(d.timestamp);
        if (dateRange.startTime && timestamp < new Date(dateRange.startTime).getTime()) {
          return false;
        }
        if (dateRange.endTime && timestamp > new Date(dateRange.endTime).getTime()) {
          return false;
        }
        return true;
      });
    }

    // Search filter
    if (searchQuery.trim()) {
      const query = searchQuery.toLowerCase();
      filtered = filtered.filter((d) =>
        d.user_prompt?.toLowerCase().includes(query) ||
        d.agent_response?.toLowerCase().includes(query) ||
        d.tool_calls?.some(t => t.tool_name?.toLowerCase().includes(query))
      );
    }

    // Status filter
    if (statusFilter === "success") {
      filtered = filtered.filter((d) =>
        !d.tool_calls?.some(t => String(t.status) === "STATUS_ERROR" || String(t.status) === "4")
      );
    } else if (statusFilter === "error") {
      filtered = filtered.filter((d) =>
        d.tool_calls?.some(t => String(t.status) === "STATUS_ERROR" || String(t.status) === "4")
      );
    } else if (statusFilter === "with_checkpoint") {
      const checkpointDecisionIds = new Set(checkpoints.filter(cp => cp.decision_log_id).map(cp => cp.decision_log_id));
      filtered = filtered.filter((d) => checkpointDecisionIds.has(d.id));
    }

    // Project filter
    if (projectFilter !== "all") {
      filtered = filtered.filter((d) => d.project_path === projectFilter);
    }

    // Trigger type filter (based on linked checkpoints)
    if (triggerTypeFilter !== "all") {
      const relevantCheckpoints = checkpoints.filter(cp =>
        triggerTypeFilter === "manual" ? (cp.trigger_type === "manual" || cp.trigger_type === "user") :
        triggerTypeFilter === "pre_danger" ? (cp.trigger_type === "pre_danger" || cp.trigger_type === "pre_dangerous") :
        cp.trigger_type === triggerTypeFilter
      );
      const checkpointDecisionIds = new Set(relevantCheckpoints.map(cp => cp.decision_log_id).filter(Boolean));
      filtered = filtered.filter((d) => checkpointDecisionIds.has(d.id));
    }

    return filtered.sort((a, b) => toMs(b.timestamp) - toMs(a.timestamp));
  }, [mergedDecisions, searchQuery, statusFilter, projectFilter, triggerTypeFilter, checkpoints, dateRange]);

  // Extract known project paths
  const knownProjects = useMemo(() => {
    const paths = new Set<string>();
    for (const d of mergedDecisions) {
      if (d.project_path) paths.add(d.project_path);
    }
    return Array.from(paths).sort();
  }, [mergedDecisions]);

  // Fetch decisions from API
  const fetchDecisions = useCallback(async (reset = false) => {
    if (reset) {
      setCurrentPage(0);
      setAllDecisions([]);
    }

    setLoadingMore(true);

    try {
      const query = reset ? { ...buildQuery(), offset: 0 } : buildQuery();
      const res = await api.getDecisionsByRange(query);

      if (res.decisions) {
        if (reset) {
          setAllDecisions(res.decisions || []);
        } else {
          setAllDecisions(prev => [...prev, ...(res.decisions || [])]);
        }
        setHasMoreData(res.decisions.length === PAGE_SIZE);
      }
    } catch (error) {
      console.error("Failed to fetch decisions:", error);
    } finally {
      setLoading(false);
      setLoadingMore(false);
    }
  }, [buildQuery]);

  // Initial load and filter changes
  useEffect(() => {
    fetchDecisions(true);
  }, [dateRange]);

  // Load more data
  const loadMore = useCallback(() => {
    if (!loadingMore && hasMoreData) {
      setCurrentPage(prev => prev + 1);
    }
  }, [loadingMore, hasMoreData]);

  useEffect(() => {
    if (currentPage > 0) {
      fetchDecisions(false);
    }
  }, [currentPage, fetchDecisions]);

  // Fetch checkpoints for all known projects
  const fetchCheckpoints = useCallback(async () => {
    const projects = knownProjects.length > 0 ? knownProjects : ["."];
    const all: Checkpoint[] = [];

    for (const proj of projects) {
      try {
        const res = await api.getCheckpoints(proj, {
          startTime: dateRange.startTime,
          endTime: dateRange.endTime,
        });
        if (res.checkpoints) all.push(...res.checkpoints);
      } catch {
        // Project may not have checkpoints
      }
    }

    // Sort by timestamp descending
    all.sort((a, b) => toMs(b.timestamp) - toMs(a.timestamp));
    setCheckpoints(all);
  }, [knownProjects, dateRange]);

  useEffect(() => {
    if (knownProjects.length > 0) {
      fetchCheckpoints();
    }
  }, [fetchCheckpoints, knownProjects]);

  // Count decisions with checkpoints
  const decisionStats = useMemo(() => {
    const withCheckpoints = filteredDecisions.filter(d =>
      checkpoints.some(cp => cp.decision_log_id === d.id)
    ).length;
    return {
      total: filteredDecisions.length,
      withCheckpoints,
    };
  }, [filteredDecisions, checkpoints]);

  // Handle restore
  const handleRestore = async (checkpoint: Checkpoint) => {
    setRestoreTarget(checkpoint);
  };

  const confirmRestore = async () => {
    if (!restoreTarget) return;
    setRestoring(true);
    try {
      await api.restoreCheckpoint(restoreTarget.id);
      setRestoreTarget(null);
      await fetchCheckpoints();
    } catch {
      // Error handling
    } finally {
      setRestoring(false);
    }
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Loader2 className="w-8 h-8 text-primary animate-spin" />
      </div>
    );
  }

  return (
    <div className="space-y-4 animate-fade-in">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-lg font-semibold text-white">
            AI Decision History & Safety Checkpoints
          </h2>
          <p className="text-sm text-gray-500 mt-1">
            AI reasoning process with attached safety snapshots
          </p>
        </div>
        <div className="text-sm text-gray-400 text-right">
          <div>{decisionStats.total} decision{decisionStats.total !== 1 ? "s" : ""}</div>
          <div className="text-xs text-accent">
            {decisionStats.withCheckpoints} with checkpoint{decisionStats.withCheckpoints !== 1 ? "s" : ""}
          </div>
        </div>
      </div>

      {/* Filters */}
      <div className="flex flex-wrap items-center gap-4">
        <DateRangeFilter onChange={setDateRange} compact />
        <SearchFilter
          onSearch={setSearchQuery}
          activeStatus={statusFilter}
          onStatusChange={setStatusFilter}
          statusOptions={STATUS_OPTIONS}
          placeholder="Search prompts, responses, tools..."
        />
        {knownProjects.length > 1 && (
          <div className="flex items-center gap-2">
            <FolderOpen className="w-3.5 h-3.5 text-gray-500" />
            <select
              value={projectFilter}
              onChange={(e) => setProjectFilter(e.target.value)}
              className="bg-gray-900 border border-gray-700 rounded px-2.5 py-1 text-xs text-gray-300 focus:outline-none focus:border-primary/50"
            >
              <option value="all">All Projects</option>
              {knownProjects.map((p) => (
                <option key={p} value={p}>
                  {p.split("/").pop() || p}
                </option>
              ))}
            </select>
          </div>
        )}
        <div className="flex items-center gap-2">
          <Filter className="w-3.5 h-3.5 text-gray-500" />
          <select
            value={triggerTypeFilter}
            onChange={(e) => setTriggerTypeFilter(e.target.value)}
            className="bg-gray-900 border border-gray-700 rounded px-2.5 py-1 text-xs text-gray-300 focus:outline-none focus:border-primary/50"
          >
            {TRIGGER_TYPE_OPTIONS.map((opt) => (
              <option key={opt.value} value={opt.value}>
                {opt.label}
              </option>
            ))}
          </select>
        </div>
      </div>

      {/* Decision list */}
      {filteredDecisions.length === 0 ? (
        <div className="card p-12 text-center">
          <Brain className="w-12 h-12 text-gray-600 mx-auto mb-4" />
          <p className="text-gray-400">
            {searchQuery || statusFilter !== "all" || projectFilter !== "all" || triggerTypeFilter !== "all"
              ? "No decisions match your filters"
              : "No AI decisions recorded yet"}
          </p>
          <p className="text-gray-600 text-sm mt-1">
            Start a conversation with Alice to see AI reasoning and decision history.
          </p>
        </div>
      ) : (
        <>
          <div className="space-y-3">
            {filteredDecisions.map((decision) => (
              <DecisionEntryCard
                key={decision.id}
                decision={decision}
                linkedCheckpoint={findLinkedCheckpoint(decision, checkpoints)}
                onViewDetail={() => setSelectedDecision(decision)}
                onRestoreCheckpoint={() => {
                  const checkpoint = findLinkedCheckpoint(decision, checkpoints);
                  if (checkpoint) handleRestore(checkpoint);
                }}
              />
            ))}
          </div>

          {/* Load more */}
          {hasMoreData && (
            <div className="flex justify-center pt-4">
              <button
                onClick={loadMore}
                disabled={loadingMore}
                className="flex items-center gap-2 px-4 py-2 text-sm text-gray-400 border border-gray-700 rounded-lg hover:border-gray-600 hover:text-gray-300 disabled:opacity-50 transition-colors"
              >
                {loadingMore ? (
                  <Loader2 className="w-4 h-4 animate-spin" />
                ) : (
                  <BarChart3 className="w-4 h-4" />
                )}
                {loadingMore ? "Loading..." : "Load More"}
              </button>
            </div>
          )}
        </>
      )}

      {/* Detail panel */}
      {selectedDecision && (
        <DecisionDetailPanel
          decision={selectedDecision}
          decisions={filteredDecisions}
          onClose={() => setSelectedDecision(null)}
          onNavigate={setSelectedDecision}
        />
      )}

      {/* Restore confirmation dialog */}
      <ConfirmDialog
        open={!!restoreTarget}
        title="Restore Checkpoint"
        message={`This will restore the project to "${restoreTarget?.description || "this checkpoint"}". Uncommitted changes may be lost.`}
        confirmLabel={restoring ? "Restoring..." : "Restore"}
        variant="danger"
        onConfirm={confirmRestore}
        onCancel={() => setRestoreTarget(null)}
      />
    </div>
  );
}
