import ModelChoice from "@/components/fleet/ModelChoice";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { ArrowLeft, CheckCircle2, ExternalLink, XCircle } from "lucide-react";
import CollapsiblePanel from "@/components/CollapsiblePanel";
import DiffViewer from "@/components/DiffViewer";
import StatusBadge from "@/components/StatusBadge";
import TimelineEntry from "@/components/TimelineEntry";
import ToolCallGantt from "@/components/ToolCallGantt";
import FleetDecisionContext from "@/components/fleet/FleetDecisionContext";
import FleetDeliverySummary from "@/components/fleet/FleetDeliverySummary";
import FleetActions from "@/components/fleet/FleetActions";
import FleetUnavailable from "@/components/fleet/FleetUnavailable";
import LivenessDot from "@/components/fleet/LivenessDot";
import { fleetApi } from "@/lib/fleetApi";
import {
  describeEvent,
  firstLine,
  formatCost,
  formatDuration,
  isActive,
  parsePatch,
  runsFromEvents,
  statusTone,
  taskElapsedMs,
  toolExecutionsFromEvents,
} from "@/lib/fleet";
import { useFleet } from "@/hooks/useFleet";
import { useNow } from "@/hooks/useNow";
import type { FleetDelivery, FleetDiff, FleetEvent, FleetVerification } from "@/types/fleet";

/** How often a live task's events are polled, beneath the WebSocket. */
const POLL_MS = 4000;
/** Event types too chatty for the plain list; they show in the runs instead. */
const QUIET = new Set(["agent.tool_use", "agent.tool_result", "agent.text", "agent.init"]);

function when(ts: string | null): string {
  return ts ? new Date(ts).toLocaleTimeString([], { hour12: false }) : "";
}

