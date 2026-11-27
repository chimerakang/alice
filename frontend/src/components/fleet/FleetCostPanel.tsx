import { useEffect, useState } from "react";
import { clsx } from "clsx";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { fleetApi, FleetApiError, type FleetBackendUsage, type FleetBackendQuota } from "@/lib/fleetApi";
import { budgetUse, firstLine, formatCost } from "@/lib/fleet";
import type { FleetCosts } from "@/types/fleet";

const PERIODS = [1, 7, 30];

/** A bar showing how much of a limit is used; red once it is reached. */
function UseBar({ used, limit }: { used: number; limit: number }) {
  const { t } = useTranslation();
  const share = budgetUse(used, limit);
  if (share === null) return <span className="text-xs text-gray-600">{t("fleet.costs.no_limit")}</span>;
  const over = share >= 1;
  return (
    <div className="flex items-center gap-2" data-over={over}>
      <div className="h-1.5 w-28 overflow-hidden rounded-full bg-gray-800">
        <div className={clsx("h-full", over ? "bg-error" : share >= 0.8 ? "bg-warning" : "bg-success")} style={{ width: `${Math.min(100, share * 100)}%` }} />
      </div>
      <span className={clsx("text-xs tabular-nums", over ? "text-error" : "text-gray-400")}>
        {Math.round(share * 100)}% · {formatCost(used)} / {formatCost(limit)}
      </span>
    </div>
  );
}

