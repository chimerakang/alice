import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "@/lib/api";
import type { HermesActiveTask, HermesSnapshotHop, HermesSubTaskView } from "@/types/alice";
import HermesStatsPanel from "@/components/HermesStatsPanel";
import {
  AlertTriangle,
  CheckCircle2,
  ChevronRight,
  ExternalLink,
  Loader2,
  PauseCircle,
  Pickaxe,
  RefreshCw,
  XCircle,
} from "lucide-react";

// HermesTasks page lists every Hermes task (active + terminal) with a
// status filter and a click-in snapshot history view. The list reuses
// the same backend types as the active panel; the drill-in fetches
// /api/hermes/snapshots on demand and renders the Walker hops as a
// vertical timeline.

type StatusFilter = "" | "planning" | "executing" | "done" | "failed" | "interrupted";

const STATUS_FILTERS: Array<{ value: StatusFilter; labelKey: string }> = [
  { value: "", labelKey: "common.all" },
  { value: "executing", labelKey: "hermes_tasks.status_executing" },
  { value: "planning", labelKey: "hermes_tasks.status_planning" },
  { value: "done", labelKey: "hermes_tasks.status_done" },
  { value: "failed", labelKey: "hermes_tasks.status_failed" },
  { value: "interrupted", labelKey: "hermes_tasks.status_interrupted" },
];

function formatRelative(iso?: string): string {
  if (!iso) return "—";
  const t = new Date(iso).getTime();
  if (Number.isNaN(t)) return iso;
  const diff = Date.now() - t;
  if (diff < 60_000) return `${Math.max(1, Math.floor(diff / 1000))}s`;
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}m`;
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)}h`;
  return `${Math.floor(diff / 86_400_000)}d`;
}

function formatExact(iso: string): string {
  if (!iso) return "—";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleString();
}

function shortTaskID(id: string): string {
  return id.length > 8 ? id.slice(0, 8) : id;
}

function statusIcon(status: string) {
  switch (status) {
    case "done":
      return <CheckCircle2 className="w-4 h-4 text-green-500" />;
    case "failed":
      return <XCircle className="w-4 h-4 text-red-500" />;
    case "interrupted":
      return <PauseCircle className="w-4 h-4 text-orange-500" />;
    case "executing":
    case "planning":
      return <Loader2 className="w-4 h-4 text-blue-400 animate-spin" />;
    default:
      return <AlertTriangle className="w-4 h-4 text-gray-500" />;
  }
}

function HopRow({ hop }: { hop: HermesSnapshotHop }) {
  return (
    <div className="flex items-start gap-3 py-2 border-l-2 border-gray-800 pl-3 hover:border-primary">
      <div className="flex-shrink-0 text-xs font-mono text-gray-500 w-10">#{hop.step}</div>
      <div className="flex-1 min-w-0">
        <div className="flex flex-wrap items-center gap-x-2 text-sm">
          <span className="font-mono text-gray-300">{hop.source_node || "?"}</span>
          <ChevronRight className="w-3 h-3 text-gray-600" />
          <span className="font-mono text-blue-400">{hop.next_step || "?"}</span>
          {hop.has_interrupt && (
            <span className="px-1.5 py-0.5 rounded bg-orange-900/40 text-orange-300 text-[10px] font-mono">
              ⏸ {hop.interrupt_reason || "interrupt"}
            </span>
          )}
        </div>
        <div className="text-xs text-gray-500 mt-0.5">
          <span className="font-mono">{hop.reason || "—"}</span>
          {hop.status && <span className="ml-2 text-gray-600">[{hop.status}]</span>}
          <span className="ml-2 text-gray-600">idx={hop.current_idx}</span>
          <span className="ml-2 text-gray-600">{formatExact(hop.created_at)}</span>
        </div>
      </div>
    </div>
  );
}

function subTaskStatusBadge(status: string): { label: string; cls: string } {
  switch (status) {
    case "done":
      return { label: "✅ done", cls: "bg-green-900/40 text-green-300" };
    case "failed":
      return { label: "❌ failed", cls: "bg-red-900/40 text-red-300" };
    case "skipped":
      return { label: "⏭ skipped", cls: "bg-yellow-900/40 text-yellow-300" };
    case "in_progress":
      return { label: "🔄 in_progress", cls: "bg-blue-900/40 text-blue-300" };
    case "pending":
      return { label: "⏸ pending", cls: "bg-gray-900/40 text-gray-300" };
    default:
      return { label: status || "?", cls: "bg-gray-900/40 text-gray-300" };
  }
}

