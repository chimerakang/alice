import { useEffect, useState, useMemo, useCallback } from "react";
import { useNavigate } from "react-router-dom";
import { api } from "@/lib/api";
import { useAppStore } from "@/stores/appStore";
import type { Checkpoint, DecisionLog } from "@/types/alice";
import ConfirmDialog from "@/components/ConfirmDialog";
import StatusBadge from "@/components/StatusBadge";
import MarkdownRenderer from "@/components/MarkdownRenderer";
import {
  Camera,
  RotateCcw,
  Plus,
  Clock,
  GitBranch,
  HardDrive,
  Loader2,
  FolderOpen,
  Zap,
  User,
  ChevronDown,
  ChevronRight,
  MessageSquare,
  Bot,
  Terminal,
  ExternalLink,
} from "lucide-react";

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

/** Find linked decision — prefer direct ID link, fallback to timestamp proximity */
function findLinkedDecision(
  checkpoint: Checkpoint,
  decisions: DecisionLog[],
): DecisionLog | null {
  // Priority 1: Direct link via decision_log_id
  if (checkpoint.decision_log_id) {
    const direct = decisions.find((d) => d.id === checkpoint.decision_log_id);
    if (direct) return direct;
  }

  // Priority 2: Fallback to timestamp proximity (for old checkpoints without decision_log_id)
  const cpTime = toMs(checkpoint.timestamp);
  if (!cpTime) return null;

  let best: DecisionLog | null = null;
  let bestDist = Infinity;

  for (const d of decisions) {
    if (d.chat_id !== checkpoint.chat_id) continue;

    const dTime = toMs(d.timestamp);
    const dist = Math.abs(cpTime - dTime);
    if (dist < 300_000 && dist < bestDist) {
      bestDist = dist;
      best = d;
    }
  }

  return best;
}

// ─── AI Context Panel ────────────────────────────────

function AIContextPanel({
  decision,
  onViewTimeline,
}: {
  decision: DecisionLog;
  onViewTimeline: () => void;
}) {
  const [expanded, setExpanded] = useState(false);
  const toolCount = decision.tool_calls?.length || 0;
  const hasError = decision.tool_calls?.some(
    (t) => String(t.status) === "STATUS_ERROR" || String(t.status) === "4",
  );

  return (
    <div className="mt-3 border-t border-gray-800/60 pt-3">
      {/* User Prompt */}
      <div className="mb-2">
        <div className="flex items-start gap-1.5 text-xs">
          <MessageSquare className="w-3 h-3 text-primary mt-0.5 shrink-0" />
          <p className="text-gray-300 leading-relaxed">
            {decision.user_prompt?.slice(0, 150)}
            {(decision.user_prompt?.length || 0) > 150 ? "…" : ""}
          </p>
        </div>
      </div>

      {/* Meta row: tools, duration, tokens, status */}
      <div className="flex items-center gap-3 text-xs text-gray-500 mb-2">
        <span className="flex items-center gap-1">
          <Terminal className="w-3 h-3" />
          {toolCount} tools
        </span>
        {decision.duration_ms > 0 && (
          <span>{(decision.duration_ms / 1000).toFixed(1)}s</span>
        )}
        {(decision.tokens_input > 0 || decision.tokens_output > 0) && (
          <span className="font-mono">
            {decision.tokens_input + decision.tokens_output} tokens
          </span>
        )}
        <StatusBadge variant={hasError ? "error" : "success"} size="sm" dot>
          {hasError ? "Error" : "Success"}
        </StatusBadge>
        <button
          onClick={onViewTimeline}
          className="ml-auto text-primary hover:text-primary-light flex items-center gap-1 transition-colors"
          title="View in Timeline"
        >
          <ExternalLink className="w-3 h-3" />
          Timeline
        </button>
      </div>

      {/* Expandable AI Response */}
      {decision.agent_response && (
        <div>
          <button
            onClick={() => setExpanded(!expanded)}
            className="flex items-center gap-1 text-xs text-gray-500 hover:text-gray-300 transition-colors mb-1"
          >
            {expanded ? <ChevronDown className="w-3 h-3" /> : <ChevronRight className="w-3 h-3" />}
            <Bot className="w-3 h-3" />
            AI Response
          </button>
          {expanded && (
            <div className="pl-4 border-l-2 border-gray-800 max-h-[300px] overflow-y-auto">
              <MarkdownRenderer content={decision.agent_response} />
            </div>
          )}
        </div>
      )}

      {/* Tool calls summary (collapsed) */}
      {toolCount > 0 && expanded && (
        <div className="mt-2 pl-4 border-l-2 border-gray-800 space-y-1">
          <div className="text-xs text-gray-500 font-semibold mb-1">Tool Calls:</div>
          {decision.tool_calls.slice(0, 10).map((t, i) => {
            const s = String(t.status);
            const isErr = s === "STATUS_ERROR" || s === "4";
            return (
              <div key={t.id || i} className="flex items-center gap-2 text-xs">
                <span className={`w-1.5 h-1.5 rounded-full shrink-0 ${isErr ? "bg-error" : "bg-success"}`} />
                <span className="font-mono text-primary-light">{t.tool_name}</span>
                {t.duration_ms > 0 && (
                  <span className="text-gray-600">{t.duration_ms}ms</span>
                )}
              </div>
            );
          })}
          {toolCount > 10 && (
            <div className="text-xs text-gray-600">...and {toolCount - 10} more</div>
          )}
        </div>
      )}
    </div>
  );
}

