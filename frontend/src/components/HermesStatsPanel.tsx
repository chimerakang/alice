import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  Bar,
  BarChart,
  CartesianGrid,
  Cell,
  Legend,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import { api } from "@/lib/api";
import type { HermesStats } from "@/types/alice";
import { Activity, BarChart3, Loader2, RefreshCw } from "lucide-react";

// HermesStatsPanel surfaces aggregate effectiveness metrics — daily
// success rate, failure-reason breakdown, source-node distribution,
// per-phase token averages, hop distribution. Mirrors the SQL queries
// from the manual analysis so the dashboard becomes the single source
// of truth for #171 / #173 bake observation.

function formatTokens(n: number): string {
  if (n >= 1_000_000) return (n / 1_000_000).toFixed(1) + "M";
  if (n >= 1_000) return (n / 1_000).toFixed(1) + "K";
  return String(n);
}

function pct(num: number, denom: number): string {
  if (denom === 0) return "—";
  return ((num / denom) * 100).toFixed(1) + "%";
}

const NODE_COLOR: Record<string, string> = {
  executor: "#3b82f6",
  reviewer: "#a855f7",
  strict_review: "#f97316",
  planner: "#22c55e",
  approval: "#eab308",
  replan_setup: "#ef4444",
  "(seed)": "#64748b",
};