function SubTaskCard({ sub, idx }: { sub: HermesSubTaskView; idx: number }) {
  const [showFull, setShowFull] = useState(false);
  const badge = subTaskStatusBadge(sub.status);
  const result = sub.result || "";
  const tooLong = result.length > 600;
  const visible = showFull ? result : result.slice(0, 600);
  return (
    <div className="rounded-md border border-gray-800 bg-gray-900/30 p-3 text-sm">
      <div className="flex items-start gap-2 mb-1">
        <span className="font-mono text-xs text-gray-500 mt-0.5">#{idx + 1}</span>
        <span className={`px-1.5 py-0.5 rounded text-[11px] font-mono ${badge.cls}`}>
          {badge.label}
        </span>
        {sub.attempts ? (
          <span className="text-[11px] text-gray-500">attempts={sub.attempts}</span>
        ) : null}
        {sub.tokens_used ? (
          <span className="text-[11px] text-gray-500">tokens={sub.tokens_used}</span>
        ) : null}
        <span className="font-mono text-[11px] text-gray-600 ml-auto">{sub.id}</span>
      </div>
      <div className="text-gray-200 whitespace-pre-wrap break-words mb-2">
        {sub.description || "(no description)"}
      </div>
      {result && (
        <details open={!tooLong}>
          <summary className="text-xs text-gray-500 cursor-pointer mb-1">
            Result {tooLong && !showFull ? `(${result.length} chars, click to expand)` : ""}
          </summary>
          <pre className="text-[12px] text-gray-300 whitespace-pre-wrap break-words bg-black/30 p-2 rounded font-mono leading-relaxed max-h-[50vh] overflow-y-auto">
            {visible}
            {tooLong && !showFull && "…"}
          </pre>
          {tooLong && (
            <button
              onClick={() => setShowFull((x) => !x)}
              className="text-xs text-blue-400 hover:underline mt-1"
            >
              {showFull ? "Collapse" : "Show full result"}
            </button>
          )}
        </details>
      )}
      {sub.retry_feedback && (
        <details className="mt-2">
          <summary className="text-xs text-orange-400 cursor-pointer">
            Reviewer retry feedback
          </summary>
          <pre className="text-[12px] text-orange-200 whitespace-pre-wrap break-words bg-black/30 p-2 rounded font-mono leading-relaxed mt-1 max-h-[40vh] overflow-y-auto">
            {sub.retry_feedback}
          </pre>
        </details>
      )}
    </div>
  );
}

function TaskHistory({ taskId }: { taskId: string }) {
  const [hops, setHops] = useState<HermesSnapshotHop[]>([]);
  const [plan, setPlan] = useState<HermesSubTaskView[]>([]);
  const [accumulated, setAccumulated] = useState("");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [tab, setTab] = useState<"subtasks" | "hops" | "accumulated">("subtasks");

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);
    api
      .getHermesSnapshots(taskId)
      .then((res) => {
        if (cancelled) return;
        setHops(res.snapshots || []);
        setPlan(res.latest_plan || []);
        setAccumulated(res.accumulated || "");
      })
      .catch((e) => {
        if (!cancelled) setError(e instanceof Error ? e.message : String(e));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [taskId]);

  if (loading) {
    return (
      <div className="text-sm text-gray-500 flex items-center gap-2 py-2">
        <Loader2 className="w-4 h-4 animate-spin" />
        Loading…
      </div>
    );
  }
  if (error) {
    return <div className="text-sm text-red-400 py-2">{error}</div>;
  }
  return (
    <div className="space-y-3">
      <div className="flex gap-2 border-b border-gray-800 pb-1">
        <TabButton active={tab === "subtasks"} onClick={() => setTab("subtasks")}>
          子任務 ({plan.length})
        </TabButton>
        <TabButton active={tab === "hops"} onClick={() => setTab("hops")}>
          Walker hops ({hops.length})
        </TabButton>
        <TabButton active={tab === "accumulated"} onClick={() => setTab("accumulated")}>
          Accumulated
        </TabButton>
      </div>

      {tab === "subtasks" && (
        <div className="space-y-2">
          {plan.length === 0 ? (
            <div className="text-sm text-gray-500">尚無子任務（任務可能還沒進到 planner）。</div>
          ) : (
            plan.map((sub, idx) => <SubTaskCard key={sub.id || idx} sub={sub} idx={idx} />)
          )}
        </div>
      )}
      {tab === "hops" && (
        <div>
          {hops.length === 0 ? (
            <div className="text-sm text-gray-500">No snapshot history.</div>
          ) : (
            hops.map((hop) => <HopRow key={hop.snapshot_id} hop={hop} />)
          )}
        </div>
      )}
      {tab === "accumulated" && (
        <div>
          {accumulated ? (
            <pre className="text-[12px] text-gray-300 whitespace-pre-wrap break-words bg-black/30 p-3 rounded font-mono leading-relaxed max-h-[60vh] overflow-y-auto">
              {accumulated}
            </pre>
          ) : (
            <div className="text-sm text-gray-500">尚無 accumulated 內容。</div>
          )}
        </div>
      )}
    </div>
  );
}

function TabButton({
  active,
  onClick,
  children,
}: {
  active: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      onClick={onClick}
      className={`px-3 py-1 text-xs rounded-t-md ${
        active
          ? "bg-gray-800 text-white border-b-2 border-primary"
          : "text-gray-400 hover:text-gray-200"
      }`}
    >
      {children}
    </button>
  );
}