// ─── Checkpoint Card ─────────────────────────────────

function CheckpointCard({
  checkpoint,
  isActive,
  linkedDecision,
  onRestore,
  onViewTimeline,
}: {
  checkpoint: Checkpoint;
  isActive: boolean;
  linkedDecision: DecisionLog | null;
  onRestore: () => void;
  onViewTimeline: () => void;
}) {
  const [descExpanded, setDescExpanded] = useState(false);
  const desc = checkpoint.description || "Untitled checkpoint";
  const descTruncateLen = 200;
  const isLongDesc = desc.length > descTruncateLen;
  const displayDesc = isLongDesc && !descExpanded
    ? desc.slice(0, descTruncateLen) + "…"
    : desc;

  return (
    <div
      className={`card p-4 ${
        isActive ? "border-primary/40 bg-primary/5" : ""
      }`}
    >
      <div className="flex items-start justify-between gap-3 mb-3">
        <div className="min-w-0 flex-1">
          {/* Description */}
          <div className="text-sm text-white font-medium leading-snug mb-1.5">
            <Camera className="w-3.5 h-3.5 text-accent inline mr-1.5 -mt-0.5" />
            <span className="whitespace-pre-wrap break-words">{displayDesc}</span>
            {isLongDesc && (
              <button
                onClick={() => setDescExpanded(!descExpanded)}
                className="ml-1.5 text-xs text-primary hover:text-primary-light transition-colors"
              >
                {descExpanded ? "Show less" : "Show more"}
              </button>
            )}
          </div>

          {/* Timestamp + trigger */}
          <div className="flex items-center gap-2 text-xs text-gray-500">
            <Clock className="w-3 h-3" />
            <span className="font-mono tabular-nums">
              {formatTimestamp(checkpoint.timestamp)}
            </span>
            <StatusBadge variant={triggerVariant(checkpoint.trigger_type)} size="sm">
              {triggerIcon(checkpoint.trigger_type)}
              {triggerLabel(checkpoint.trigger_type)}
            </StatusBadge>
            {isActive && (
              <StatusBadge variant="success" size="sm" dot>
                Active
              </StatusBadge>
            )}
          </div>
        </div>

        {/* Restore button */}
        {!isActive && (
          <button
            onClick={onRestore}
            className="flex items-center gap-1.5 px-3 py-1.5 text-xs text-warning border border-warning/30 rounded-lg hover:bg-warning/10 transition-colors shrink-0"
          >
            <RotateCcw className="w-3 h-3" />
            Restore
          </button>
        )}
      </div>

      {/* Dangerous operation tag */}
      {checkpoint.dangerous_op && (
        <div className="flex items-center gap-1.5 text-xs text-warning/80 mb-2">
          <Zap className="w-3 h-3 shrink-0" />
          <span className="font-mono">{checkpoint.dangerous_op}</span>
        </div>
      )}

      {/* Git + storage info */}
      <div className="flex items-center gap-4 text-xs text-gray-500">
        {checkpoint.git_branch && (
          <span className="flex items-center gap-1">
            <GitBranch className="w-3 h-3" />
            <span className="font-mono">{checkpoint.git_branch}</span>
          </span>
        )}
        {checkpoint.git_commit_hash && (
          <span className="font-mono text-gray-600">
            {checkpoint.git_commit_hash.slice(0, 8)}
          </span>
        )}
        {checkpoint.size > 0 && (
          <span className="flex items-center gap-1">
            <HardDrive className="w-3 h-3" />
            {formatSize(checkpoint.size)}
          </span>
        )}
      </div>

      {/* Linked AI Decision Context */}
      {linkedDecision ? (
        <AIContextPanel
          decision={linkedDecision}
          onViewTimeline={onViewTimeline}
        />
      ) : (
        <div className="mt-3 border-t border-gray-800/60 pt-3 text-xs text-gray-600 italic">
          No linked AI decision found
        </div>
      )}
    </div>
  );
}

