import { useEffect, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { api } from "@/lib/api";
import type { HermesActiveTask, HermesInterrupt } from "@/types/alice";
import { AlertTriangle, Loader2, PauseCircle, Pickaxe, RefreshCw, X } from "lucide-react";

// HermesTasksPanel surfaces non-terminal Hermes tasks on the dashboard
// and lets the operator resolve a paused interrupt without going
// through Telegram. Mirrors the Telegram failure-pause / budget-pause
// UX (#171 Class C UI).

function formatRelative(iso?: string): string {
  if (!iso) return "—";
  const t = new Date(iso).getTime();
  if (Number.isNaN(t)) return iso;
  const diff = Date.now() - t;
  if (diff < 60_000) return `${Math.max(1, Math.floor(diff / 1000))}s ago`;
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}m ago`;
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)}h ago`;
  return `${Math.floor(diff / 86_400_000)}d ago`;
}

function shortTaskID(id: string): string {
  return id.length > 8 ? id.slice(0, 8) : id;
}

interface InterruptKind {
  label: string;
  icon: ReactNode;
  buttons: Array<{ decision: "retry" | "skip" | "abort"; label: string; variant: "primary" | "secondary" | "danger" }>;
}

function classifyInterrupt(interrupt?: HermesInterrupt): InterruptKind | null {
  if (!interrupt) return null;
  if (interrupt.reason === "budget_exceeded") {
    return {
      label: "💸 預算耗盡",
      icon: <AlertTriangle className="w-4 h-4 text-yellow-400" />,
      buttons: [
        { decision: "retry", label: "▶ 繼續", variant: "primary" },
        { decision: "abort", label: "🛑 中止", variant: "danger" },
      ],
    };
  }
  if (interrupt.reason === "subtask_failure_pause") {
    return {
      label: "⏸ 子任務失敗",
      icon: <PauseCircle className="w-4 h-4 text-orange-400" />,
      buttons: [
        { decision: "retry", label: "🔁 重試", variant: "primary" },
        { decision: "skip", label: "⏭ 跳過", variant: "secondary" },
        { decision: "abort", label: "🛑 中止", variant: "danger" },
      ],
    };
  }
  if (interrupt.reason?.startsWith("ctx_cancelled")) {
    return {
      label: "⏹ Context cancel",
      icon: <X className="w-4 h-4 text-gray-400" />,
      buttons: [
        { decision: "retry", label: "🔁 繼續", variant: "primary" },
        { decision: "abort", label: "🛑 中止", variant: "danger" },
      ],
    };
  }
  // Unknown interrupt — at least let the operator abort.
  return {
    label: `⚠ ${interrupt.reason || "unknown"}`,
    icon: <AlertTriangle className="w-4 h-4 text-red-400" />,
    buttons: [
      { decision: "retry", label: "🔁 重試", variant: "secondary" },
      { decision: "abort", label: "🛑 中止", variant: "danger" },
    ],
  };
}

function variantClass(variant: "primary" | "secondary" | "danger"): string {
  switch (variant) {
    case "primary":
      return "bg-blue-600 hover:bg-blue-500 text-white";
    case "danger":
      return "bg-red-600 hover:bg-red-500 text-white";
    default:
      return "bg-gray-700 hover:bg-gray-600 text-white";
  }
}

export default function HermesTasksPanel() {
  const { t } = useTranslation();
  const [tasks, setTasks] = useState<HermesActiveTask[]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const load = () => {
    setLoading(true);
    setError(null);
    api
      .getHermesActiveTasks()
      .then((res) => setTasks(res.tasks || []))
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    load();
    const id = setInterval(load, 15_000);
    return () => clearInterval(id);
  }, []);

  const resolve = async (taskId: string, decision: "retry" | "skip" | "abort") => {
    setBusy(taskId);
    try {
      const res = await api.resolveHermesTask(taskId, decision);
      if (!res.ok) throw new Error("resolve failed");
      load();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(null);
    }
  };

  if (tasks.length === 0 && !loading && !error) {
    return null; // hide the panel entirely when nothing is active
  }

  return (
    <div className="card p-4">
      <div className="flex items-center justify-between mb-3">
        <div className="flex items-center gap-2">
          <Pickaxe className="w-5 h-5 text-primary" />
          <h2 className="text-lg font-semibold text-white">
            {t("runtime.hermes_tasks.title", "Hermes 任務（未結束）")}
          </h2>
          <span className="text-xs text-gray-500">{tasks.length}</span>
        </div>
        <button
          onClick={load}
          className="btn btn-secondary text-xs inline-flex items-center gap-1"
          disabled={loading}
        >
          {loading ? <Loader2 className="w-3 h-3 animate-spin" /> : <RefreshCw className="w-3 h-3" />}
          {t("common.refresh")}
        </button>
      </div>
      {error && (
        <div className="text-sm text-red-400 mb-2">{error}</div>
      )}
      <div className="space-y-2">
        {tasks.map((task) => {
          const kind = classifyInterrupt(task.interrupt);
          const isBusy = busy === task.task_id;
          return (
            <div
              key={task.task_id}
              className="rounded-md border border-gray-800 bg-gray-900/40 p-3 text-sm"
            >
              <div className="flex items-start gap-x-3 gap-y-1">
                <span className="font-mono text-xs text-gray-500 mt-0.5">{shortTaskID(task.task_id)}</span>
                <div className="flex-1 min-w-0">
                  <div className="text-gray-200 font-medium whitespace-pre-wrap break-words">
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
                          className="text-blue-400 hover:underline"
                        >
                          #{task.github_issue_number} ↗
                        </a>
                      ) : (
                        <span className="text-blue-400">#{task.github_issue_number}</span>
                      )
                    ) : null}
                    {task.max_total_tokens ? (
                      <span>tokens {task.used_tokens}/{task.max_total_tokens}</span>
                    ) : null}
                    <span className="text-gray-600 ml-auto">{formatRelative(task.updated_at)}</span>
                  </div>
                </div>
              </div>
              {kind && (
                <div className="mt-2 flex flex-wrap items-center gap-2">
                  {kind.icon}
                  <span className="text-xs text-gray-300">{kind.label}</span>
                  {task.interrupt?.reason ? (
                    <span className="font-mono text-[10px] text-gray-600">{task.interrupt.reason}</span>
                  ) : null}
                  <div className="flex gap-2 ml-auto">
                    {kind.buttons.map((b) => (
                      <button
                        key={b.decision}
                        onClick={() => resolve(task.task_id, b.decision)}
                        disabled={isBusy}
                        className={`px-3 py-1 rounded text-xs ${variantClass(b.variant)} disabled:opacity-50`}
                      >
                        {isBusy ? "…" : b.label}
                      </button>
                    ))}
                  </div>
                </div>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
}
