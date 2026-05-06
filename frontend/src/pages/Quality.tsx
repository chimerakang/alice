import { useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import { useNavigate } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { AlertTriangle, CheckCircle2, Gauge, Info, Lightbulb, ListChecks, TrendingUp } from "lucide-react";
import {
  Bar,
  BarChart,
  CartesianGrid,
  Cell,
  Line,
  LineChart,
  Pie,
  PieChart,
  ResponsiveContainer,
  Scatter,
  ScatterChart,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import { api } from "@/lib/api";
import type { QualityDecompositionStats, QualityInsight, QualityScoreStats } from "@/types/alice";

type WindowValue = "7d" | "30d" | "90d";

const verdictColors = ["#22C55E", "#F59E0B", "#EF4444"];

function formatPercent(value: number | undefined): string {
  return `${(value ?? 0).toFixed(0)}%`;
}

function formatNumber(value: number | undefined, digits = 1): string {
  return (value ?? 0).toFixed(digits);
}

function insightIcon(severity: string) {
  if (severity === "warning") return <AlertTriangle className="w-4 h-4 text-warning" />;
  if (severity === "success") return <CheckCircle2 className="w-4 h-4 text-success" />;
  return <Info className="w-4 h-4 text-info" />;
}

function StatCard({ icon, label, value, detail }: { icon: ReactNode; label: string; value: string; detail?: string }) {
  return (
    <div className="card p-5">
      <div className="flex items-center gap-2 text-sm text-gray-400 mb-2">
        {icon}
        <span>{label}</span>
      </div>
      <div className="text-2xl font-bold font-mono text-white">{value}</div>
      {detail && <div className="text-xs text-gray-500 mt-1">{detail}</div>}
    </div>
  );
}

export default function Quality() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [windowValue, setWindowValue] = useState<WindowValue>("30d");
  const [decomposition, setDecomposition] = useState<QualityDecompositionStats | null>(null);
  const [scores, setScores] = useState<QualityScoreStats | null>(null);
  const [insights, setInsights] = useState<QualityInsight[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;
    const load = async () => {
      setLoading(true);
      const [decompRes, scoreRes, insightRes] = await Promise.allSettled([
        api.getQualityDecomposition(windowValue),
        api.getQualityScores(windowValue),
        api.getQualityInsights(windowValue),
      ]);
      if (cancelled) return;
      if (decompRes.status === "fulfilled") setDecomposition(decompRes.value);
      if (scoreRes.status === "fulfilled") setScores(scoreRes.value);
      if (insightRes.status === "fulfilled") setInsights(insightRes.value.insights || []);
      setLoading(false);
    };
    load();
    const timer = window.setInterval(load, 60000);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [windowValue]);

  const verdictData = useMemo(() => {
    const dist = scores?.verdict_distribution || {};
    return [
      { name: t("quality.verdicts.pass"), value: dist.pass || 0 },
      { name: t("quality.verdicts.partial"), value: dist.partial || 0 },
      { name: t("quality.verdicts.fail"), value: dist.fail || 0 },
    ];
  }, [scores, t]);

  if (loading && !decomposition && !scores) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="w-8 h-8 border-2 border-primary border-t-transparent rounded-full animate-spin" />
      </div>
    );
  }

  return (
    <div className="space-y-6 animate-fade-in">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h2 className="text-lg font-semibold text-white">{t("quality.title")}</h2>
          <p className="text-sm text-gray-500 mt-1">{t("quality.subtitle")}</p>
        </div>
        <div className="inline-flex rounded-lg border border-gray-800 bg-black/30 p-1">
          {(["7d", "30d", "90d"] as WindowValue[]).map((value) => (
            <button
              key={value}
              type="button"
              onClick={() => setWindowValue(value)}
              className={`px-3 py-1.5 rounded-md text-sm transition-colors ${
                windowValue === value ? "bg-primary/15 text-primary" : "text-gray-400 hover:text-gray-200"
              }`}
            >
              {t(`quality.time_windows.${value}`)}
            </button>
          ))}
        </div>
      </div>

      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        <StatCard icon={<Gauge className="w-4 h-4 text-primary" />} label={t("quality.stats.avg_sub_tasks")} value={formatNumber(decomposition?.avg_sub_tasks)} detail={t("quality.stats.avg_sub_tasks_detail", { stddev: formatNumber(decomposition?.stddev_sub_tasks), count: decomposition?.task_count || 0 })} />
        <StatCard icon={<CheckCircle2 className="w-4 h-4 text-success" />} label={t("quality.stats.pass_rate")} value={formatPercent(scores?.pass_rate)} detail={t("quality.stats.pass_rate_detail", { count: scores?.review_count || 0 })} />
        <StatCard icon={<TrendingUp className="w-4 h-4 text-info" />} label={t("quality.stats.avg_score")} value={formatNumber(scores?.avg_overall_score, 0)} detail={t("quality.stats.avg_score_detail", { score: formatNumber(scores?.avg_sub_task_score, 0) })} />
        <StatCard icon={<AlertTriangle className="w-4 h-4 text-warning" />} label={t("quality.stats.partial_rate")} value={formatPercent(scores?.partial_rate)} detail={t("quality.stats.partial_rate_detail", { granularity: decomposition?.best_granularity || "-" })} />
      </div>

      <section className="space-y-4">
        <h3 className="text-sm font-semibold text-gray-300 flex items-center gap-2">
          <Gauge className="w-4 h-4" />
          {t("quality.sections.decomposition_effect")}
        </h3>
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
          <div className="card p-6">
            <div className="text-sm font-medium text-gray-300 mb-4">{t("quality.charts.granularity_distribution")}</div>
            <ResponsiveContainer width="100%" height={220}>
              <BarChart data={decomposition?.granularity_buckets || []}>
                <CartesianGrid strokeDasharray="3 3" stroke="#374151" />
                <XAxis dataKey="label" stroke="#9CA3AF" fontSize={12} />
                <YAxis stroke="#9CA3AF" fontSize={12} />
                <Tooltip contentStyle={{ backgroundColor: "#1F2937", border: "1px solid #374151", borderRadius: 8 }} />
                <Bar dataKey="count" fill="#3B82F6" radius={[4, 4, 0, 0]} />
              </BarChart>
            </ResponsiveContainer>
          </div>
          <div className="card p-6">
            <div className="text-sm font-medium text-gray-300 mb-4">{t("quality.charts.granularity_vs_score")}</div>
            <ResponsiveContainer width="100%" height={220}>
              <ScatterChart>
                <CartesianGrid strokeDasharray="3 3" stroke="#374151" />
                <XAxis dataKey="sub_task_count" name={t("quality.charts.sub_tasks")} stroke="#9CA3AF" fontSize={12} />
                <YAxis dataKey="avg_score" name={t("quality.charts.score")} stroke="#9CA3AF" fontSize={12} domain={[0, 100]} />
                <Tooltip contentStyle={{ backgroundColor: "#1F2937", border: "1px solid #374151", borderRadius: 8 }} />
                <Scatter data={decomposition?.granularity_scores || []} fill="#22C55E" />
              </ScatterChart>
            </ResponsiveContainer>
          </div>
          <div className="card p-6">
            <div className="text-sm font-medium text-gray-300 mb-4">{t("quality.charts.weekly_sub_task_trend")}</div>
            <ResponsiveContainer width="100%" height={220}>
              <LineChart data={decomposition?.weekly_trend || []}>
                <CartesianGrid strokeDasharray="3 3" stroke="#374151" />
                <XAxis dataKey="period" stroke="#9CA3AF" fontSize={12} />
                <YAxis stroke="#9CA3AF" fontSize={12} />
                <Tooltip contentStyle={{ backgroundColor: "#1F2937", border: "1px solid #374151", borderRadius: 8 }} />
                <Line type="monotone" dataKey="avg_sub_tasks" stroke="#F97316" strokeWidth={2} dot={false} />
              </LineChart>
            </ResponsiveContainer>
          </div>
          <div className="card p-6">
            <div className="text-sm font-medium text-gray-300 mb-4">{t("quality.charts.description_length_vs_fail_rate")}</div>
            <ResponsiveContainer width="100%" height={220}>
              <BarChart data={decomposition?.description_buckets || []}>
                <CartesianGrid strokeDasharray="3 3" stroke="#374151" />
                <XAxis dataKey="label" stroke="#9CA3AF" fontSize={12} />
                <YAxis stroke="#9CA3AF" fontSize={12} />
                <Tooltip contentStyle={{ backgroundColor: "#1F2937", border: "1px solid #374151", borderRadius: 8 }} />
                <Bar dataKey="fail_rate" fill="#EF4444" radius={[4, 4, 0, 0]} />
              </BarChart>
            </ResponsiveContainer>
          </div>
        </div>
      </section>

      <section className="space-y-4">
        <h3 className="text-sm font-semibold text-gray-300 flex items-center gap-2">
          <ListChecks className="w-4 h-4" />
          {t("quality.sections.review_scoreboard")}
        </h3>
        <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
          <div className="card p-6 lg:col-span-2">
            <div className="text-sm font-medium text-gray-300 mb-4">{t("quality.charts.pass_partial_fail_trend")}</div>
            <ResponsiveContainer width="100%" height={240}>
              <LineChart data={scores?.trend || []}>
                <CartesianGrid strokeDasharray="3 3" stroke="#374151" />
                <XAxis dataKey="period" stroke="#9CA3AF" fontSize={12} />
                <YAxis stroke="#9CA3AF" fontSize={12} />
                <Tooltip contentStyle={{ backgroundColor: "#1F2937", border: "1px solid #374151", borderRadius: 8 }} />
                <Line type="monotone" dataKey="pass_rate" stroke="#22C55E" strokeWidth={2} dot={false} />
                <Line type="monotone" dataKey="partial_rate" stroke="#F59E0B" strokeWidth={2} dot={false} />
                <Line type="monotone" dataKey="fail_rate" stroke="#EF4444" strokeWidth={2} dot={false} />
              </LineChart>
            </ResponsiveContainer>
          </div>
          <div className="card p-6">
            <div className="text-sm font-medium text-gray-300 mb-4">{t("quality.charts.verdict_distribution")}</div>
            <ResponsiveContainer width="100%" height={240}>
              <PieChart>
                <Pie data={verdictData} dataKey="value" nameKey="name" innerRadius={50} outerRadius={82}>
                  {verdictData.map((entry, index) => (
                    <Cell key={entry.name} fill={verdictColors[index]} />
                  ))}
                </Pie>
                <Tooltip contentStyle={{ backgroundColor: "#1F2937", border: "1px solid #374151", borderRadius: 8 }} />
              </PieChart>
            </ResponsiveContainer>
          </div>
        </div>

        <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
          <div className="card p-6">
            <div className="text-sm font-medium text-gray-300 mb-4">{t("quality.charts.top_issue_tags")}</div>
            <div className="space-y-3">
              {(scores?.top_issue_tags || []).map((tag, index) => (
                <div key={tag.tag} className="flex items-center justify-between gap-3 text-sm">
                  <div className="min-w-0">
                    <span className="text-gray-500 mr-2">{index + 1}.</span>
                    <span className="text-gray-200 font-mono">{tag.tag}</span>
                  </div>
                  <div className="text-gray-400 font-mono">
                    {tag.count} {tag.trend === "up" ? "↑" : tag.trend === "down" ? "↓" : "→"}
                  </div>
                </div>
              ))}
              {(scores?.top_issue_tags || []).length === 0 && <div className="text-sm text-gray-500">{t("quality.empty.no_issue_tags")}</div>}
            </div>
          </div>
          <div className="card p-6">
            <div className="text-sm font-medium text-gray-300 mb-4">{t("quality.charts.low_scoring_sub_tasks")}</div>
            <div className="space-y-2">
              {(scores?.low_scoring_sub_tasks || []).slice(0, 5).map((item) => (
                <button
                  key={`${item.task_id}:${item.sub_task_id}`}
                  type="button"
                  onClick={() => navigate(`/reviews?focus=${encodeURIComponent(item.task_id)}`)}
                  className="w-full text-left rounded-md border border-gray-800/80 px-3 py-2 hover:bg-white/5"
                >
                  <div className="flex items-center justify-between gap-3">
                    <span className="text-sm text-gray-300 truncate">{item.description || item.sub_task_id}</span>
                    <span className="text-sm font-mono text-warning">{item.score}</span>
                  </div>
                  <div className="text-xs text-gray-500 font-mono truncate">{item.task_id}</div>
                </button>
              ))}
              {(scores?.low_scoring_sub_tasks || []).length === 0 && <div className="text-sm text-gray-500">{t("quality.empty.no_low_scoring_sub_tasks")}</div>}
            </div>
          </div>
        </div>
      </section>

      <section className="space-y-4">
        <h3 className="text-sm font-semibold text-gray-300 flex items-center gap-2">
          <Lightbulb className="w-4 h-4" />
          {t("quality.sections.alice_insights")}
        </h3>
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
          {insights.map((insight) => (
            <div key={insight.name} className="card p-5">
              <div className="flex items-start gap-3">
                {insightIcon(insight.severity)}
                <div className="min-w-0">
                  <div className="text-sm font-medium text-gray-100">{insight.message}</div>
                  <div className="text-sm text-gray-500 mt-1">{insight.suggestion}</div>
                </div>
              </div>
            </div>
          ))}
          {insights.length === 0 && <div className="text-sm text-gray-500">{t("quality.empty.no_active_insights")}</div>}
        </div>
      </section>
    </div>
  );
}
