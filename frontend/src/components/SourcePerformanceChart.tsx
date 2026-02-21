import { useMemo } from "react";
import { BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer } from "recharts";
import { Monitor, Code2, Send } from "lucide-react";
import type { DecisionLog } from "@/types/alice";

interface SourcePerformanceChartProps {
  decisions: DecisionLog[];
}

interface PerformanceData {
  source: string;
  label: string;
  icon: React.ComponentType<{ className?: string }>;
  avgDuration: number;
  successRate: number;
  totalDecisions: number;
  color: string;
}

const SOURCE_CONFIG: Record<string, { label: string; icon: React.ComponentType<{ className?: string }>; color: string }> = {
  terminal: {
    label: "Terminal",
    icon: Monitor,
    color: "#6b7280"
  },
  vscode: {
    label: "VS Code",
    icon: Code2,
    color: "#8b5cf6"
  },
  telegram: {
    label: "Telegram",
    icon: Send,
    color: "#06b6d4"
  },
  unknown: {
    label: "Unknown",
    icon: Monitor,
    color: "#64748b"
  }
};

function CustomTooltip({ active, payload }: any) {
  if (!active || !payload || !payload.length) return null;

  const data = payload[0].payload as PerformanceData;
  const IconComponent = data.icon;

  return (
    <div className="bg-gray-900/95 border border-gray-700 rounded-lg p-3 shadow-lg">
      <div className="flex items-center gap-2 mb-2">
        <div className="w-4 h-4 flex items-center justify-center" style={{ color: data.color }}>
          <IconComponent className="w-4 h-4" />
        </div>
        <span className="text-sm font-medium text-white">{data.label}</span>
      </div>
      <div className="space-y-1 text-xs text-gray-300">
        <div>Avg Duration: {(data.avgDuration ?? 0).toFixed(0)}ms</div>
        <div>Success Rate: {(data.successRate ?? 0).toFixed(1)}%</div>
        <div>Total Decisions: {data.totalDecisions ?? 0}</div>
      </div>
    </div>
  );
}

export default function SourcePerformanceChart({ decisions }: SourcePerformanceChartProps) {
  const chartData = useMemo(() => {
    if (decisions.length === 0) return [];

    // Group decisions by source
    const sourceGroups: Record<string, DecisionLog[]> = {};

    decisions.forEach(decision => {
      const source = decision.source || 'telegram'; // Default to telegram for legacy data
      if (!sourceGroups[source]) {
        sourceGroups[source] = [];
      }
      sourceGroups[source].push(decision);
    });

    // Calculate performance metrics for each source
    const performanceData: PerformanceData[] = Object.entries(sourceGroups).map(([source, sourceDecisions]) => {
      const config = SOURCE_CONFIG[source] || SOURCE_CONFIG.unknown;

      // Calculate average duration
      const durations = sourceDecisions
        .filter(d => d.duration_ms && d.duration_ms > 0)
        .map(d => d.duration_ms!);
      const avgDuration = durations.length > 0
        ? durations.reduce((sum, duration) => sum + duration, 0) / durations.length
        : 0;

      // Calculate success rate
      const successfulDecisions = sourceDecisions.filter(d =>
        d.outcome?.success !== false &&
        (!d.tool_calls || d.tool_calls.every(tc =>
          String(tc.status) !== "STATUS_ERROR" && String(tc.status) !== "4"
        ))
      );
      const successRate = sourceDecisions.length > 0
        ? (successfulDecisions.length / sourceDecisions.length) * 100
        : 0;

      return {
        source,
        label: config.label,
        icon: config.icon,
        avgDuration,
        successRate,
        totalDecisions: sourceDecisions.length,
        color: config.color
      };
    });

    // Sort by total decisions descending
    return performanceData.sort((a, b) => b.totalDecisions - a.totalDecisions);
  }, [decisions]);

  if (decisions.length === 0) {
    return (
      <div className="flex items-center justify-center h-[200px] text-sm text-gray-600">
        No decision data available
      </div>
    );
  }

  if (chartData.length === 0) {
    return (
      <div className="flex items-center justify-center h-[200px] text-sm text-gray-600">
        No performance data available
      </div>
    );
  }

  return (
    <div className="space-y-4">
      {/* Duration Chart */}
      <div>
        <h4 className="text-xs text-gray-500 mb-2">Average Duration (ms)</h4>
        <ResponsiveContainer width="100%" height={120}>
          <BarChart data={chartData} margin={{ top: 5, right: 5, left: -10, bottom: 0 }}>
            <CartesianGrid strokeDasharray="3 3" stroke="#1f2937" />
            <XAxis
              dataKey="label"
              tick={{ fill: "#6b7280", fontSize: 10 }}
              tickLine={false}
              axisLine={false}
            />
            <YAxis
              tick={{ fill: "#6b7280", fontSize: 10 }}
              tickLine={false}
              axisLine={false}
            />
            <Tooltip content={<CustomTooltip />} />
            <Bar
              dataKey="avgDuration"
              fill="#6366f1"
              radius={[2, 2, 0, 0]}
              maxBarSize={40}
            />
          </BarChart>
        </ResponsiveContainer>
      </div>

      {/* Success Rate Chart */}
      <div>
        <h4 className="text-xs text-gray-500 mb-2">Success Rate (%)</h4>
        <ResponsiveContainer width="100%" height={120}>
          <BarChart data={chartData} margin={{ top: 5, right: 5, left: -10, bottom: 0 }}>
            <CartesianGrid strokeDasharray="3 3" stroke="#1f2937" />
            <XAxis
              dataKey="label"
              tick={{ fill: "#6b7280", fontSize: 10 }}
              tickLine={false}
              axisLine={false}
            />
            <YAxis
              domain={[0, 100]}
              tick={{ fill: "#6b7280", fontSize: 10 }}
              tickLine={false}
              axisLine={false}
            />
            <Tooltip content={<CustomTooltip />} />
            <Bar
              dataKey="successRate"
              fill="#10b981"
              radius={[2, 2, 0, 0]}
              maxBarSize={40}
            />
          </BarChart>
        </ResponsiveContainer>
      </div>
    </div>
  );
}