/** One task: its history as it happens, its changes, its checks, its commands. */
export default function FleetTaskPage() {
  const { t } = useTranslation();
  const { id = "" } = useParams();
  const { loaded, unavailable, error, tasks, projects, refreshTask } = useFleet();
  const task = tasks[id];
  const now = useNow(5_000);

  const [events, setEvents] = useState<FleetEvent[]>([]);
  const [diff, setDiff] = useState<FleetDiff | null>(null);
  const [checks, setChecks] = useState<FleetVerification[]>([]);
  const [deliveryRead, setDeliveryRead] = useState<{ key: string; result: FleetDelivery | null } | null>(null);
  const [missing, setMissing] = useState(false);
  const cursor = useRef(0);
  const loading = useRef(false);

  // Reads whatever events are new since the last read. Called on a timer
  // while the task is live and whenever the store learns the task changed.
  const pullEvents = useCallback(async () => {
    if (!id || loading.current) return;
    loading.current = true;
    try {
      for (let page = 0; page < 20; page++) {
        const got = await fleetApi.getTaskEvents(id, { afterId: cursor.current, limit: 500 });
        cursor.current = got.next_after_id || cursor.current;
        if (got.events?.length) setEvents((cur) => [...cur, ...got.events]);
        if (!got.has_more) break;
      }
    } catch {
      // Keep what is shown; the next pull tries again.
    } finally {
      loading.current = false;
    }
  }, [id]);

  // A task opened by link may not be in the store yet.
  useEffect(() => {
    if (!id || !loaded || unavailable) return;
    if (!task) {
      fleetApi
        .getTask(id)
        .then(() => void refreshTask(id))
        .catch(() => setMissing(true));
    }
  }, [id, loaded, unavailable, task, refreshTask]);

  useEffect(() => {
    cursor.current = 0;
    setEvents([]);
    setDiff(null);
    setChecks([]);
    setDeliveryRead(null);
    setMissing(false);
  }, [id]);

  // The store refreshes the task on every fleet event about it, so a change
  // of updated_at is the signal that there is something new to read.
  const stamp = task ? `${task.status}|${task.updated_at}|${task.last_activity_at}` : "";
  useEffect(() => {
    if (!task) return;
    void pullEvents();
  }, [stamp, task, pullEvents]);

  const shape = task?.shape;
  const deliveryKey = JSON.stringify([id, stamp]);
  useEffect(() => {
    if (!id || !shape) return;
    let active = true;
    void fleetApi.getTaskDiff(id).then((result) => {
      if (active) setDiff(result);
    }).catch(() => {});
    void fleetApi.getTaskVerifications(id).then((result) => {
      if (active) setChecks(result);
    }).catch(() => {});
    if (shape === "ship") {
      void fleetApi.getTaskDelivery(id).then((result) => {
        if (active) setDeliveryRead({ key: deliveryKey, result });
      }).catch(() => {
        if (active) setDeliveryRead({ key: deliveryKey, result: null });
      });
    }
    return () => { active = false; };
  }, [id, shape, stamp, deliveryKey]);

  const live = task ? isActive(task.status) : false;
  useEffect(() => {
    if (!live) return;
    const timer = setInterval(() => void pullEvents(), POLL_MS);
    return () => clearInterval(timer);
  }, [live, pullEvents]);

  const runs = useMemo(() => (task ? runsFromEvents(task, events) : []), [task, events]);
  const tools = useMemo(() => toolExecutionsFromEvents(events), [events]);
  const files = useMemo(() => parsePatch(diff?.patch ?? ""), [diff]);
  const lifecycle = useMemo(() => events.filter((e) => !QUIET.has(e.type)), [events]);
  const latestAttempt = checks.reduce((max, c) => Math.max(max, c.attempt), 0);
  // A ship task is checked only with its project's commands: with none set,
  // "not run yet" would promise a result that is never coming.
  const project = task ? projects.find((p) => p.id === task.project_id) : undefined;
  const noCommands = task?.shape === "ship" && !!project && (project.verify_commands ?? []).length === 0;

  if (!loaded) return <p className="text-sm text-gray-500">{t("common.loading")}</p>;
  if (unavailable) return <FleetUnavailable reason={unavailable} error={error} />;
  if (!task) {
    return (
      <div className="space-y-3">
        <Link to="/fleet" className="inline-flex items-center gap-1 text-sm text-gray-400 hover:text-gray-200">
          <ArrowLeft className="w-4 h-4" />
          {t("fleet.task.back")}
        </Link>
        <p className="text-sm text-gray-400">{missing ? t("fleet.task.not_found", { id }) : t("common.loading")}</p>
      </div>
    );
  }

  const goalTitle = firstLine(task.goal);
  const issuePrefix = `Work on GitHub issue #${task.issue_number}:`;
  const title = task.issue_number > 0 && goalTitle.startsWith(issuePrefix)
    ? goalTitle.slice(issuePrefix.length).trimStart()
    : goalTitle;
  const needsDecision = task.status === "needs_decision";
  const delivery = deliveryRead?.key === deliveryKey ? deliveryRead.result : null;
  const deliveryLoading = shape === "ship" && deliveryRead?.key !== deliveryKey;
  const currentDelivery = delivery?.task_id === id && delivery.status === task.status ? delivery : null;

  return (
    <div className="space-y-5" data-testid="fleet-task">
      <Link to="/fleet" className="inline-flex items-center gap-1 text-sm text-gray-400 hover:text-gray-200">
        <ArrowLeft className="w-4 h-4" />
        {t("fleet.task.back")}
      </Link>

      <header className="space-y-3">
        <div className="flex flex-wrap items-center gap-2">
          <StatusBadge variant={statusTone(task.status)}>{t(`fleet.status.${task.status}`)}</StatusBadge>
          <LivenessDot liveness={task.liveness} withLabel />
          <span className="text-xs text-gray-500">
            {t(`fleet.shape.${task.shape}`)} · {task.project_name || task.project_id} · {t(`fleet.trust.${task.trust_mode}`)}
          </span>
          <code className="text-xs text-gray-600">{task.id}</code>
        </div>
        <h1 className="text-xl font-semibold text-white break-words">
          {task.issue_number > 0 && <span className="text-primary mr-2">#{task.issue_number}</span>}
          {title}
        </h1>
        {(task.startup_recovered || task.startup_resume_capped) && (
          <p className="text-xs text-gray-400">
            {t(task.startup_resume_capped ? "fleet.task.startup_resume_capped" : "fleet.task.startup_recovered")}
          </p>
        )}
        {task.status_reason && !needsDecision && <p className="text-sm text-gray-400 break-words">{task.status_reason}</p>}
        <dl className="flex flex-wrap gap-x-6 gap-y-1 text-xs text-gray-500">
          <div>
            <dt className="inline">{t("fleet.task.elapsed")}: </dt>
            <dd className="inline text-gray-300 tabular-nums">{formatDuration(taskElapsedMs(task, now))}</dd>
          </div>
          <div>
            <dt className="inline">{t("fleet.task.cost")}: </dt>
            <dd className="inline text-gray-300 tabular-nums">
              {formatCost(task.cost_usd)}
              {task.budget_cost_usd > 0 && ` / ${formatCost(task.budget_cost_usd)}`}
            </dd>
          </div>
          <div>
            <dt className="inline">{t("fleet.task.tokens")}: </dt>
            <dd className="inline text-gray-300 tabular-nums">
              {Math.round(task.input_tokens).toLocaleString()} / {Math.round(task.output_tokens).toLocaleString()}
            </dd>
          </div>
          {task.model && (
            <div>
              <dt className="inline">{t("fleet.task.model")}: </dt>
              <dd className="inline text-gray-300">
                {task.model}
                {task.model_source && (
                  <span className="ml-2 text-xs text-gray-500" data-testid="model-choice">
                    (<ModelChoice task={task} />)
                  </span>
                )}
              </dd>
            </div>
          )}
          {task.branch && (
            <div>
              <dt className="inline">{t("fleet.task.branch")}: </dt>
              <dd className="inline text-gray-300 font-mono">{task.branch}</dd>
            </div>
          )}
          {task.pr_url && (
            <a href={task.pr_url} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 text-primary hover:underline">
              <ExternalLink className="w-3 h-3" />
              PR #{task.pr_number}
            </a>
          )}
        </dl>
        {needsDecision && <FleetDecisionContext task={task} delivery={currentDelivery} deliveryLoading={deliveryLoading} />}
        {!needsDecision && currentDelivery && (
          <FleetDeliverySummary
            delivery={currentDelivery}
            onShowDiff={() => document.getElementById("fleet-task-diff")?.scrollIntoView({ behavior: "smooth" })}
          />
        )}
        <FleetActions task={task} />
        {task.goal.trim() !== goalTitle && (
          <CollapsiblePanel title={t("fleet.task.original_brief")}>
            <p className="whitespace-pre-wrap break-words p-4 text-sm text-gray-300">{task.goal}</p>
          </CollapsiblePanel>
        )}
        {needsDecision && currentDelivery && (
          <CollapsiblePanel title={t("fleet.delivery.title")}>
            <FleetDeliverySummary
              delivery={currentDelivery}
              onShowDiff={() => document.getElementById("fleet-task-diff")?.scrollIntoView({ behavior: "smooth" })}
            />
          </CollapsiblePanel>
        )}
      </header>

      <CollapsiblePanel title={t("fleet.task.timeline")} badge={runs.length} defaultOpen>
        <div className="p-4 space-y-4">
          {tools.length > 0 && <ToolCallGantt tools={tools} />}
          {runs.length === 0 && <p className="text-sm text-gray-500">{t("fleet.task.no_runs")}</p>}
          <div>
            {runs.map((run) => (
              <TimelineEntry key={run.id} decision={run} />
            ))}
          </div>
          <ol className="space-y-1 border-t border-gray-800 pt-3" data-testid="fleet-events">
            {lifecycle.map((event) => (
              <li key={event.id} className="flex gap-3 text-xs">
                <span className="w-16 shrink-0 font-mono tabular-nums text-gray-600">{when(event.created_at)}</span>
                <span className="w-28 shrink-0 text-gray-500">{event.type}</span>
                <span className="min-w-0 flex-1 text-gray-300 break-words">{describeEvent(event)}</span>
              </li>
            ))}
          </ol>
        </div>
      </CollapsiblePanel>

      {task.shape === "ship" && (
        <div id="fleet-task-diff">
        <CollapsiblePanel title={t("fleet.task.diff")} badge={files.length} defaultOpen={files.length > 0}>
          <div className="p-4 space-y-2">
            {diff?.truncated && <p className="text-xs text-warning">{t("fleet.task.diff_truncated")}</p>}
            {files.length === 0 ? (
              <p className="text-sm text-gray-500">{t("fleet.task.no_diff")}</p>
            ) : (
              <DiffViewer files={files} />
            )}
            {(diff?.untracked?.length ?? 0) > 0 && (
              <div className="text-xs text-gray-400">
                <p className="mb-1 text-gray-500">{t("fleet.task.untracked")}</p>
                <ul className="font-mono space-y-0.5">
                  {diff?.untracked.map((path) => (
                    <li key={path}>{path}</li>
                  ))}
                </ul>
              </div>
            )}
          </div>
        </CollapsiblePanel>
        </div>
      )}

      <CollapsiblePanel title={t("fleet.task.checks")} badge={checks.filter((c) => c.attempt === latestAttempt).length} defaultOpen={checks.length > 0}>
        <div className="p-4 space-y-3">
          {checks.length === 0 && !noCommands && <p className="text-sm text-gray-500">{t("fleet.task.no_checks")}</p>}
          {checks.length === 0 && noCommands && (
            <p className="text-sm text-warning">
              {t("fleet.task.no_verify_commands")}{" "}
              <Link to="/fleet/projects" className="underline hover:text-gray-200">
                {t("fleet.task.open_project_settings")}
              </Link>
            </p>
          )}
          {checks.map((check) => (
            <div key={check.id} className="rounded-md border border-gray-800">
              <div className="flex flex-wrap items-center gap-2 px-3 py-2 text-sm">
                {check.passed ? <CheckCircle2 className="w-4 h-4 text-success" /> : <XCircle className="w-4 h-4 text-error" />}
                <code className="min-w-0 flex-1 break-all text-gray-200">{check.command || check.name}</code>
                <span className="text-xs text-gray-500 tabular-nums">
                  {t("fleet.task.attempt", { n: check.attempt })} · {t("fleet.task.exit_code", { code: check.exit_code })} ·{" "}
                  {formatDuration(check.duration_ms)}
                  {check.timed_out && ` · ${t("fleet.task.timed_out")}`}
                </span>
              </div>
              {(check.output_tail || check.error) && (
                <pre className="max-h-64 overflow-auto border-t border-gray-800 bg-black/40 p-3 text-xs leading-5 text-gray-300 whitespace-pre-wrap break-words">
                  {check.error ? `${check.error}\n` : ""}
                  {check.output_tail}
                </pre>
              )}
            </div>
          ))}
        </div>
      </CollapsiblePanel>
    </div>
  );
}
