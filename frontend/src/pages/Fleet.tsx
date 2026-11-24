import { useMemo, useState } from "react";
import { clsx } from "clsx";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Inbox, Plus } from "lucide-react";
import FleetTaskCard from "@/components/fleet/FleetTaskCard";
import FleetUnavailable from "@/components/fleet/FleetUnavailable";
import FleetDispatchForm from "@/components/fleet/FleetDispatchForm";
import { useFleet } from "@/hooks/useFleet";
import { FLEET_COLUMNS, formatCost, groupByColumn } from "@/lib/fleet";
import type { FleetCounts } from "@/types/fleet";

/** How many finished tasks the last column shows before "show all". */
const FINISHED_PREVIEW = 8;

const emptyCounts: FleetCounts = {
  queued: 0, running: 0, review_ready: 0, needs_decision: 0, failed_today: 0, done_today: 0,
};

function Stat({ label, value, tone }: { label: string; value: string | number; tone?: string }) {
  return (
    <div className="rounded-lg border border-gray-800 bg-black/30 px-3 py-2 min-w-[6.5rem]">
      <div className="text-[10px] uppercase tracking-wider text-gray-500">{label}</div>
      <div className={clsx("text-xl font-semibold tabular-nums", tone ?? "text-white")}>{value}</div>
    </div>
  );
}

/** The fleet board: every project's tasks, in columns by status. */
export default function Fleet() {
  const { t } = useTranslation();
  const { loaded, unavailable, error, digest, projects, tasks } = useFleet();
  const [project, setProject] = useState("");
  const [showAllFinished, setShowAllFinished] = useState(false);
  const [dispatching, setDispatching] = useState(false);

  const columns = useMemo(() => {
    const all = Object.values(tasks);
    return groupByColumn(project ? all.filter((task) => task.project_id === project) : all);
  }, [tasks, project]);

  if (!loaded) return <p className="text-sm text-gray-500">{t("common.loading")}</p>;
  if (unavailable) return <FleetUnavailable reason={unavailable} error={error} />;

  // With a project picked, the header shows that project's numbers.
  const scoped = project ? digest?.projects.find((p) => p.project_id === project) : null;
  const counts = (project ? scoped?.counts : digest?.totals) ?? emptyCounts;
  const today = (project ? scoped?.today : digest?.today) ?? null;
  const waiting = counts.review_ready + counts.needs_decision;

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-bold text-white">{t("fleet.board.title")}</h1>
          <p className="text-sm text-gray-500">{t("fleet.board.subtitle")}</p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <select
            value={project}
            onChange={(e) => setProject(e.target.value)}
            aria-label={t("fleet.board.project_filter")}
            className="rounded-md border border-gray-700 bg-black/40 px-2 py-1.5 text-sm text-gray-200"
          >
            <option value="">{t("fleet.board.all_projects")}</option>
            {projects.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name || p.id}
              </option>
            ))}
          </select>
          <Link
            to="/fleet/inbox"
            className="inline-flex items-center gap-1.5 rounded-md border border-warning/30 bg-warning/10 px-3 py-1.5 text-sm text-warning hover:bg-warning/20"
          >
            <Inbox className="w-4 h-4" />
            {t("fleet.board.inbox", { count: waiting })}
          </Link>
          <button
            type="button"
            onClick={() => setDispatching((v) => !v)}
            disabled={projects.length === 0}
            className="inline-flex items-center gap-1.5 rounded-md border border-primary/30 bg-primary/15 px-3 py-1.5 text-sm text-primary hover:bg-primary/25 disabled:opacity-50"
          >
            <Plus className="w-4 h-4" />
            {t("fleet.board.dispatch")}
          </button>
        </div>
      </div>

      <div className="flex flex-wrap gap-2" data-testid="fleet-counts">
        <Stat label={t("fleet.counts.running")} value={counts.running} tone="text-primary" />
        <Stat label={t("fleet.counts.queued")} value={counts.queued} />
        <Stat label={t("fleet.counts.review_ready")} value={counts.review_ready} tone="text-warning" />
        <Stat label={t("fleet.counts.needs_decision")} value={counts.needs_decision} tone="text-warning" />
        <Stat label={t("fleet.counts.done_today")} value={counts.done_today} tone="text-success" />
        <Stat label={t("fleet.counts.failed_today")} value={counts.failed_today} tone="text-error" />
        <Stat label={t("fleet.counts.cost_today")} value={formatCost(today?.cost_usd ?? 0)} />
      </div>

      {dispatching && (
        <FleetDispatchForm projects={projects} defaultProject={project} onClose={() => setDispatching(false)} />
      )}

      {projects.length === 0 ? (
        <div className="rounded-lg border border-gray-800 p-8 text-center text-sm text-gray-400">
          {t("fleet.board.no_projects")}{" "}
          <Link to="/fleet/projects" className="text-primary hover:underline">
            {t("fleet.board.go_projects")}
          </Link>
        </div>
      ) : (
        // One column per row on a phone, side by side from a tablet up.
        <div className="grid grid-cols-1 gap-4 md:grid-cols-3 xl:grid-cols-5" data-testid="fleet-board">
          {FLEET_COLUMNS.map((column) => {
            const all = columns[column.key] ?? [];
            const limited = column.key === "finished" && !showAllFinished;
            const shown = limited ? all.slice(0, FINISHED_PREVIEW) : all;
            return (
              <section key={column.key} className="min-w-0" data-column={column.key}>
                <header className="mb-2 flex items-center justify-between px-1">
                  <h2 className="text-xs font-semibold uppercase tracking-wider text-gray-400">
                    {t(`fleet.columns.${column.key}`)}
                  </h2>
                  <span className="text-xs tabular-nums text-gray-600">{all.length}</span>
                </header>
                <div className="space-y-2">
                  {shown.map((task) => (
                    <FleetTaskCard key={task.id} task={task} showProject={!project} />
                  ))}
                  {all.length === 0 && (
                    <div className="rounded-lg border border-dashed border-gray-800 px-3 py-6 text-center text-xs text-gray-600">
                      {t("fleet.board.empty_column")}
                    </div>
                  )}
                  {limited && all.length > shown.length && (
                    <button
                      type="button"
                      onClick={() => setShowAllFinished(true)}
                      className="w-full rounded-md border border-gray-800 py-1.5 text-xs text-gray-400 hover:text-gray-200"
                    >
                      {t("fleet.board.show_all", { count: all.length })}
                    </button>
                  )}
                </div>
              </section>
            );
          })}
        </div>
      )}
    </div>
  );
}
