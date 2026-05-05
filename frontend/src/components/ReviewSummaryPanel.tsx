import { useMemo, type ComponentType } from "react";
import { useTranslation } from "react-i18next";
import { clsx } from "clsx";
import {
  Activity,
  BarChart3,
  MessageSquareText,
  Wifi,
} from "lucide-react";
import {
  ResponsiveContainer,
  PieChart,
  Pie,
  Cell,
  BarChart,
  Bar,
  XAxis,
  YAxis,
  Tooltip,
  CartesianGrid,
} from "recharts";
import StatusBadge from "@/components/StatusBadge";
import type { ReviewFeedItem } from "@/lib/reviews";
import { buildReviewVerdictChartData, computeReviewSummary } from "@/lib/reviews";

function MetricCard({
  label,
  value,
  icon: Icon,
  color,
}: {
  label: string;
  value: string | number;
  icon: ComponentType<{ className?: string }>;
  color: string;
}) {
  return (
    <div className="card p-4">
      <div className="flex items-center justify-between mb-2">
        <span className="text-xs text-gray-500 uppercase tracking-wide">{label}</span>
        <Icon className={`w-4 h-4 ${color}`} />
      </div>
      <div className="text-2xl font-bold font-mono tracking-tight text-white">
        {value}
      </div>
    </div>
  );
}

function EmptyChart({ label }: { label: string }) {
  return (
    <div className="flex items-center justify-center h-[180px] text-sm text-gray-600">
      {label}
    </div>
  );
}

interface ReviewSummaryPanelProps {
  reviews: ReviewFeedItem[];
  liveConnected?: boolean;
  className?: string;
  emptyMessage?: string;
  showLiveCount?: boolean;
}

export default function ReviewSummaryPanel({
  reviews,
  liveConnected = false,
  className,
  emptyMessage,
  showLiveCount = true,
}: ReviewSummaryPanelProps) {
  const { t } = useTranslation();
  const stats = useMemo(() => computeReviewSummary(reviews), [reviews]);
  const verdictChartData = useMemo(() => buildReviewVerdictChartData(stats), [stats]);
  const resolvedEmptyMessage = emptyMessage ?? t("reviews.empty_no_reviews");

  return (
    <div className={clsx("space-y-4", className)}>
      <div className="flex items-center gap-2">
        <MessageSquareText className="w-4 h-4 text-accent" />
        <h3 className="text-sm font-semibold text-gray-400">{t("reviews.summary_title")}</h3>
        <StatusBadge variant={liveConnected ? "success" : "neutral"} size="sm" dot>
          {liveConnected ? t("common.live") : t("common.historical")}
        </StatusBadge>
      </div>

      {reviews.length === 0 ? (
        <div className="flex items-center justify-center h-40 text-sm text-gray-500">
          {resolvedEmptyMessage}
        </div>
      ) : (
        <div className="space-y-4">
          <div className={clsx("grid gap-3", showLiveCount ? "grid-cols-2 lg:grid-cols-4" : "grid-cols-1 sm:grid-cols-3")}>
            <MetricCard label="Reviews" value={stats.total} icon={MessageSquareText} color="text-accent" />
            <MetricCard label="Pass Rate" value={`${stats.passRate}%`} icon={Activity} color="text-success" />
            <MetricCard label="Avg Score" value={stats.avgScore} icon={BarChart3} color="text-primary" />
            {showLiveCount ? (
              <MetricCard label="Live" value={stats.liveCount} icon={Wifi} color="text-cyan-400" />
            ) : null}
          </div>

          <div className="grid grid-cols-1 xl:grid-cols-2 gap-4">
            <div className="card p-4">
              <h4 className="text-xs font-semibold text-gray-400 mb-3">Verdict Distribution</h4>
              {verdictChartData.length === 0 ? (
                <EmptyChart label="No verdict data" />
              ) : (
                <div className="flex items-center gap-4">
                  <ResponsiveContainer width={110} height={110}>
                    <PieChart>
                      <Pie
                        data={verdictChartData}
                        cx="50%"
                        cy="50%"
                        innerRadius={28}
                        outerRadius={45}
                        dataKey="value"
                        strokeWidth={0}
                      >
                        {verdictChartData.map((entry, index) => (
                          <Cell key={entry.name} fill={entry.color || ["#22c55e", "#f59e0b", "#ef4444"][index]} />
                        ))}
                      </Pie>
                    </PieChart>
                  </ResponsiveContainer>
                  <div className="space-y-2 text-xs">
                    {verdictChartData.map((entry) => (
                      <div key={entry.name} className="flex items-center gap-2">
                        <span className="w-2 h-2 rounded-full" style={{ backgroundColor: entry.color }} />
                        <span className="text-gray-400">{entry.name}</span>
                        <span className="text-white font-mono">{entry.value}</span>
                      </div>
                    ))}
                  </div>
                </div>
              )}
            </div>

            <div className="card p-4">
              <h4 className="text-xs font-semibold text-gray-400 mb-3">Top Issue Tags</h4>
              {stats.topIssueTags.length === 0 ? (
                <EmptyChart label="No issue tags recorded" />
              ) : (
                <>
                  <ResponsiveContainer width="100%" height={180}>
                    <BarChart
                      data={stats.topIssueTags}
                      layout="vertical"
                      margin={{ top: 0, right: 10, left: 10, bottom: 0 }}
                    >
                      <CartesianGrid strokeDasharray="3 3" stroke="#1f2937" horizontal={false} />
                      <XAxis type="number" tick={{ fill: "#6b7280", fontSize: 10 }} tickLine={false} axisLine={false} allowDecimals={false} />
                      <YAxis
                        type="category"
                        dataKey="tag"
                        width={120}
                        tick={{ fill: "#d1d5db", fontSize: 10 }}
                        tickLine={false}
                        axisLine={false}
                      />
                      <Tooltip
                        contentStyle={{ backgroundColor: "#111827", border: "1px solid #374151", borderRadius: "0.5rem", fontSize: 12 }}
                        labelStyle={{ color: "#9ca3af" }}
                      />
                      <Bar dataKey="value" fill="#6366f1" radius={[0, 4, 4, 0]} />
                    </BarChart>
                  </ResponsiveContainer>
                  <div className="mt-3 flex flex-wrap gap-1.5">
                    {stats.topIssueTags.map((entry) => (
                      <span
                        key={entry.tag}
                        className="inline-flex items-center gap-1 rounded-full bg-gray-800 px-2 py-1 text-[10px] text-gray-300"
                      >
                        <span className="font-mono text-gray-500">{entry.value}</span>
                        <span>{entry.tag}</span>
                      </span>
                    ))}
                  </div>
                </>
              )}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