/** The loaded view, split out so it can be rendered on its own in tests. */
export function FleetCostView({ costs, days }: { costs: FleetCosts; days: number }) {
  const { t } = useTranslation();
  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center gap-x-8 gap-y-2">
        <div>
          <div className="text-[10px] uppercase tracking-wider text-gray-500">{t("fleet.costs.today")}</div>
          <div className="text-xl font-semibold text-white tabular-nums">{formatCost(costs.today?.cost_usd ?? 0)}</div>
        </div>
        <div>
          <div className="text-[10px] uppercase tracking-wider text-gray-500">{t("fleet.costs.period", { days })}</div>
          <div className="text-xl font-semibold text-white tabular-nums">{formatCost(costs.period?.cost_usd ?? 0)}</div>
        </div>
        <div>
          <div className="text-[10px] uppercase tracking-wider text-gray-500">{t("fleet.costs.fleet_daily")}</div>
          <UseBar used={costs.today?.cost_usd ?? 0} limit={costs.daily_limit_usd} />
        </div>
      </div>

      <div className="overflow-x-auto">
        <table className="w-full text-sm" data-testid="fleet-cost-projects">
          <thead>
            <tr className="text-left text-xs text-gray-500">
              <th className="py-1.5 pr-4 font-medium">{t("fleet.costs.project")}</th>
              <th className="py-1.5 pr-4 font-medium">{t("fleet.costs.today")}</th>
              <th className="py-1.5 pr-4 font-medium">{t("fleet.costs.period", { days })}</th>
              <th className="py-1.5 pr-4 font-medium">{t("fleet.costs.daily_use")}</th>
              <th className="py-1.5 font-medium">{t("fleet.costs.task_limit")}</th>
            </tr>
          </thead>
          <tbody>
            {(costs.projects ?? []).map((p) => (
              <tr key={p.project_id} className="border-t border-gray-800/60">
                <td className="py-1.5 pr-4 text-gray-200">{p.name || p.project_id}</td>
                <td className="py-1.5 pr-4 tabular-nums text-gray-300">{formatCost(p.today?.cost_usd ?? 0)}</td>
                <td className="py-1.5 pr-4 tabular-nums text-gray-300">{formatCost(p.period?.cost_usd ?? 0)}</td>
                <td className="py-1.5 pr-4">
                  <UseBar used={p.today?.cost_usd ?? 0} limit={p.daily_limit_usd} />
                </td>
                <td className="py-1.5 tabular-nums text-gray-400">
                  {p.task_limit_usd > 0 ? formatCost(p.task_limit_usd) : t("fleet.costs.no_limit")}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <div className="grid grid-cols-1 gap-5 lg:grid-cols-2">
        <div data-testid="fleet-cost-tasks">
          <h4 className="mb-1.5 text-xs font-semibold uppercase tracking-wider text-gray-400">{t("fleet.costs.by_task")}</h4>
          {(costs.tasks ?? []).length === 0 && <p className="text-xs text-gray-600">{t("fleet.costs.no_tasks")}</p>}
          <ul className="space-y-1">
            {(costs.tasks ?? []).map((task) => (
              <li key={task.id} className="flex items-center gap-3 text-sm">
                <span className="w-16 shrink-0 tabular-nums text-gray-200">{formatCost(task.cost_usd)}</span>
                <Link to={`/fleet/tasks/${task.id}`} className="min-w-0 flex-1 truncate text-gray-300 hover:text-primary">
                  {firstLine(task.goal)}
                </Link>
                <span className="shrink-0 text-xs text-gray-500">{task.project_name || task.project_id}</span>
              </li>
            ))}
          </ul>
        </div>
        <div data-testid="fleet-cost-overruns">
          <h4 className="mb-1.5 text-xs font-semibold uppercase tracking-wider text-gray-400">{t("fleet.costs.overruns")}</h4>
          {(costs.overruns ?? []).length === 0 && <p className="text-xs text-gray-600">{t("fleet.costs.no_overruns")}</p>}
          <ul className="space-y-1.5">
            {(costs.overruns ?? []).map((task) => (
              <li key={task.id} className="text-sm">
                <Link to={`/fleet/tasks/${task.id}`} className="text-gray-300 hover:text-primary">
                  {firstLine(task.goal)}
                </Link>
                <span className="ml-2 text-xs text-gray-500">{task.project_name || task.project_id}</span>
                <p className="text-xs text-error break-words">{task.status_reason}</p>
              </li>
            ))}
          </ul>
        </div>
      </div>
    </div>
  );
}

function FleetBackendUsageView({ days }: { days: number }) {
  const { t } = useTranslation();
  const [usage, setUsage] = useState<{ rows: FleetBackendUsage[]; quota: FleetBackendQuota[] } | { error: string } | null>(null);

  useEffect(() => {
    let stale = false;
    const until = new Date();
    const since = new Date(until.getTime() - days * 86_400_000);
    fleetApi.getBackendUsage(since.toISOString(), until.toISOString()).then(
      ({ backends, quota }) => {
        if (!stale) setUsage({ rows: backends, quota: quota ?? [] });
      },
      (err: unknown) => {
        if (!stale) setUsage({ error: err instanceof Error ? err.message : String(err) });
      },
    );
    return () => { stale = true; };
  }, [days]);

  return (
    <section className="space-y-2">
      <h4 className="text-sm font-semibold text-gray-200">{t("fleet.costs.by_backend", { days })}</h4>
      {!usage && <p className="text-xs text-gray-500">{t("common.loading")}</p>}
      {usage && "error" in usage && <p className="text-xs text-error">{usage.error}</p>}
      {usage && "rows" in usage && usage.quota.length > 0 && (
        <div className="grid gap-2 sm:grid-cols-2">
          {usage.quota.map((row) => (
            <div key={row.backend} className="rounded-md border border-gray-800 p-3 text-xs text-gray-300">
              <p className="font-medium capitalize text-white">{row.backend}</p>
              <p className={row.blocked ? "text-warning" : undefined}>
                {t(row.blocked ? "fleet.costs.quota_blocked" : "fleet.costs.quota_no_block")}
              </p>
              <p>{row.provider_remaining !== undefined
                ? t("fleet.costs.quota_provider", { percent: Math.round(row.provider_remaining * 100) })
                : row.estimated_remaining === undefined
                ? t("fleet.costs.quota_unknown")
                : t("fleet.costs.quota_estimate", { percent: Math.round(row.estimated_remaining * 100) })}</p>
              <p>{row.resets_at
                ? t("fleet.costs.quota_reset", { time: new Date(row.resets_at).toLocaleString() })
                : row.blocked && t("fleet.costs.quota_reset_unknown")}</p>
            </div>
          ))}
        </div>
      )}
      {usage && "rows" in usage && (usage.rows.length === 0 ? (
        <p className="text-xs text-gray-500">{t("fleet.costs.no_backend_usage")}</p>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-left text-sm">
            <thead className="text-xs text-gray-500">
              <tr>
                {["backend", "input_tokens", "output_tokens", "cost_usd", "tasks"].map((column) => (
                  <th key={column} scope="col" className="py-1.5 pr-4">{t(`fleet.costs.backend_columns.${column}`)}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {usage.rows.map((row) => (
                <tr key={row.backend} className="border-t border-gray-800/60 text-gray-300 tabular-nums">
                  <th scope="row" className="py-1.5 pr-4 font-normal">{row.backend || t("fleet.costs.unattributed")}</th>
                  <td className="pr-4">{row.input_tokens}</td>
                  <td className="pr-4">{row.output_tokens}</td>
                  <td className="pr-4">{formatCost(row.cost_usd)}</td>
                  <td className="pr-4">{row.tasks}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ))}
    </section>
  );
}

/**
 * Fleet spending on the Performance page: by project and by task, against
 * the budgets in force, with the tasks a budget has stopped. Shows nothing
 * when the fleet is off.
 */
export default function FleetCostPanel() {
  const { t } = useTranslation();
  const [days, setDays] = useState(7);
  const [loaded, setLoaded] = useState<{ days: number; costs: FleetCosts | null; error: string; off: boolean } | null>(null);

  useEffect(() => {
    let stale = false;
    fleetApi
      .getCosts(days)
      .then((costs) => !stale && setLoaded({ days, costs, error: "", off: false }))
      .catch((err: unknown) => {
        if (stale) return;
        const off = err instanceof FleetApiError && err.disabled;
        setLoaded({ days, costs: null, error: err instanceof Error ? err.message : String(err), off });
      });
    return () => {
      stale = true;
    };
  }, [days]);

  if (loaded?.off) return null;

  return (
    <section className="card p-5 space-y-4" data-testid="fleet-costs">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h3 className="text-base font-semibold text-white">{t("fleet.costs.title")}</h3>
        <div className="flex gap-1">
          {PERIODS.map((d) => (
            <button
              key={d}
              type="button"
              onClick={() => setDays(d)}
              className={clsx(
                "rounded-md border px-2.5 py-1 text-xs",
                d === days ? "border-primary/40 bg-primary/15 text-primary" : "border-gray-700 text-gray-400 hover:text-gray-200",
              )}
            >
              {t("fleet.costs.days", { days: d })}
            </button>
          ))}
        </div>
      </div>
      {!loaded && <p className="text-sm text-gray-500">{t("common.loading")}</p>}
      {loaded?.error && <p className="text-sm text-error break-words">{loaded.error}</p>}
      {loaded?.costs && <FleetCostView costs={loaded.costs} days={loaded.days} />}
      {loaded?.costs && <FleetBackendUsageView key={days} days={days} />}
    </section>
  );
}