// ─── Create Checkpoint Form ──────────────────────────

function CreateCheckpointForm({
  projects,
  onCreated,
}: {
  projects: string[];
  onCreated: () => void;
}) {
  const [projectDir, setProjectDir] = useState(projects[0] || "");
  const [description, setDescription] = useState("");
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState("");

  const handleCreate = async () => {
    if (!projectDir || !description.trim()) return;
    setCreating(true);
    setError("");
    try {
      await api.createCheckpoint(projectDir, description.trim());
      setDescription("");
      onCreated();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to create checkpoint");
    } finally {
      setCreating(false);
    }
  };

  return (
    <div className="card p-4 border-dashed">
      <h4 className="text-xs font-semibold text-gray-400 mb-3 flex items-center gap-1.5">
        <Plus className="w-3.5 h-3.5" />
        Create Checkpoint
      </h4>
      <div className="space-y-3">
        {projects.length > 1 && (
          <select
            value={projectDir}
            onChange={(e) => setProjectDir(e.target.value)}
            className="w-full bg-gray-900 border border-gray-700 rounded-lg px-3 py-2 text-sm text-gray-200 focus:outline-none focus:border-primary/50"
          >
            {projects.map((p) => (
              <option key={p} value={p}>
                {p.split("/").pop() || p}
              </option>
            ))}
          </select>
        )}
        <div className="flex gap-2">
          <input
            type="text"
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            placeholder="Checkpoint description..."
            className="flex-1 bg-gray-900 border border-gray-700 rounded-lg px-3 py-2 text-sm text-gray-200 placeholder-gray-500 focus:outline-none focus:border-primary/50"
            onKeyDown={(e) => e.key === "Enter" && handleCreate()}
          />
          <button
            onClick={handleCreate}
            disabled={creating || !description.trim()}
            className="flex items-center gap-1.5 px-4 py-2 text-sm font-medium bg-primary/20 text-primary border border-primary/30 rounded-lg hover:bg-primary/30 disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
          >
            {creating ? (
              <Loader2 className="w-3.5 h-3.5 animate-spin" />
            ) : (
              <Camera className="w-3.5 h-3.5" />
            )}
            Create
          </button>
        </div>
        {error && <p className="text-xs text-error">{error}</p>}
      </div>
    </div>
  );
}

// ─── Main Page ───────────────────────────────────────

