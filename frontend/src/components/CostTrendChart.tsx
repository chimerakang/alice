import { useEffect, useState } from "react";
import {
  AreaChart,
  Area,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
} from "recharts";

interface ChartDataPoint {
  time: string;
  actualCost: number;
  expectedCost: number;
  savings: number;
}

interface CostTrendChartProps {
  hours?: number;
}

export function CostTrendChart({ hours = 168 }: CostTrendChartProps) {
  const [data, setData] = useState<ChartDataPoint[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    fetchData();
  }, [hours]);

  const fetchData = async () => {
    try {
      setLoading(true);
      // 使用 performance metrics API 獲取詳細數據
      const response = await fetch(
        `/api/performance/analytics?hours=${hours}`
      );
      if (!response.ok) throw new Error("Failed to fetch data");
      const analyticsData = await response.json();

      // 同時獲取 cost savings 報告用於對比
      const savingsResponse = await fetch(`/api/costs/savings?hours=${hours}`);
      const savingsData = savingsResponse.ok
        ? await savingsResponse.json()
        : null;

      // 生成圖表數據
      if (analyticsData.analytics?.total_cost > 0 && savingsData) {
        const trendData = [
          {
            time: `Last ${hours}h`,
            actualCost: parseFloat(savingsData.actual_cost.toFixed(2)),
            expectedCost: parseFloat(
              savingsData.default_model_cost.toFixed(2)
            ),
            savings: parseFloat(savingsData.savings_cost.toFixed(2)),
          },
        ];
        setData(trendData);
      } else {
        setData([]);
      }

      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unknown error");
      setData([]);
    } finally {
      setLoading(false);
    }
  };

  if (loading) {
    return (
      <div className="bg-gray-900/50 border border-gray-700 rounded-lg p-6">
        <div className="h-6 bg-gray-700/30 rounded w-1/3 mb-4"></div>
        <div className="h-64 bg-gray-800/20 rounded animate-pulse"></div>
      </div>
    );
  }

  if (error || data.length === 0) {
    return (
      <div className="bg-gray-900/50 border border-gray-700 rounded-lg p-6">
        <h3 className="text-sm font-medium text-gray-400 mb-4">Cost Trend</h3>
        <p className="text-xs text-gray-500">
          {error || "No data available"}
        </p>
      </div>
    );
  }

  return (
    <div className="bg-gray-900/50 border border-gray-700 rounded-lg p-6">
      <h3 className="text-sm font-medium text-white mb-4 flex items-center gap-2">
        <span>📈 Cost Trend Analysis</span>
        <span className="text-xs text-gray-500">(Actual vs. Default Model)</span>
      </h3>

      <ResponsiveContainer width="100%" height={300}>
        <AreaChart
          data={data}
          margin={{ top: 10, right: 30, left: 0, bottom: 0 }}
        >
          <defs>
            <linearGradient id="colorExpected" x1="0" y1="0" x2="0" y2="1">
              <stop offset="5%" stopColor="#ef4444" stopOpacity={0.3} />
              <stop offset="95%" stopColor="#ef4444" stopOpacity={0} />
            </linearGradient>
            <linearGradient id="colorActual" x1="0" y1="0" x2="0" y2="1">
              <stop offset="5%" stopColor="#10b981" stopOpacity={0.3} />
              <stop offset="95%" stopColor="#10b981" stopOpacity={0} />
            </linearGradient>
            <linearGradient id="colorSavings" x1="0" y1="0" x2="0" y2="1">
              <stop offset="5%" stopColor="#3b82f6" stopOpacity={0.2} />
              <stop offset="95%" stopColor="#3b82f6" stopOpacity={0} />
            </linearGradient>
          </defs>

          <CartesianGrid
            strokeDasharray="3 3"
            stroke="rgba(107, 114, 128, 0.2)"
          />
          <XAxis dataKey="time" stroke="rgb(107, 114, 128)" />
          <YAxis stroke="rgb(107, 114, 128)" />

          {/* 成本趨勢區域 */}
          <Area
            type="monotone"
            dataKey="expectedCost"
            stroke="#ef4444"
            strokeWidth={2}
            fillOpacity={1}
            fill="url(#colorExpected)"
            name="If Using Sonnet"
          />
          <Area
            type="monotone"
            dataKey="actualCost"
            stroke="#10b981"
            strokeWidth={2}
            fillOpacity={1}
            fill="url(#colorActual)"
            name="Actual Cost (Routed)"
          />

          <Tooltip
            contentStyle={{
              backgroundColor: "rgba(17, 24, 39, 0.95)",
              border: "1px solid rgb(55, 65, 81)",
              borderRadius: "8px",
            }}
            formatter={(value: any) => `$${value.toFixed(2)}`}
            labelFormatter={(label) => `Period: ${label}`}
          />
        </AreaChart>
      </ResponsiveContainer>

      {/* 統計摘要 */}
      <div className="flex flex-row gap-3 mt-4 pt-4 border-t border-gray-700/30">
        {data.map((point, idx) => (
          <div key={idx} className="flex flex-row gap-3 w-full">
            <SummaryCard
              icon="💚"
              label="Actual Cost"
              value={`$${point.actualCost.toFixed(2)}`}
            />
            <SummaryCard
              icon="❌"
              label="Standard Model"
              value={`$${point.expectedCost.toFixed(2)}`}
            />
            <SummaryCard
              icon="✅"
              label="Savings"
              value={`$${point.savings.toFixed(2)}`}
            />
          </div>
        ))}
      </div>
    </div>
  );
}

function SummaryCard({
  icon,
  label,
  value,
}: {
  icon: string;
  label: string;
  value: string;
}) {
  return (
    <div className="bg-gray-800/40 rounded p-2 border border-gray-700/20">
      <div className="flex items-center gap-2">
        <span className="text-sm">{icon}</span>
        <div className="min-w-0">
          <div className="text-xs text-gray-500">{label}</div>
          <div className="text-sm font-semibold text-white truncate">{value}</div>
        </div>
      </div>
    </div>
  );
}