export default function HermesTasks() {
  const { t } = useTranslation();
  const [tasks, setTasks] = useState<HermesActiveTask[]>([]);
  const [status, setStatus] = useState<StatusFilter>("");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [expanded, setExpanded] = useState<string | null>(null);
  const [total, setTotal] = useState(0);

  const load = () => {
    setLoading(true);
    setError(null);
    api
      .getHermesTasks({ status: status || undefined, limit: 100 })
      .then((res) => {
        setTasks(res.tasks || []);
        setTotal(res.total || 0);
      })
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [status]);

  const counts = useMemo(() => {
    return tasks.reduce<Record<string, number>>((acc, task) => {
      acc[task.status] = (acc[task.status] || 0) + 1;
      return acc;
    }, {});
  }, [tasks]);

  return (
    <div className="space-y-4">
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3">
        <div>
          <h1 className="text-2xl font-bold text-white flex items-center gap-2">
            <Pickaxe className="w-6 h-6 text-primary" />
            {t("hermes_tasks.title")}
          </h1>
          <p className="text-sm text-gray-500 mt-1">
            {t("hermes_tasks.subtitle")}
          </p>
        </div>
        <button
          onClick={load}
          disabled={loading}
          className="btn btn-secondary inline-flex items-center gap-2 self-start sm:self-auto"
        >
          {loading ? <Loader2 className="w-4 h-4 animate-spin" /> : <RefreshCw className="w-4 h-4" />}
          {t("common.refresh")}
        </button>
      </div>

      <HermesStatsPanel />

      <div className="flex flex-wrap gap-2">
        {STATUS_FILTERS.map((f) => {
          const active = status === f.value;
          const n = f.value ? counts[f.value] || 0 : tasks.length;
          return (
            <button
              key={f.value || "all"}
              onClick={() => setStatus(f.value)}
              className={`px-3 py-1 rounded-md text-sm border ${
                active
                  ? "bg-primary text-white border-primary"
                  : "border-gray-800 text-gray-300 hover:border-gray-600"
              }`}
            >
              {t(f.labelKey, f.value || "all")}
              <span className="ml-2 text-xs text-gray-500">{n}</span>
            </button>
          );
        })}
      </div>

      {error && <div className="text-sm text-red-400">{error}</div>}

      <div className="card divide-y divide-gray-800">
        {tasks.length === 0 && !loading && (
          <div className="p-6 text-center text-sm text-gray-500">
            {t("hermes_tasks.empty")}
          </div>
        )}
        {tasks.map((task) => {
          const isOpen = expanded === task.task_id;
          return (
            <div key={task.task_id}>
              <div
                role="button"
                tabIndex={0}
                onClick={() => setExpanded(isOpen ? null : task.task_id)}
                onKeyDown={(e) => {
                  if (e.key === "Enter" || e.key === " ") {
                    e.preventDefault();
                    setExpanded(isOpen ? null : task.task_id);
                  }
                }}
                className="w-full text-left p-3 hover:bg-gray-900/40 cursor-pointer"
              >
                <div className="flex flex-wrap items-start gap-x-3 gap-y-1">
                  <div className="flex items-center gap-2 mt-0.5">
                    {statusIcon(task.status)}
                    <span className="font-mono text-xs text-gray-500">
                      {shortTaskID(task.task_id)}
                    </span>
                  </div>
                  <div className="flex-1 min-w-0">
                    <div className="text-gray-200 font-medium whitespace-pre-wrap break-words max-h-40 overflow-y-auto">
                      {task.goal || "(no goal)"}
                    </div>
                    <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-gray-500 mt-1">
                      <span>
                        {task.status} · step={task.next_step || "—"} · {task.current_idx + 1}/
                        {task.plan_length || "?"}
                      </span>
                      {task.github_issue_number ? (
                        task.github_url ? (
                          <a
                            href={task.github_url}
                            target="_blank"
                            rel="noopener noreferrer"
                            onClick={(e) => e.stopPropagation()}
                            className="text-blue-400 hover:underline inline-flex items-center gap-1"
                          >
                            #{task.github_issue_number}
                            <ExternalLink className="w-3 h-3" />
                          </a>
                        ) : (
                          <span className="text-blue-400">#{task.github_issue_number}</span>
                        )
                      ) : null}
                      {task.max_total_tokens ? (
                        <span>
                          tokens {task.used_tokens}/{task.max_total_tokens}
                        </span>
                      ) : null}
                      <span className="text-gray-600 ml-auto">
                        {formatRelative(task.updated_at)} ago
                      </span>
                    </div>
                  </div>
                </div>
              </div>
              {isOpen && (
                <div className="px-4 py-3 bg-gray-900/30">
                  <TaskHistory taskId={task.task_id} />
                </div>
              )}
            </div>
          );
        })}
      </div>

      {total > tasks.length && (
        <div className="text-xs text-gray-500 text-center">
          顯示 {tasks.length} / {total}（捲頁尚未實作）
        </div>
      )}
    </div>
  );
}