export default function Checkpoints() {
  const navigate = useNavigate();
  const { decisions: storeDecisions } = useAppStore();
  const [checkpoints, setCheckpoints] = useState<Checkpoint[]>([]);
  const [allDecisions, setAllDecisions] = useState<DecisionLog[]>([]);
  const [loading, setLoading] = useState(true);
  const [projectFilter, setProjectFilter] = useState("all");
  const [restoreTarget, setRestoreTarget] = useState<Checkpoint | null>(null);
  const [restoring, setRestoring] = useState(false);

  // Merge store decisions with API-fetched ones
  const mergedDecisions = useMemo(() => {
    const seen = new Set<string>();
    const merged: DecisionLog[] = [];
    for (const d of [...storeDecisions, ...allDecisions]) {
      if (!seen.has(d.id)) {
        seen.add(d.id);
        merged.push(d);
      }
    }
    return merged;
  }, [storeDecisions, allDecisions]);

  // Extract known project paths
  const knownProjects = useMemo(() => {
    const paths = new Set<string>();
    for (const d of mergedDecisions) {
      if (d.project_path) paths.add(d.project_path);
    }
    return Array.from(paths).sort();
  }, [mergedDecisions]);

  // Fetch decisions from API
  useEffect(() => {
    api.getRecentDecisions(50).then((res) => {
      if (res.decisions) setAllDecisions(res.decisions);
    }).catch(() => {});
  }, []);

  // Fetch checkpoints for all known projects
  const fetchCheckpoints = useCallback(async () => {
    const projects = knownProjects.length > 0 ? knownProjects : ["."];
    const all: Checkpoint[] = [];

    for (const proj of projects) {
      try {
        const res = await api.getCheckpoints(proj);
        if (res.checkpoints) all.push(...res.checkpoints);
      } catch {
        // Project may not have checkpoints
      }
    }

    // Sort by timestamp descending
    all.sort((a, b) => toMs(b.timestamp) - toMs(a.timestamp));
    setCheckpoints(all);
  }, [knownProjects]);

  useEffect(() => {
    const load = async () => {
      await fetchCheckpoints();
      setLoading(false);
    };
    load();
  }, [fetchCheckpoints]);

  // Filter by project
  const filtered = useMemo(() => {
    if (projectFilter === "all") return checkpoints;
    return checkpoints.filter((c) => c.project_dir === projectFilter);
  }, [checkpoints, projectFilter]);

  // Unique project dirs from checkpoints
  const checkpointProjects = useMemo(() => {
    const paths = new Set<string>();
    for (const c of checkpoints) {
      if (c.project_dir) paths.add(c.project_dir);
    }
    return Array.from(paths).sort();
  }, [checkpoints]);

  // All known projects (union)
  const allProjects = useMemo(() => {
    const all = new Set([...knownProjects, ...checkpointProjects]);
    return Array.from(all).sort();
  }, [knownProjects, checkpointProjects]);

  // Handle restore
  const handleRestore = async () => {
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
        <h2 className="text-lg font-semibold text-white">
          Checkpoint Management
        </h2>
        <span className="text-sm text-gray-400">
          {checkpoints.length} checkpoint{checkpoints.length !== 1 ? "s" : ""}
        </span>
      </div>

      {/* Create form */}
      {allProjects.length > 0 && (
        <CreateCheckpointForm
          projects={allProjects}
          onCreated={fetchCheckpoints}
        />
      )}

      {/* Project filter */}
      {checkpointProjects.length > 1 && (
        <div className="flex items-center gap-2">
          <FolderOpen className="w-3.5 h-3.5 text-gray-500" />
          <div className="flex items-center gap-1.5 flex-wrap">
            <button
              onClick={() => setProjectFilter("all")}
              className={`px-2.5 py-1 text-xs rounded-full border transition-colors ${
                projectFilter === "all"
                  ? "bg-primary/15 text-primary border-primary/30"
                  : "text-gray-400 border-gray-700 hover:border-gray-600 hover:text-gray-300"
              }`}
            >
              All
            </button>
            {checkpointProjects.map((p) => (
              <button
                key={p}
                onClick={() => setProjectFilter(p)}
                title={p}
                className={`px-2.5 py-1 text-xs rounded-full border transition-colors ${
                  projectFilter === p
                    ? "bg-primary/15 text-primary border-primary/30"
                    : "text-gray-400 border-gray-700 hover:border-gray-600 hover:text-gray-300"
                }`}
              >
                {p.split("/").pop() || p}
              </button>
            ))}
          </div>
        </div>
      )}

      {/* Checkpoint list */}
      {filtered.length === 0 ? (
        <div className="card p-12 text-center">
          <Camera className="w-12 h-12 text-gray-600 mx-auto mb-4" />
          <p className="text-gray-400">No checkpoints yet</p>
          <p className="text-gray-600 text-sm mt-1">
            Checkpoints are created automatically during AI operations, or you can create one manually above.
          </p>
        </div>
      ) : (
        <div className="space-y-3">
          {filtered.map((cp) => (
            <CheckpointCard
              key={cp.id}
              checkpoint={cp}
              isActive={cp.is_active}
              linkedDecision={findLinkedDecision(cp, mergedDecisions)}
              onRestore={() => setRestoreTarget(cp)}
              onViewTimeline={() => navigate("/timeline")}
            />
          ))}
        </div>
      )}

      {/* Restore confirmation dialog */}
      <ConfirmDialog
        open={!!restoreTarget}
        title="Restore Checkpoint"
        message={`This will restore the project to "${restoreTarget?.description || "this checkpoint"}". Uncommitted changes may be lost.`}
        confirmLabel={restoring ? "Restoring..." : "Restore"}
        variant="danger"
        onConfirm={handleRestore}
        onCancel={() => setRestoreTarget(null)}
      />
    </div>
  );
}
