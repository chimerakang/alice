import { TrendingDown } from "lucide-react";
import { useEffect, useState } from "react";

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
      if (!response.ok) throw new Error("Failed to fetch savings data");
      const data = await response.json();
      setReport(data);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unknown error");
      setReport(null);
    } finally {
      setLoading(false);
    }
  };

  if (loading) {
    return (
      <div className="bg-gradient-to-r from-green-900/20 to-emerald-900/20 border border-green-700/30 rounded-lg p-6 animate-pulse">
        <div className="h-8 bg-green-700/30 rounded w-1/3 mb-4"></div>
        <div className="h-4 bg-green-700/30 rounded w-2/5"></div>
      </div>
    );
  }

  if (error || !report || report.total_requests === 0) {
    return (
      <div className="bg-gray-900/50 border border-gray-700 rounded-lg p-6">
        <p className="text-gray-400 text-sm">
          {report?.total_requests === 0 ? "本週還沒有路由數據" : "無法載入節省數據"}
        </p>
      </div>
    );
  }

  const savingsPercentage = Math.round(report.savings_percent);
  const savingsAmount = report.savings_cost;
  const actualCost = report.actual_cost;

  return (
    <div className="bg-gradient-to-r from-green-900/30 to-emerald-900/20 border border-green-700/50 rounded-lg p-6 shadow-lg">
      <div className="flex items-start justify-between mb-4">
        <div className="flex items-center gap-3">
          <div className="p-3 bg-green-900/40 rounded-lg">
            <TrendingDown className="w-6 h-6 text-green-400" />
          </div>
          <div>
            <h3 className="text-lg font-semibold text-white">Smart Routing Savings</h3>
            <p className="text-xs text-gray-400">
              Last {report.period_hours} hours ({new Date(report.start_time).toLocaleDateString()} - {new Date(report.end_time).toLocaleDateString()})
            </p>
          </div>
        </div>
        <div className="text-right">
          <div className="text-3xl font-bold text-green-400">
            ${savingsAmount.toFixed(2)}
          </div>
          <div className="text-sm text-green-300">Saved ({savingsPercentage}%)</div>
        </div>
      </div>

      <div className="space-y-3">
        {/* Progress Bar */}
        <div className="space-y-1">
          <div className="flex justify-between text-xs">
            <span className="text-gray-400">Actual Cost</span>
            <span className="text-gray-300 font-medium">${actualCost.toFixed(2)}</span>
          </div>
          <div className="w-full bg-gray-800/50 rounded-full h-2 overflow-hidden border border-gray-700/30">
            <div
              className="h-full bg-gradient-to-r from-green-500 to-emerald-400 transition-all duration-500"
              style={{
                width: `${Math.min((actualCost / report.default_model_cost) * 100, 100)}%`,
              }}
            />
          </div>
          <div className="flex justify-between text-xs">
            <span className="text-gray-400">If using Sonnet (default)</span>
            <span className="text-gray-300 font-medium">${report.default_model_cost.toFixed(2)}</span>
          </div>
        </div>

        {/* Stats Grid */}
        <div className="grid grid-cols-2 md:grid-cols-4 gap-3 mt-4 pt-4 border-t border-gray-700/30">
          <StatCard
            label="Total Requests"
            value={report.total_requests.toString()}
            icon="📊"
          />
          <StatCard
            label="Cost per Request"
            value={`$${(actualCost / report.total_requests).toFixed(4)}`}
            icon="💵"
          />
          <StatCard
            label="Models Used"
            value={Object.keys(report.by_model).length.toString()}
            icon="🔄"
          />
          <StatCard
            label="Efficiency"
            value={`${savingsPercentage}%`}
            icon="⚡"
          />
        </div>
      </div>
    </div>
  );
}

function StatCard({ label, value, icon }: { label: string; value: string; icon: string }) {
  return (
    <div className="bg-gray-800/40 rounded p-3 border border-gray-700/20">
      <div className="flex items-center gap-2 mb-1">
        <span className="text-lg">{icon}</span>
        <span className="text-xs text-gray-500">{label}</span>
      </div>
      <div className="text-sm font-semibold text-white">{value}</div>
    </div>
  );
}
