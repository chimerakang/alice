import { useMemo } from "react";
import { PieChart, Pie, Cell, ResponsiveContainer, Tooltip } from "recharts";
import { Monitor, Code2, Send } from "lucide-react";
import type { DecisionLog } from "@/types/alice";

interface SourceDistributionChartProps {
  decisions: DecisionLog[];
}

interface SourceData {
  name: string;
  value: number;
  color: string;
  icon: React.ComponentType<{ className?: string }>;
  label: string;
}

const SOURCE_CONFIG: Record<string, Omit<SourceData, 'value'>> = {
  terminal: {
    name: "terminal",
    color: "#6b7280",
    icon: Monitor,
    label: "Terminal"
  },
  vscode: {
    name: "vscode",
    color: "#8b5cf6",
    icon: Code2,
    label: "VS Code"
  },
  telegram: {
    name: "telegram",
    color: "#06b6d4",
    icon: Send,
    label: "Telegram"
  },
  unknown: {
    name: "unknown",
    color: "#64748b",
    icon: Monitor,
    label: "Unknown"
  }
};

function CustomTooltip({ active, payload }: any) {
  if (!active || !payload || !payload.length) return null;

  const data = payload[0].payload as SourceData;
  const IconComponent = data.icon;

  return (
    <div className="bg-gray-900/95 border border-gray-700 rounded-lg p-3 shadow-lg">
      <div className="flex items-center gap-2 mb-1">
        <IconComponent className="w-4 h-4 text-white" />
        <span className="text-sm font-medium text-white">{data.label}</span>
      </div>
      <div className="text-sm text-gray-300">
        {data.value} decisions ({((data.value / payload[0].payload.total) * 100).toFixed(1)}%)
      </div>
    </div>
  );
}

function CustomLegend({ payload }: any) {
  return (
    <div className="flex flex-wrap justify-center gap-4 mt-4">
      {payload?.map((entry: any, index: number) => {
        const IconComponent = entry.icon;
        if (!IconComponent) return null;
        return (
          <div key={index} className="flex items-center gap-2">
            <IconComponent className="w-3 h-3" style={{ color: entry.color }} />
            <span className="text-xs text-gray-400">{entry.label}</span>
            <span className="text-xs text-gray-500">({entry.value})</span>
          </div>
        );
      })}
    </div>
  );
}

export default function SourceDistributionChart({ decisions }: SourceDistributionChartProps) {
  const chartData = useMemo(() => {
    // Count decisions by source
    const sourceCounts: Record<string, number> = {};

    decisions.forEach(decision => {
      const source = decision.source || 'telegram'; // Default to telegram for legacy data
      sourceCounts[source] = (sourceCounts[source] || 0) + 1;
    });

    // Convert to chart data with configuration
    const data: SourceData[] = Object.entries(sourceCounts).map(([source, count]) => {
      const config = SOURCE_CONFIG[source] || SOURCE_CONFIG.unknown;
      return {
        ...config,
        value: count,
        total: decisions.length
      } as SourceData & { total: number };
    });

    // Sort by value descending
    return data.sort((a, b) => b.value - a.value);
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
        No source data available
      </div>
    );
  }

  return (
    <div className="w-full">
      <ResponsiveContainer width="100%" height={200}>
        <PieChart>
          <Pie
            data={chartData}
            cx="50%"
            cy="50%"
            innerRadius={40}
            outerRadius={80}
            paddingAngle={2}
            dataKey="value"
          >
            {chartData.map((entry, index) => (
              <Cell key={`cell-${index}`} fill={entry.color} />
            ))}
          </Pie>
          <Tooltip content={<CustomTooltip />} />
        </PieChart>
      </ResponsiveContainer>
      <CustomLegend payload={chartData} />
    </div>
  );
}