export default function HermesStatsPanel() {
  const { t } = useTranslation();
  const [stats, setStats] = useState<HermesStats | null>(null);
  const [days, setDays] = useState(14);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const load = () => {
    setLoading(true);
    setError(null);
    api
      .getHermesStats(days)
      .then((s) => setStats(s))
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [days]);

  const sourceNodeData = useMemo(() => {
    if (!stats) return [];
    return Object.entries(stats.source_nodes)
      .map(([name, value]) => ({ name, value }))
      .sort((a, b) => b.value - a.value);
  }, [stats]);

  const dailyData = useMemo(() => {
    if (!stats) return [];
    return stats.daily.map((d) => ({
      day: d.day.slice(5), // MM-DD
      done: d.done,
      failed: d.failed,
      interrupted: d.interrupted,
      total: d.total,
      success_rate: d.total ? Math.round((d.done / d.total) * 1000) / 10 : 0,
    }));
  }, [stats]);

  if (loading && !stats) {
    return (
      <div className="card p-4">
        <div className="flex items-center gap-2 text-sm text-gray-400">
          <Loader2 className="w-4 h-4 animate-spin" />
          {t("hermes_tasks.stats_loading")}
        </div>
      </div>
    );
  }
  if (error || !stats) {
    return (
      <div className="card p-4">
        <div className="text-sm text-red-400">
          {error || t("hermes_tasks.stats_error")}
        </div>
      </div>
    );
  }

  const successRate = stats.totals.total ? stats.totals.done / stats.totals.total : 0;

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <BarChart3 className="w-5 h-5 text-primary" />
          <h2 className="text-lg font-semibold text-white">
            {t("hermes_tasks.stats_title")}
          </h2>
          <span className="text-xs text-gray-500">
            {t("hermes_tasks.stats_window", { days: stats.window_days })}
          </span>
        </div>
        <div className="flex items-center gap-2">
          <select
            value={days}
            onChange={(e) => setDays(parseInt(e.target.value, 10))}
            className="bg-gray-900 text-gray-200 text-xs border border-gray-700 rounded px-2 py-1"
          >
            <option value={7}>7d</option>
            <option value={14}>14d</option>
            <option value={30}>30d</option>
            <option value={90}>90d</option>
          </select>
          <button
            onClick={load}
            disabled={loading}
            className="btn btn-secondary text-xs inline-flex items-center gap-1"
          >
            {loading ? (
              <Loader2 className="w-3 h-3 animate-spin" />
            ) : (
              <RefreshCw className="w-3 h-3" />
            )}
            {t("common.refresh")}
          </button>
        </div>
      </div>

      {/* Summary cards */}
      <div className="grid grid-cols-2 md:grid-cols-5 gap-3">
        <SummaryCard label={t("hermes_tasks.stats_card_total")} value={String(stats.totals.total)} />
        <SummaryCard
          label={t("hermes_tasks.stats_card_done_failed")}
          value={`${stats.totals.done} / ${stats.totals.failed}`}
        />
        <SummaryCard
          label={t("hermes_tasks.stats_card_success_rate")}
          value={pct(stats.totals.done, stats.totals.total)}
          accent={
            successRate >= 0.9
              ? "good"
              : successRate >= 0.75
              ? "warn"
              : "bad"
          }
        />
        <SummaryCard label={t("hermes_tasks.stats_card_interrupted")} value={String(stats.totals.interrupted)} />
        <SummaryCard
          label={t("hermes_tasks.stats_card_generated_at")}
          value={new Date(stats.generated_at).toLocaleTimeString()}
        />
      </div>

      {/* Daily success rate */}
      <div className="card p-4">
        <div className="flex items-center gap-2 mb-2">
          <Activity className="w-4 h-4 text-blue-400" />
          <h3 className="text-sm font-semibold text-gray-200">{t("hermes_tasks.stats_chart_daily")}</h3>
        </div>
        <ResponsiveContainer width="100%" height={220}>
          <BarChart data={dailyData}>
            <CartesianGrid strokeDasharray="3 3" stroke="#374151" />
            <XAxis dataKey="day" tick={{ fill: "#9ca3af", fontSize: 11 }} />
            <YAxis tick={{ fill: "#9ca3af", fontSize: 11 }} />
            <Tooltip
              contentStyle={{
                backgroundColor: "#111827",
                border: "1px solid #374151",
                fontSize: 12,
              }}
              formatter={(v, name) => [v as number, String(name)]}
            />
            <Legend wrapperStyle={{ fontSize: 12 }} />
            <Bar dataKey="done" stackId="a" fill="#22c55e" name="done" />
            <Bar dataKey="failed" stackId="a" fill="#ef4444" name="failed" />
            <Bar dataKey="interrupted" stackId="a" fill="#eab308" name="interrupted" />
          </BarChart>
        </ResponsiveContainer>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-3">
        {/* Source node distribution */}
        <div className="card p-4">
          <h3 className="text-sm font-semibold text-gray-200 mb-2">
            {t("hermes_tasks.stats_chart_source_nodes")}
          </h3>
          <ResponsiveContainer width="100%" height={Math.max(140, sourceNodeData.length * 32)}>
            <BarChart data={sourceNodeData} layout="vertical" margin={{ left: 16, right: 16 }}>
              <CartesianGrid strokeDasharray="3 3" stroke="#374151" />
              <XAxis type="number" tick={{ fill: "#9ca3af", fontSize: 11 }} />
              <YAxis dataKey="name" type="category" tick={{ fill: "#9ca3af", fontSize: 11 }} width={100} />
              <Tooltip
                contentStyle={{
                  backgroundColor: "#111827",
                  border: "1px solid #374151",
                  fontSize: 12,
                }}
              />
              <Bar dataKey="value">
                {sourceNodeData.map((d) => (
                  <Cell key={d.name} fill={NODE_COLOR[d.name] || "#6366f1"} />
                ))}
              </Bar>
            </BarChart>
          </ResponsiveContainer>
        </div>

        {/* Failure reasons */}
        <div className="card p-4">
          <h3 className="text-sm font-semibold text-gray-200 mb-2">{t("hermes_tasks.stats_chart_failure_reasons")}</h3>
          {Object.keys(stats.failure_reasons).length === 0 ? (
            <div className="text-sm text-gray-500 py-2">{t("hermes_tasks.stats_no_failures")}</div>
          ) : (
            <table className="w-full text-sm">
              <thead>
                <tr className="text-xs text-gray-500 border-b border-gray-800">
                  <th className="text-left pb-1">{t("hermes_tasks.stats_table_reason")}</th>
                  <th className="text-right pb-1">{t("hermes_tasks.stats_table_count")}</th>
                </tr>
              </thead>
              <tbody>
                {Object.entries(stats.failure_reasons)
                  .sort((a, b) => b[1] - a[1])
                  .map(([reason, count]) => (
                    <tr key={reason} className="border-b border-gray-900">
                      <td className="py-1 font-mono text-xs text-gray-300">{reason}</td>
                      <td className="text-right py-1 font-mono text-gray-200">{count}</td>
                    </tr>
                  ))}
              </tbody>
            </table>
          )}
        </div>
      </div>

      {/* Phase tokens + hops */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-3">
        <div className="card p-4">
          <h3 className="text-sm font-semibold text-gray-200 mb-2">
            {t("hermes_tasks.stats_chart_phase_tokens")}
          </h3>
          {stats.phases.length === 0 ? (
            <div className="text-sm text-gray-500">{t("hermes_tasks.stats_no_phase_data")}</div>
          ) : (
            <table className="w-full text-sm">
              <thead>
                <tr className="text-xs text-gray-500 border-b border-gray-800">
                  <th className="text-left pb-1">{t("hermes_tasks.stats_table_phase")}</th>
                  <th className="text-right pb-1">{t("hermes_tasks.stats_table_calls")}</th>
                  <th className="text-right pb-1">{t("hermes_tasks.stats_table_avg_in")}</th>
                  <th className="text-right pb-1">{t("hermes_tasks.stats_table_avg_out")}</th>
                  <th className="text-right pb-1">{t("hermes_tasks.stats_table_sum_in")}</th>
                </tr>
              </thead>
              <tbody>
                {stats.phases
                  .slice()
                  .sort((a, b) => b.sum_input - a.sum_input)
                  .map((p) => (
                    <tr key={p.phase} className="border-b border-gray-900">
                      <td className="py-1 font-mono text-xs text-gray-300">{p.phase}</td>
                      <td className="text-right py-1 font-mono text-gray-200">{p.calls}</td>
                      <td className="text-right py-1 font-mono text-gray-200">
                        {formatTokens(p.avg_input)}
                      </td>
                      <td className="text-right py-1 font-mono text-gray-200">
                        {formatTokens(p.avg_output)}
                      </td>
                      <td className="text-right py-1 font-mono text-gray-400">
                        {formatTokens(p.sum_input)}
                      </td>
                    </tr>
                  ))}
              </tbody>
            </table>
          )}
        </div>

        <div className="card p-4">
          <h3 className="text-sm font-semibold text-gray-200 mb-2">{t("hermes_tasks.stats_chart_hops")}</h3>
          {stats.hops.length === 0 ? (
            <div className="text-sm text-gray-500">{t("hermes_tasks.stats_no_snapshot_data")}</div>
          ) : (
            <ResponsiveContainer width="100%" height={220}>
              <BarChart data={stats.hops}>
                <CartesianGrid strokeDasharray="3 3" stroke="#374151" />
                <XAxis
                  dataKey="hops"
                  tick={{ fill: "#9ca3af", fontSize: 11 }}
                  label={{ value: t("hermes_tasks.stats_xaxis_hops"), fontSize: 10, fill: "#9ca3af" }}
                />
                <YAxis tick={{ fill: "#9ca3af", fontSize: 11 }} />
                <Tooltip
                  contentStyle={{
                    backgroundColor: "#111827",
                    border: "1px solid #374151",
                    fontSize: 12,
                  }}
                  formatter={(v) => [v as number, t("hermes_tasks.stats_tooltip_tasks")]}
                  labelFormatter={(l) => `${l} hops`}
                />
                <Bar dataKey="tasks" fill="#6366f1" />
              </BarChart>
            </ResponsiveContainer>
          )}
        </div>
      </div>
    </div>
  );
}

function SummaryCard({
  label,
  value,
  accent,
}: {
  label: string;
  value: string;
  accent?: "good" | "warn" | "bad";
}) {
  const colorClass =
    accent === "good"
      ? "text-green-400"
      : accent === "warn"
      ? "text-yellow-400"
      : accent === "bad"
      ? "text-red-400"
      : "text-white";
  return (
    <div className="card p-3">
      <div className="text-xs text-gray-500">{label}</div>
      <div className={`text-xl font-bold font-mono mt-1 ${colorClass}`}>{value}</div>
    </div>
  );
}
