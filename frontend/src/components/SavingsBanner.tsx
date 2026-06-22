import { TrendingDown } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

interface CostSavingsReport {
  period_hours: number;
  start_time: string;
  end_time: string;
  actual_cost: number;
  default_model_cost: number;
  savings_cost: number;
  savings_percent: number;
  total_requests: number;
  by_model: Record<string, {
    calls: number;
    actual_cost: number;
    would_have_cost: number;
    saved: number;
    input_tokens: number;
    output_tokens: number;
  }>;
  routing_method_stat: Record<string, number>;
}

interface SavingsBannerProps {
  hours?: number;
}

export function SavingsBanner({ hours = 168 }: SavingsBannerProps) {
  const { t } = useTranslation();
  const [report, setReport] = useState<CostSavingsReport | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    fetchSavings();
  }, [hours]);

  const fetchSavings = async () => {
    try {
      setLoading(true);
      const response = await fetch(`/api/costs/savings?hours=${hours}`);
      if (!response.ok) throw new Error(t("savings.error.fetch_failed"));
      const data = await response.json();
      setReport(data);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : t("savings.error.unknown"));
      setReport(null);
    } finally {
      setLoading(false);
    }
  };

  if (loading) {
    return (
      <div className="card p-6 border-l-4 border-l-success animate-pulse">
        <div className="h-8 bg-gray-700/30 rounded w-1/3 mb-4"></div>
        <div className="h-4 bg-gray-700/30 rounded w-2/5"></div>
      </div>
    );
  }

  if (error || !report || report.total_requests === 0) {
    return (
      <div className="bg-gray-900/50 border border-gray-700 rounded-lg p-6">
        <p className="text-gray-400 text-sm">
          {report?.total_requests === 0 ? t("savings.no_routing_data") : t("savings.load_failed")}
        </p>
      </div>
    );
  }

  const savingsPercentage = Math.round(report.savings_percent);
  const savingsAmount = report.savings_cost;
  const actualCost = report.actual_cost;

  return (
    <div className="card p-6 border-l-4 border-l-success">
      <div className="flex items-start justify-between mb-4">
        <div className="flex items-center gap-3">
          <div className="p-2 bg-gray-800/50 rounded-lg">
            <TrendingDown className="w-5 h-5 text-success" />
          </div>
          <div>
            <h3 className="text-base font-semibold text-white">{t("dashboard.panels.smart_routing_savings")}</h3>
            <p className="text-xs text-gray-500">
              {t("dashboard.panels.savings_period", {
                hours: report.period_hours,
                start: new Date(report.start_time).toLocaleDateString(),
                end: new Date(report.end_time).toLocaleDateString(),
              })}
            </p>
          </div>
        </div>
        <div className="text-right">
          <div className="text-2xl font-bold text-success">
            ${savingsAmount.toFixed(2)}
          </div>
          <div className="text-xs text-gray-400">{t("dashboard.panels.saved_percent", { percent: savingsPercentage })}</div>
        </div>
      </div>

      <div className="space-y-3">
        {/* Progress Bar */}
        <div className="space-y-1">
          <div className="flex justify-between text-xs">
            <span className="text-gray-500">{t("dashboard.panels.actual_cost")}</span>
            <span className="text-gray-300 font-medium">${actualCost.toFixed(2)}</span>
          </div>
          <div className="w-full bg-gray-800/50 rounded-full h-2 overflow-hidden">
            <div
              className="h-full bg-success transition-all duration-500"
              style={{
                width: `${Math.min((actualCost / report.default_model_cost) * 100, 100)}%`,
              }}
            />
          </div>
          <div className="flex justify-between text-xs">
            <span className="text-gray-500">{t("dashboard.panels.default_model_cost")}</span>
            <span className="text-gray-300 font-medium">${report.default_model_cost.toFixed(2)}</span>
          </div>
        </div>

        {/* Stats Grid */}
        <div className="grid grid-cols-2 md:grid-cols-4 gap-3 mt-4 pt-4 border-t border-gray-800/50">
          <StatCard
            label={t("dashboard.panels.total_requests")}
            value={report.total_requests.toString()}
          />
          <StatCard
            label={t("dashboard.panels.cost_per_request")}
            value={`$${(actualCost / report.total_requests).toFixed(4)}`}
          />
          <StatCard
            label={t("dashboard.panels.models_used")}
            value={Object.keys(report.by_model).length.toString()}
          />
          <StatCard
            label={t("dashboard.panels.efficiency")}
            value={`${savingsPercentage}%`}
          />
        </div>
      </div>
    </div>
  );
}

function StatCard({ label, value }: { label: string; value: string }) {
  return (
    <div className="text-center">
      <div className="text-xs text-gray-500 mb-1">{label}</div>
      <div className="text-sm font-semibold text-white">{value}</div>
    </div>
  );
}
