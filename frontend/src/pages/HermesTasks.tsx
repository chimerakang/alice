import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate, useSearchParams } from "react-router-dom";
import { api } from "@/lib/api";
import type {
  DecisionLog,
  HermesActiveTask,
  HermesSnapshotHop,
  HermesSubTaskView,
  HermesTaskState,
  RuntimeEventRecord,
  UnifiedTask,
  UnifiedReviewSubTaskResult,
} from "@/types/alice";
import HermesStatsPanel from "@/components/HermesStatsPanel";
import ReviewSubTaskTable from "@/components/ReviewSubTaskTable";
import StatusBadge from "@/components/StatusBadge";
import {
  AlertTriangle,
  CheckCircle2,
  ChevronRight,
  ExternalLink,
  Loader2,
  PauseCircle,
  Pickaxe,
  RefreshCw,
  ArrowRight,
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

function graphNodePayload(event: RuntimeEventRecord) {
  const payload = event.payload || {};
  const duration = Number(payload.duration_ms || 0);
  return {
    node: String(payload.node || "?"),
    status: String(payload.status || "?"),
    nextStep: String(payload.next_step || ""),
    reason: String(payload.reason || ""),
    durationMS: Number.isFinite(duration) ? duration : 0,
    stepIndex: Number(payload.step_index || 0),
    halt: Boolean(payload.halt),
    error: String(payload.error || ""),
  };
}

function graphNodeStatusClass(status: string): string {
  switch (status) {
    case "done":
      return "bg-green-900/40 text-green-300";
    case "failed":
      return "bg-red-900/40 text-red-300";
    case "started":
      return "bg-blue-900/40 text-blue-300";
    default:
      return "bg-gray-900/40 text-gray-300";
  }
}

function GraphNodeEventRow({ event }: { event: RuntimeEventRecord }) {
  const node = graphNodePayload(event);
  return (
    <div className="grid grid-cols-[84px_minmax(0,1fr)] gap-3 py-2 border-l-2 border-gray-800 pl-3 hover:border-primary">
      <div className="text-xs text-gray-500">
        <div className="font-mono">#{node.stepIndex}</div>
        <div>{formatRelative(event.timestamp)} ago</div>
      </div>
      <div className="min-w-0">
        <div className="flex flex-wrap items-center gap-2 text-sm">
          <span className={`px-1.5 py-0.5 rounded text-[11px] font-mono ${graphNodeStatusClass(node.status)}`}>
            {node.status}
          </span>
          <span className="font-mono text-gray-200">{node.node}</span>
          {node.nextStep ? (
            <>
              <ChevronRight className="w-3 h-3 text-gray-600" />
              <span className="font-mono text-blue-400">{node.nextStep}</span>
            </>
          ) : null}
          {node.halt ? (
            <span className="px-1.5 py-0.5 rounded bg-orange-900/40 text-orange-300 text-[10px] font-mono">
              halt
            </span>
          ) : null}
          {node.durationMS ? (
            <span className="text-xs text-gray-500 ml-auto">{node.durationMS}ms</span>
          ) : null}
        </div>
        <div className="text-xs text-gray-500 mt-0.5">
          <span className="font-mono">{node.error || node.reason || "—"}</span>
          <span className="ml-2 text-gray-600">{formatExact(event.timestamp)}</span>
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

function formatTokens(value?: number): string {
  if (!value || value <= 0) return "—";
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(1)}M`;
  if (value >= 1_000) return `${(value / 1_000).toFixed(1)}K`;
  return String(value);
}

function formatCompactDuration(startedAt?: string, endedAt?: string): string {
  if (!startedAt || !endedAt) return "—";
  const start = new Date(startedAt).getTime();
  const end = new Date(endedAt).getTime();
  if (!Number.isFinite(start) || !Number.isFinite(end) || end < start) return "—";
  const ms = end - start;
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`;
  return `${(ms / 60000).toFixed(1)}m`;
}

function countArtifacts(task?: UnifiedTask | null): number {
  return task?.sub_tasks?.reduce((total, subTask) => total + (subTask.artifacts?.length || 0), 0) || 0;
}

function findDecisionForTask(decisions: DecisionLog[], taskId: string): DecisionLog | null {
  return decisions.find((decision) => decision.id === taskId || decision.unified_task?.id === taskId) || null;
}

function latestChecklistEvent(events: RuntimeEventRecord[]): RuntimeEventRecord | null {
  return (
    events.find((event) => {
      if (event.type !== "IssueFSMTransition") return false;
      const payload = event.payload || {};
      const source = String(payload.source || "");
      const to = String(payload.to || "");
      const eventName = String(payload.event || "");
      return (
        source === "engine.sync_checklist" ||
        to.includes("checklist") ||
        eventName.includes("Checklist") ||
        eventName === "SyncFailed"
      );
    }) || null
  );
}

function latestIssueEvent(
  events: RuntimeEventRecord[],
  predicate: (event: RuntimeEventRecord) => boolean,
): RuntimeEventRecord | null {
  return events.find(predicate) || null;
}

function checklistStatusLabel(event: RuntimeEventRecord | null): { label: string; variant: "success" | "warning" | "error" | "neutral" } {
  if (!event) return { label: "unknown", variant: "neutral" };
  const payload = event.payload || {};
  if (payload.checklist_synced === true || payload.to === "checklist_synced") {
    return { label: "checklist_synced", variant: "success" };
  }
  if (payload.checklist_synced === false || payload.to === "checklist_unsynced") {
    return { label: "checklist_unsynced", variant: "warning" };
  }
  if (String(payload.event || "").toLowerCase().includes("close") || String(payload.reason || "").toLowerCase().includes("blocked")) {
    return { label: String(payload.to || payload.event || "blocked"), variant: "error" };
  }
  return { label: String(payload.to || payload.event || "unknown"), variant: "neutral" };
}

function formatPercent(value?: number): string {
  if (value === undefined || !Number.isFinite(value)) return "—";
  return `${value.toFixed(1)}%`;
}

function formatCacheTokens(value?: number): string {
  if (!value || value <= 0) return "—";
  return formatTokens(value);
}

function summarizeTaskUsage(task?: HermesTaskState | null) {
  const modelUsages = task?.model_usages || [];
  const phaseUsages = task?.phase_usages || [];
  const inputTokens = modelUsages.reduce((total, usage) => total + (usage.input_tokens || 0), 0);
  const outputTokens = modelUsages.reduce((total, usage) => total + (usage.output_tokens || 0), 0);
  const cacheReadTokens = modelUsages.reduce((total, usage) => total + (usage.cache_read_input_tokens || 0), 0);
  const cacheCreationTokens = modelUsages.reduce((total, usage) => total + (usage.cache_creation_input_tokens || 0), 0);
  const totalCostUSD = modelUsages.reduce((total, usage) => total + (usage.cost_usd || 0), 0);
  const cacheHitPercent = inputTokens > 0 ? (cacheReadTokens * 100) / inputTokens : 0;

  return {
    modelUsages,
    phaseUsages,
    inputTokens,
    outputTokens,
    cacheReadTokens,
    cacheCreationTokens,
    totalCostUSD,
    cacheHitPercent,
  };
}

function diagnosticsFromEvents(events: RuntimeEventRecord[]) {
  const syncEvent = latestIssueEvent(
    events,
    (event) => event.type === "IssueFSMTransition" && String(event.payload?.source || "") === "engine.sync_checklist",
  );
  const closeEvent = latestIssueEvent(
    events,
    (event) =>
      event.type === "IssueFSMTransition" &&
      ["engine.close_issue", "engine.on_done.close"].includes(String(event.payload?.source || "")),
  );
  const closeGuardEvent = latestIssueEvent(
    events,
    (event) =>
      event.type === "IssueFSMTransition" &&
      String(event.payload?.source || "") === "engine.on_done" &&
      (event.payload?.can_auto_close === false || event.payload?.needs_human_confirmation === true || event.payload?.has_unchecked_required_items === true),
  );

  return {
    syncEvent,
    closeEvent,
    closeGuardEvent,
  };
}

function retryEvidenceLink(taskId: string): string {
  return `/reviews?focus=${encodeURIComponent(taskId)}`;
}

function summaryVariant(score?: number): "success" | "warning" | "error" {
  if (score === undefined) return "warning";
  if (score >= 85) return "success";
  if (score >= 70) return "warning";
  return "error";
}

function SubTaskCard({
  sub,
  idx,
  reviewResult,
  retryEvidenceHref,
}: {
  sub: HermesSubTaskView;
  idx: number;
  reviewResult?: UnifiedReviewSubTaskResult;
  retryEvidenceHref?: string;
}) {
  const { t } = useTranslation();
  const [showFull, setShowFull] = useState(false);
  const badge = subTaskStatusBadge(sub.status);
  const result = sub.result || "";
  const tooLong = result.length > 600;
  const visible = showFull ? result : result.slice(0, 600);
  const lowScore = reviewResult ? reviewResult.score < 70 : false;
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
      {reviewResult ? (
        <div className="mb-2 flex flex-wrap items-center gap-2 text-xs">
          <span className={`px-1.5 py-0.5 rounded font-mono ${lowScore ? "bg-red-900/40 text-red-300" : "bg-green-900/40 text-green-300"}`}>
            {t("reviews.subtask_review_score", { score: reviewResult.score })}
          </span>
          {lowScore && retryEvidenceHref ? (
            <a
              href={retryEvidenceHref}
              className="inline-flex items-center rounded-md border border-orange-700/60 bg-orange-950/30 px-2 py-1 text-orange-200 hover:border-orange-500 hover:text-orange-100"
            >
              {t("reviews.subtask_retry_evidence")}
            </a>
          ) : null}
        </div>
      ) : null}
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

function TaskHistory({ taskId, task: sourceTask }: { taskId: string; task?: HermesActiveTask }) {
  const { t } = useTranslation();
  const [decision, setDecision] = useState<DecisionLog | null>(null);
  const [hops, setHops] = useState<HermesSnapshotHop[]>([]);
  const [plan, setPlan] = useState<HermesSubTaskView[]>([]);
  const [nodeEvents, setNodeEvents] = useState<RuntimeEventRecord[]>([]);
  const [issueEvents, setIssueEvents] = useState<RuntimeEventRecord[]>([]);
  const [taskState, setTaskState] = useState<HermesTaskState | null>(null);
  const [accumulated, setAccumulated] = useState("");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [tab, setTab] = useState<"subtasks" | "timeline" | "events" | "reviews" | "context">("subtasks");

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);
    Promise.all([
      api.getTaskDecisions({ limit: 2000 }),
      api.getHermesSnapshots(taskId),
      api.getRuntimeEvents({ taskId, type: "GraphNodeEvent", limit: 200 }),
      api.getRuntimeEvents({ taskId, type: "IssueFSMTransition", limit: 50 }),
    ])
      .then(([taskRes, snapshotRes, eventsRes, checklistRes]) => {
        if (cancelled) return;
        const matched = findDecisionForTask(taskRes.decisions || [], taskId);
        setDecision(matched);
        setHops(snapshotRes.snapshots || []);
        setPlan(snapshotRes.latest_plan || []);
        setTaskState(snapshotRes.task || null);
        setAccumulated(snapshotRes.accumulated || "");
        setNodeEvents((eventsRes.events || []).slice().reverse());
        setIssueEvents((checklistRes.events || []).slice().reverse());
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

  const runTask = decision?.unified_task || null;
  const reviews = runTask?.reviews || [];
  const checklistEvent = latestChecklistEvent(issueEvents);
  const checklist = checklistStatusLabel(checklistEvent);
  const diagnostics = diagnosticsFromEvents(issueEvents);
  const usageSummary = summarizeTaskUsage(taskState);
  const totalTokens = (runTask?.total_input_tokens || 0) + (runTask?.total_output_tokens || 0);
  const reviewScore = reviews.length > 0 ? reviews[0].overall_score : undefined;
  const artifactCount = countArtifacts(runTask);
  const commitCount = decision?.git_state?.commit_hash ? 1 : 0;
  const latestReview = reviews[0] || null;
  const retryEvidenceHref = retryEvidenceLink(taskId);
  const reviewSubTaskMap = new Map(
    (latestReview?.sub_task_results || []).map((subTask) => [subTask.sub_task_id, subTask] as const),
  );

  const summaryCards = [
    { label: t("hermes_tasks.summary.tokens"), value: formatTokens(totalTokens) },
    { label: t("hermes_tasks.summary.cost"), value: runTask ? `$${runTask.total_cost_usd.toFixed(2)}` : "—" },
    { label: t("hermes_tasks.summary.duration"), value: formatCompactDuration(runTask?.started_at, runTask?.ended_at) },
    { label: t("hermes_tasks.summary.phases"), value: String(Math.max(hops.length, plan.length || 0)) },
    { label: t("hermes_tasks.summary.commits"), value: decision ? String(commitCount) : "—" },
    { label: t("hermes_tasks.summary.cache"), value: usageSummary.inputTokens > 0 ? formatPercent(usageSummary.cacheHitPercent) : t("hermes_tasks.summary.cache_unknown") },
  ];

  if (loading) {
    return (
      <div className="text-sm text-gray-500 flex items-center gap-2 py-2">
        <Loader2 className="w-4 h-4 animate-spin" />
        {t("hermes_tasks.loading_inspector")}
      </div>
    );
  }
  if (error) {
    return <div className="text-sm text-red-400 py-2">{error}</div>;
  }

  return (
    <div className="space-y-4">
      <div className="grid grid-cols-2 lg:grid-cols-3 xl:grid-cols-6 gap-3">
        {summaryCards.map((card) => (
          <div key={card.label} className="rounded-lg border border-gray-800 bg-black/20 p-3">
            <div className="text-[10px] uppercase tracking-[0.2em] text-gray-500">{card.label}</div>
            <div className="mt-2 text-xl font-mono font-bold text-white">{card.value}</div>
          </div>
        ))}
      </div>

      <div className="grid grid-cols-1 xl:grid-cols-3 gap-3">
        <div className="card p-4 xl:col-span-2">
          <div className="flex items-center justify-between gap-3 mb-3">
            <h3 className="text-sm font-semibold text-gray-200">{t("hermes_tasks.phase_timeline")}</h3>
            <StatusBadge variant={reviewScore === undefined ? "neutral" : summaryVariant(reviewScore)} size="sm" dot>
              {runTask?.status || t("common.unknown")}
            </StatusBadge>
          </div>
          {hops.length === 0 ? (
            <div className="text-sm text-gray-500">{t("hermes_tasks.no_phase_timeline")}</div>
          ) : (
            <div className="space-y-2">
              {hops.map((hop) => <HopRow key={hop.snapshot_id} hop={hop} />)}
            </div>
          )}
        </div>

        <div className="space-y-3">
          <div className="card p-4">
            <div className="flex items-center justify-between gap-2 mb-3">
              <h3 className="text-sm font-semibold text-gray-200">{t("hermes_tasks.checklist_status")}</h3>
              <StatusBadge variant={checklist.variant} size="sm" dot>
                {t(`hermes_tasks.checklist_status_values.${checklist.label}`, { defaultValue: checklist.label })}
              </StatusBadge>
            </div>
            {checklistEvent ? (
              <div className="space-y-1 text-xs text-gray-400">
                <div>{checklistEvent.payload?.reason ? String(checklistEvent.payload.reason) : t("hermes_tasks.no_checklist_reason")}</div>
                <div>
                  {t("hermes_tasks.checklist_counts", {
                    checked: String(Number(checklistEvent.payload?.checked_count || 0)),
                    total: String(Number(checklistEvent.payload?.checklist_total || 0)),
                    unchecked: String(Number(checklistEvent.payload?.unchecked_count || 0)),
                  })}
                </div>
                <div>{String(checklistEvent.payload?.source || checklistEvent.type)}</div>
                {checklistEvent.payload?.retry_action ? (
                  <div className="font-mono text-orange-300">
                    {t("hermes_tasks.retry_action_label")}: {String(checklistEvent.payload.retry_action)}
                  </div>
                ) : null}
                <div className="text-gray-600 font-mono">{formatExact(checklistEvent.timestamp)}</div>
              </div>
            ) : (
              <div className="text-sm text-gray-500">{t("hermes_tasks.no_checklist_status")}</div>
            )}
          </div>

          <div className="card p-4">
            <div className="flex items-center justify-between gap-2 mb-3">
              <h3 className="text-sm font-semibold text-gray-200">{t("hermes_tasks.issueops_diagnostics")}</h3>
              <StatusBadge
                variant={diagnostics.closeEvent || diagnostics.closeGuardEvent ? "error" : diagnostics.syncEvent ? "warning" : "neutral"}
                size="sm"
                dot
              >
                {diagnostics.closeEvent
                  ? t("hermes_tasks.issueops_close_failed")
                  : diagnostics.closeGuardEvent
                    ? t("hermes_tasks.issueops_close_guard")
                    : diagnostics.syncEvent
                      ? t("hermes_tasks.issueops_checklist")
                      : t("hermes_tasks.issueops_idle")}
              </StatusBadge>
            </div>
            {diagnostics.syncEvent || diagnostics.closeEvent || diagnostics.closeGuardEvent ? (
              <div className="space-y-2 text-xs text-gray-400">
                {diagnostics.syncEvent ? (
                  <div className="rounded-md border border-gray-800/60 bg-black/20 p-2">
                    <div className="uppercase tracking-wide text-gray-500 mb-1">{t("hermes_tasks.issueops_checklist")}</div>
                    <div className="text-gray-300">{String(diagnostics.syncEvent.payload?.reason || t("hermes_tasks.no_checklist_reason"))}</div>
                    <div className="font-mono text-gray-500 mt-1">
                      {String(diagnostics.syncEvent.payload?.source || diagnostics.syncEvent.type)} ·{" "}
                      {formatExact(diagnostics.syncEvent.timestamp)}
                    </div>
                    {diagnostics.syncEvent.payload?.retry_action ? (
                      <div className="mt-1 font-mono text-orange-300">
                        {t("hermes_tasks.retry_action_label")}: {String(diagnostics.syncEvent.payload.retry_action)}
                      </div>
                    ) : null}
                  </div>
                ) : null}
                {diagnostics.closeEvent ? (
                  <div className="rounded-md border border-red-800/60 bg-red-950/20 p-2">
                    <div className="uppercase tracking-wide text-red-300 mb-1">{t("hermes_tasks.issueops_close_failed")}</div>
                    <div className="text-red-200">{String(diagnostics.closeEvent.payload?.reason || diagnostics.closeEvent.payload?.event || t("common.unknown"))}</div>
                    <div className="font-mono text-red-300 mt-1">
                      {String(diagnostics.closeEvent.payload?.source || diagnostics.closeEvent.type)} ·{" "}
                      {formatExact(diagnostics.closeEvent.timestamp)}
                    </div>
                  </div>
                ) : null}
                {!diagnostics.closeEvent && diagnostics.closeGuardEvent ? (
                  <div className="rounded-md border border-yellow-800/60 bg-yellow-950/20 p-2">
                    <div className="uppercase tracking-wide text-yellow-300 mb-1">{t("hermes_tasks.issueops_close_guard")}</div>
                    <div className="text-yellow-100">{String(diagnostics.closeGuardEvent.payload?.reason || t("common.unknown"))}</div>
                    <div className="font-mono text-yellow-300 mt-1">
                      {String(diagnostics.closeGuardEvent.payload?.source || diagnostics.closeGuardEvent.type)} ·{" "}
                      {formatExact(diagnostics.closeGuardEvent.timestamp)}
                    </div>
                  </div>
                ) : null}
              </div>
            ) : (
              <div className="text-sm text-gray-500">{t("hermes_tasks.issueops_no_diagnostics")}</div>
            )}
          </div>

          <div className="card p-4">
            <div className="flex items-center justify-between gap-2 mb-3">
              <h3 className="text-sm font-semibold text-gray-200">{t("hermes_tasks.token_attribution")}</h3>
              <StatusBadge variant="neutral" size="sm" dot>
                {usageSummary.phaseUsages.length > 0 ? t("hermes_tasks.token_attribution_phase") : t("hermes_tasks.token_attribution_model")}
              </StatusBadge>
            </div>
            <div className="grid grid-cols-2 gap-2 text-xs">
              <div className="rounded-md border border-gray-800/60 bg-black/20 p-2">
                <div className="text-gray-500 uppercase tracking-wide">{t("hermes_tasks.summary.tokens")}</div>
                <div className="mt-1 font-mono text-gray-200">{formatTokens(usageSummary.inputTokens + usageSummary.outputTokens)}</div>
              </div>
              <div className="rounded-md border border-gray-800/60 bg-black/20 p-2">
                <div className="text-gray-500 uppercase tracking-wide">{t("hermes_tasks.summary.cache")}</div>
                <div className="mt-1 font-mono text-gray-200">{formatPercent(usageSummary.cacheHitPercent)}</div>
              </div>
              <div className="rounded-md border border-gray-800/60 bg-black/20 p-2">
                <div className="text-gray-500 uppercase tracking-wide">{t("hermes_tasks.summary.cost")}</div>
                <div className="mt-1 font-mono text-gray-200">${usageSummary.totalCostUSD.toFixed(2)}</div>
              </div>
              <div className="rounded-md border border-gray-800/60 bg-black/20 p-2">
                <div className="text-gray-500 uppercase tracking-wide">{t("hermes_tasks.summary.phases")}</div>
                <div className="mt-1 font-mono text-gray-200">{String(usageSummary.phaseUsages.length || usageSummary.modelUsages.length)}</div>
              </div>
            </div>
            {usageSummary.phaseUsages.length > 0 ? (
              <div className="mt-3 space-y-2">
                {usageSummary.phaseUsages.map((phase) => (
                  <div key={`${phase.phase}:${phase.model}`} className="rounded-md border border-gray-800/60 bg-black/20 p-2">
                    <div className="flex items-center justify-between gap-2 text-xs">
                      <span className="font-mono text-gray-200">{phase.phase}</span>
                      <span className="font-mono text-gray-500">{phase.model || "—"}</span>
                    </div>
                    <div className="mt-1 flex flex-wrap gap-3 text-[11px] text-gray-500">
                      <span>{formatTokens(phase.input_tokens + phase.output_tokens)} tokens</span>
                      <span>{formatPercent(phase.input_tokens > 0 ? (phase.cache_read_input_tokens || 0) * 100 / phase.input_tokens : 0)} cache hit</span>
                      <span>${phase.cost_usd.toFixed(2)}</span>
                    </div>
                  </div>
                ))}
              </div>
            ) : usageSummary.modelUsages.length > 0 ? (
              <div className="mt-3 space-y-2">
                {usageSummary.modelUsages.slice(0, 3).map((model) => (
                  <div key={model.model} className="rounded-md border border-gray-800/60 bg-black/20 p-2">
                    <div className="flex items-center justify-between gap-2 text-xs">
                      <span className="font-mono text-gray-200">{model.model}</span>
                      <span className="font-mono text-gray-500">{model.call_count}×</span>
                    </div>
                    <div className="mt-1 flex flex-wrap gap-3 text-[11px] text-gray-500">
                      <span>{formatTokens(model.input_tokens + model.output_tokens)} tokens</span>
                      <span>{formatCacheTokens(model.cache_read_input_tokens)} cache read</span>
                      <span>${model.cost_usd.toFixed(2)}</span>
                    </div>
                  </div>
                ))}
              </div>
            ) : (
              <div className="mt-3 text-sm text-gray-500">{t("hermes_tasks.token_attribution_empty")}</div>
            )}
          </div>

          <div className="card p-4">
            <div className="flex items-center justify-between gap-2 mb-3">
              <h3 className="text-sm font-semibold text-gray-200">{t("hermes_tasks.run_links")}</h3>
              {sourceTask?.github_issue_number ? (
                sourceTask.github_url ? (
                  <a
                    href={sourceTask.github_url}
                    target="_blank"
                    rel="noreferrer"
                    className="text-xs text-primary hover:text-primary-light inline-flex items-center gap-1"
                  >
                    #{sourceTask.github_issue_number}
                    <ExternalLink className="w-3 h-3" />
                  </a>
                ) : (
                  <span className="text-xs text-primary">#{sourceTask.github_issue_number}</span>
                )
              ) : null}
            </div>
            <div className="space-y-2 text-xs text-gray-400">
              <div>{t("hermes_tasks.run_links_task", { taskId })}</div>
              {runTask?.project_dir && <div className="font-mono text-gray-500 truncate">{runTask.project_dir}</div>}
              {decision?.git_state?.commit_hash && (
                <div className="font-mono text-gray-500">
                  {t("timeline.detail.commit")}: {decision.git_state.commit_hash.slice(0, 8)}
                </div>
              )}
              {artifactCount > 0 && (
                <div>{t("hermes_tasks.evidence_artifacts", { count: artifactCount })}</div>
              )}
              <div className="flex flex-wrap gap-2 pt-1">
                <a href={`/reviews?focus=${encodeURIComponent(taskId)}`} className="inline-flex items-center gap-1 text-primary hover:text-primary-light">
                  {t("hermes_tasks.open_reviews")}
                  <ArrowRight className="w-3 h-3" />
                </a>
                <a href={`/issues-runs?focus=${encodeURIComponent(taskId)}`} className="inline-flex items-center gap-1 text-primary hover:text-primary-light">
                  {t("hermes_tasks.open_issue_trace")}
                  <ArrowRight className="w-3 h-3" />
                </a>
              </div>
            </div>
          </div>
        </div>
      </div>

      <div className="flex flex-wrap gap-2 border-b border-gray-800 pb-1">
        <TabButton active={tab === "subtasks"} onClick={() => setTab("subtasks")}>
          {t("hermes_tasks.tab_subtasks")} ({plan.length})
        </TabButton>
        <TabButton active={tab === "timeline"} onClick={() => setTab("timeline")}>
          {t("hermes_tasks.tab_timeline")} ({hops.length})
        </TabButton>
        <TabButton active={tab === "events"} onClick={() => setTab("events")}>
          {t("hermes_tasks.tab_events")} ({nodeEvents.length})
        </TabButton>
        <TabButton active={tab === "reviews"} onClick={() => setTab("reviews")}>
          {t("hermes_tasks.tab_reviews")} ({reviews.length})
        </TabButton>
        <TabButton active={tab === "context"} onClick={() => setTab("context")}>
          {t("hermes_tasks.tab_context")}
        </TabButton>
      </div>

      {tab === "subtasks" && (
        <div className="space-y-2">
          {plan.length === 0 ? (
            <div className="text-sm text-gray-500">{t("hermes_tasks.no_subtasks")}</div>
          ) : (
            plan.map((sub, idx) => (
              <SubTaskCard
                key={sub.id || idx}
                sub={sub}
                idx={idx}
                reviewResult={reviewSubTaskMap.get(sub.id)}
                retryEvidenceHref={retryEvidenceHref}
              />
            ))
          )}
        </div>
      )}

      {tab === "timeline" && (
        <div className="space-y-2">
          {hops.length === 0 ? (
            <div className="text-sm text-gray-500">{t("hermes_tasks.no_phase_timeline")}</div>
          ) : (
            hops.map((hop) => <HopRow key={hop.snapshot_id} hop={hop} />)
          )}
        </div>
      )}

      {tab === "events" && (
        <div className="space-y-2">
          {nodeEvents.length === 0 ? (
            <div className="text-sm text-gray-500">{t("hermes_tasks.no_graph_events")}</div>
          ) : (
            nodeEvents.map((event, idx) => (
              <GraphNodeEventRow key={`${event.timestamp}-${idx}`} event={event} />
            ))
          )}
        </div>
      )}

      {tab === "reviews" && (
        <div className="space-y-3">
          {reviews.length === 0 ? (
            <div className="text-sm text-gray-500">{t("hermes_tasks.no_reviews")}</div>
          ) : (
            reviews.map((review, idx) => (
              <div key={review.id || review.created_at || idx} className="rounded-md border border-gray-800 bg-gray-900/30 p-3 space-y-2">
                <div className="flex flex-wrap items-center gap-2">
                  <StatusBadge
                    variant={review.verdict === "pass" ? "success" : review.verdict === "fail" ? "error" : "warning"}
                    size="sm"
                  >
                    {review.verdict || t("common.unknown")}
                  </StatusBadge>
                  <span className="text-xs text-gray-400">{review.reviewer_model || t("common.unknown")}</span>
                  {review.source ? (
                    <span className={`px-1.5 py-0.5 rounded text-[10px] font-mono ${review.source === "retry" ? "bg-orange-900/40 text-orange-300" : "bg-gray-800 text-gray-400"}`}>
                      {review.source}
                    </span>
                  ) : null}
                  <span className="text-xs text-gray-500 font-mono ml-auto">{review.overall_score}/100</span>
                </div>
                {review.feedback_text && <p className="text-sm text-gray-300 whitespace-pre-wrap">{review.feedback_text}</p>}
                {review.issue_tags?.length > 0 && (
                  <div className="flex flex-wrap gap-1">
                    {review.issue_tags.map((tag) => (
                      <span key={tag} className="px-1.5 py-0.5 rounded bg-gray-800 text-[10px] text-gray-400">
                        {tag}
                      </span>
                    ))}
                  </div>
                )}
                <ReviewSubTaskTable subTaskResults={review.sub_task_results || []} retryEvidenceHref={retryEvidenceHref} />
              </div>
            ))
          )}
        </div>
      )}

      {tab === "context" && (
        <div>
          {accumulated ? (
            <pre className="text-[12px] text-gray-300 whitespace-pre-wrap break-words bg-black/30 p-3 rounded font-mono leading-relaxed max-h-[60vh] overflow-y-auto">
              {accumulated}
            </pre>
          ) : (
            <div className="text-sm text-gray-500">{t("hermes_tasks.no_accumulated")}</div>
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
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const [tasks, setTasks] = useState<HermesActiveTask[]>([]);
  const [status, setStatus] = useState<StatusFilter>("");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [expanded, setExpanded] = useState<string | null>(null);
  const [total, setTotal] = useState(0);
  // counts is fetched separately from /api/hermes/stats so the chip
  // numbers reflect ALL tasks in the window, not the currently-filtered
  // subset. Without this, switching to "Interrupted" made every other
  // chip read 0 because they were computed from the loaded list only.
  const [counts, setCounts] = useState<Record<string, number>>({});
  const [allCount, setAllCount] = useState(0);
  const selectedTaskId = searchParams.get("task_id") || "";

  const PAGE_SIZE = 100;

  // load fetches the first page of the current filter, replacing the
  // task list. Used on initial mount, filter change, and manual refresh.
  const load = () => {
    setLoading(true);
    setError(null);
    api
      .getHermesTasks({ status: status || undefined, limit: PAGE_SIZE, offset: 0 })
      .then((res) => {
        setTasks(res.tasks || []);
        setTotal(res.total || 0);
      })
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
      .finally(() => setLoading(false));
  };

  // loadMore appends the next page using the current task count as the
  // offset. The backend's stable order (updated_at DESC, then task_id)
  // means this gives a deterministic page boundary as long as no new
  // tasks land mid-pagination — good enough for an admin-facing
  // history view.
  const loadMore = () => {
    setLoading(true);
    setError(null);
    api
      .getHermesTasks({ status: status || undefined, limit: PAGE_SIZE, offset: tasks.length })
      .then((res) => {
        setTasks((prev) => [...prev, ...(res.tasks || [])]);
        setTotal(res.total || 0);
      })
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
      .finally(() => setLoading(false));
  };

  // Fetch counts once (independent of which filter is active) so chip
  // values stay stable when the user changes the status filter.
  // Refreshes alongside the list when the manual refresh button is
  // pressed.
  const loadCounts = () => {
    api
      .getHermesStats(90)
      .then((s) => {
        setAllCount(s.totals.total);
        setCounts({
          done: s.totals.done,
          failed: s.totals.failed,
          interrupted: s.totals.interrupted,
          executing: s.totals.executing,
          planning: s.totals.planning,
        });
      })
      .catch(() => {
        // non-fatal — chip numbers fall back to "?"
      });
  };

  useEffect(() => {
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [status]);

  useEffect(() => {
    loadCounts();
  }, []);

  useEffect(() => {
    if (selectedTaskId) {
      setExpanded(selectedTaskId);
    }
  }, [selectedTaskId]);

  const openInspector = (taskId: string) => {
    navigate(`/run-inspector?task_id=${encodeURIComponent(taskId)}`);
    setExpanded(taskId);
  };

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
          onClick={() => {
            load();
            loadCounts();
          }}
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
          const n = f.value ? counts[f.value] ?? 0 : allCount;
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
                      <button
                        type="button"
                        onClick={(e) => {
                          e.stopPropagation();
                          openInspector(task.task_id);
                        }}
                        className="inline-flex items-center gap-1 rounded-full border border-gray-700 px-2 py-0.5 text-[10px] text-primary hover:border-primary/50"
                      >
                        {t("hermes_tasks.open_inspector")}
                        <ArrowRight className="w-3 h-3" />
                      </button>
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
                  <TaskHistory taskId={task.task_id} task={task} />
                </div>
              )}
            </div>
          );
        })}
      </div>

      {total > tasks.length && (
        <div className="flex flex-col items-center gap-2 py-2">
          <div className="text-xs text-gray-500">
            {t("hermes_tasks.pagination_status", {
              shown: tasks.length,
              total,
            })}
          </div>
          <button
            onClick={loadMore}
            disabled={loading}
            className="btn btn-secondary text-sm inline-flex items-center gap-2"
          >
            {loading ? <Loader2 className="w-3 h-3 animate-spin" /> : null}
            {t("hermes_tasks.pagination_load_more")}
          </button>
        </div>
      )}
    </div>
  );
}
