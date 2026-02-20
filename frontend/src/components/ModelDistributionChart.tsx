import { useMemo, useEffect, useState } from "react";
import { PieChart, Pie, Cell, ResponsiveContainer, Tooltip } from "recharts";

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

interface ModelDistributionChartProps {
  hours?: number;
}

const MODEL_CONFIG: Record<string, { color: string; icon: string; label: string }> = {
  haiku: {
    color: "#10b981",
    icon: "🟢",
    label: "Haiku (Fast)",
  },
  sonnet: {
    color: "#f59e0b",
    icon: "🟡",
    label: "Sonnet (Standard)",
  },
  opus: {
    color: "#ef4444",
    icon: "🔴",
    label: "Opus (Deep)",
  },
};

interface ChartData {
  name: string;
  value: number;
  calls: number;
  cost: number;
  color: string;
  icon: string;
}

export function ModelDistributionChart({ hours = 168 }: ModelDistributionChartProps) {
  const [report, setReport] = useState<CostSavingsReport | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    fetchData();
  }, [hours]);

  const fetchData = async () => {
    try {
      setLoading(true);
      const response = await fetch(`/api/costs/savings?hours=${hours}`);
      if (!response.ok) throw new Error("Failed to fetch data");
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

  const chartData = useMemo(() => {
    if (!report) return [];

    return Object.entries(report.by_model).map(([model, data]) => {
      const config = MODEL_CONFIG[model.toLowerCase()] || {
        color: "#6b7280",
        icon: "⚪",
        label: model,
      };

      return {
        name: config.label,
        value: data.calls,
        calls: data.calls,
        cost: data.actual_cost,
        color: config.color,
        icon: config.icon,
      };
    });
  }, [report]);

  if (loading) {
    return (
      <div className="bg-gray-900/50 border border-gray-700 rounded-lg p-6">
        <div className="h-6 bg-gray-700/30 rounded w-1/3 mb-4"></div>
        <div className="h-48 bg-gray-800/20 rounded animate-pulse"></div>
      </div>
    );
  }

  if (error || !report || chartData.length === 0) {
    return (
      <div className="bg-gray-900/50 border border-gray-700 rounded-lg p-6">
        <h3 className="text-sm font-medium text-gray-400 mb-4">Model Distribution</h3>
        <p className="text-xs text-gray-500">No data available</p>
      </div>
    );
  }

  return (
    <div className="bg-gray-900/50 border border-gray-700 rounded-lg p-6">
      <h3 className="text-sm font-medium text-white mb-4 flex items-center gap-2">
        <span>🔄 Model Distribution</span>
        <span className="text-xs text-gray-500">({report.total_requests} requests)</span>
      </h3>

      <ResponsiveContainer width="100%" height={300}>
        <PieChart>
          <Pie
            data={chartData}
            cx="50%"
            cy="50%"
            labelLine={false}
            label={({ percent }) => (
              <text x="0" y="0" fill="#fff" textAnchor="middle" fontSize={12}>
                {`${((percent || 0) * 100).toFixed(0)}%`}
              </text>
            )}
            outerRadius={80}
            fill="#8884d8"
            dataKey="value"
          >
            {chartData.map((entry, index) => (
              <Cell key={`cell-${index}`} fill={entry.color} />
            ))}
          </Pie>
          <Tooltip
            contentStyle={{
              backgroundColor: "rgba(17, 24, 39, 0.95)",
              border: "1px solid rgb(55, 65, 81)",
              borderRadius: "8px",
              padding: "8px",
            }}
            content={({ active, payload }) => {
              if (!active || !payload?.[0]) return null;
              const data = payload[0].payload as ChartData;
              return (
                <div className="space-y-1">
                  <div className="flex items-center gap-2">
                    <span>{data.icon}</span>
                    <span className="text-sm font-medium text-white">{data.name}</span>
                  </div>
                  <div className="text-xs text-gray-300">
                    Calls: {data.calls}
                  </div>
                  <div className="text-xs text-gray-300">
                    Cost: ${data.cost.toFixed(2)}
                  </div>
                </div>
              );
            }}
          />
        </PieChart>
      </ResponsiveContainer>

      <div className="grid grid-cols-3 gap-2 mt-4 pt-4 border-t border-gray-700/30">
        {chartData.map((data, idx) => (
          <div key={idx} className="bg-gray-800/40 rounded p-2 border border-gray-700/20">
            <div className="flex items-center gap-2 mb-1">
              <span>{data.icon}</span>
              <span className="text-xs text-gray-400">{data.name}</span>
            </div>
            <div className="text-xs font-semibold text-white">{data.calls} calls</div>
            <div className="text-xs text-gray-500">${data.cost.toFixed(2)}</div>
          </div>
        ))}
      </div>
    </div>
  );
}
