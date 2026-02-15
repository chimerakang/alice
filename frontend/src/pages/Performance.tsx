import { useEffect, useState, useCallback } from "react";
import { api } from "@/lib/api";
import type { PerformanceAnalytics, PerformanceMetric } from "@/types/alice";
import DateRangeFilter from "@/components/DateRangeFilter";
import type { DateRange } from "@/components/DateRangeFilter";
import { BarChart3, Clock, CheckCircle, AlertTriangle, Cpu, DollarSign, TrendingUp } from "lucide-react";
import {
  LineChart,
  Line,
  AreaChart,
  Area,
  BarChart,
  Bar,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
} from "recharts";

interface TrendsData {
  timestamp: string;
  response_time: number;
  tokens: number;
  cost: number;
  memory_mb: number;
  success_rate: number;
}

interface ToolDistribution {
  tool_type: string;
  avg_execution_time: number;
  count: number;
}

// 格式化響應時間為人類可讀格式
function formatResponseTime(ms: number): string {
  if (ms >= 60000) {
    // >= 1 minute: show as "2m 35s"
    const minutes = Math.floor(ms / 60000);
    const seconds = Math.floor((ms % 60000) / 1000);
    return seconds > 0 ? `${minutes}m ${seconds}s` : `${minutes}m`;
  } else if (ms >= 1000) {
    // >= 1 second: show as "1.5s"
    return `${(ms / 1000).toFixed(1)}s`;
  } else {
    // < 1 second: show as "123ms"
    return `${Math.round(ms)}ms`;
  }
}

export default function Performance() {
  const [analytics, setAnalytics] = useState<PerformanceAnalytics | null>(null);
  const [metrics, setMetrics] = useState<PerformanceMetric[]>([]);
  const [recommendations, setRecommendations] = useState<{ text: string; priority: string }[]>([]);
  const [toolDistribution, setToolDistribution] = useState<ToolDistribution[]>([]);
  const [loading, setLoading] = useState(true);
  const [dateRange, setDateRange] = useState<DateRange>({});

  const handleDateRangeChange = useCallback((range: DateRange) => {
    setDateRange(range);
  }, []);

  useEffect(() => {
    const loadData = async () => {
      try {
        setLoading(true);
        const [analyticsData, metricsData, recsData, toolDistData] = await Promise.allSettled([
          api.getPerformanceAnalytics({
            startTime: dateRange.startTime,
            endTime: dateRange.endTime,
          }),
          api.getPerformanceMetrics({
            limit: 200,
            startTime: dateRange.startTime,
            endTime: dateRange.endTime,
          }),
          api.getPerformanceRecommendations(),
          api.getToolDistribution({
            startTime: dateRange.startTime,
            endTime: dateRange.endTime,
          }),
        ]);

        if (analyticsData.status === "fulfilled") setAnalytics(analyticsData.value);
        if (metricsData.status === "fulfilled") {
          setMetrics(metricsData.value.metrics || []);
        }
        if (recsData.status === "fulfilled") {
          const recommendations = (recsData.value.recommendations || []) as { text: string; priority: string }[];
          setRecommendations(recommendations);
        }
        if (toolDistData.status === "fulfilled") {
          setToolDistribution(toolDistData.value.tool_distribution || []);
        }
      } catch (error) {
        console.error("Failed to load performance data:", error);
      } finally {
        setLoading(false);
      }
    };

    loadData();
    const interval = setInterval(loadData, 30000);
    return () => clearInterval(interval);
  }, [dateRange]);

  // Transform metrics data for charts
  const trendsData: TrendsData[] = metrics
    .slice(0, 20) // Last 20 data points
    .reverse() // Show chronologically
    .map((m) => ({
      timestamp: new Date(m.timestamp).toLocaleTimeString([], {
        hour: '2-digit',
        minute: '2-digit'
      }),
      response_time: Math.round(m.api_latency_ms), // Use the correct field name
      tokens: m.tokens_used,
      cost: Number(m.estimated_cost.toFixed(4)),
      memory_mb: Math.round(m.memory_usage / 1024 / 1024),
      success_rate: m.api_success ? 100 : 0,
    }));

  // Tool distribution is now loaded from API endpoint via state

  if (loading) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="w-8 h-8 border-2 border-primary border-t-transparent rounded-full animate-spin" />
      </div>
    );
  }

  return (
    <div className="space-y-6 animate-fade-in">
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold text-white">Performance Analytics</h2>
        <div className="text-xs text-gray-500">
          Last updated: {new Date().toLocaleTimeString()}
        </div>
      </div>

      {/* Date Range Filter */}
      <DateRangeFilter onChange={handleDateRangeChange} />

      {/* Summary Cards */}
      {analytics && (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
          <div className="card p-5">
            <div className="flex items-center gap-2 mb-2">
              <BarChart3 className="w-4 h-4 text-primary" />
              <span className="text-sm text-gray-400">Total Operations</span>
            </div>
            <div className="text-2xl font-bold font-mono text-white">
              {analytics.total_operations}
            </div>
          </div>

          <div className="card p-5">
            <div className="flex items-center gap-2 mb-2">
              <Clock className="w-4 h-4 text-warning" />
              <span className="text-sm text-gray-400">Avg Response Time</span>
            </div>
            <div className="text-2xl font-bold font-mono text-white">
              {formatResponseTime(analytics.avg_response_time)}
            </div>
          </div>

          <div className="card p-5">
            <div className="flex items-center gap-2 mb-2">
              <AlertTriangle className="w-4 h-4 text-error" />
              <span className="text-sm text-gray-400">Error Rate</span>
            </div>
            <div className="text-2xl font-bold font-mono text-white">
              {(analytics.error_rate * 100).toFixed(1)}%
            </div>
          </div>

          <div className="card p-5">
            <div className="flex items-center gap-2 mb-2">
              <CheckCircle className="w-4 h-4 text-success" />
              <span className="text-sm text-gray-400">Throughput</span>
            </div>
            <div className="text-2xl font-bold font-mono text-white">
              {analytics.throughput.toFixed(1)}/hr
            </div>
          </div>
        </div>
      )}

      {/* Charts Grid */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        {/* API Response Time Trend */}
        <div className="card p-6">
          <h3 className="text-sm font-semibold text-gray-300 mb-4 flex items-center gap-2">
            <TrendingUp className="w-4 h-4" />
            API Response Time Trend
          </h3>
          <ResponsiveContainer width="100%" height={200}>
            <LineChart data={trendsData}>
              <CartesianGrid strokeDasharray="3 3" stroke="#374151" />
              <XAxis
                dataKey="timestamp"
                stroke="#9CA3AF"
                fontSize={12}
                tick={{ fill: '#9CA3AF' }}
              />
              <YAxis
                stroke="#9CA3AF"
                fontSize={12}
                tick={{ fill: '#9CA3AF' }}
              />
              <Tooltip
                contentStyle={{
                  backgroundColor: '#1F2937',
                  border: '1px solid #374151',
                  borderRadius: '8px',
                  color: '#F9FAFB'
                }}
                labelStyle={{ color: '#D1D5DB' }}
              />
              <Line
                type="monotone"
                dataKey="response_time"
                stroke="#3B82F6"
                strokeWidth={2}
                dot={{ fill: '#3B82F6', strokeWidth: 2, r: 4 }}
                activeDot={{ r: 6, stroke: '#3B82F6', strokeWidth: 2 }}
              />
            </LineChart>
          </ResponsiveContainer>
        </div>

        {/* Cost Trend */}
        <div className="card p-6">
          <h3 className="text-sm font-semibold text-gray-300 mb-4 flex items-center gap-2">
            <DollarSign className="w-4 h-4" />
            Cost Trend (USD)
          </h3>
          <ResponsiveContainer width="100%" height={200}>
            <AreaChart data={trendsData}>
              <CartesianGrid strokeDasharray="3 3" stroke="#374151" />
              <XAxis
                dataKey="timestamp"
                stroke="#9CA3AF"
                fontSize={12}
                tick={{ fill: '#9CA3AF' }}
              />
              <YAxis
                stroke="#9CA3AF"
                fontSize={12}
                tick={{ fill: '#9CA3AF' }}
                tickFormatter={(value) => `$${value}`}
              />
              <Tooltip
                contentStyle={{
                  backgroundColor: '#1F2937',
                  border: '1px solid #374151',
                  borderRadius: '8px',
                  color: '#F9FAFB'
                }}
                labelStyle={{ color: '#D1D5DB' }}
                formatter={(value) => [`$${value}`, 'Cost']}
              />
              <Area
                type="monotone"
                dataKey="cost"
                stroke="#10B981"
                fill="#10B981"
                fillOpacity={0.2}
                strokeWidth={2}
              />
            </AreaChart>
          </ResponsiveContainer>
        </div>

        {/* Token Usage */}
        <div className="card p-6">
          <h3 className="text-sm font-semibold text-gray-300 mb-4 flex items-center gap-2">
            <BarChart3 className="w-4 h-4" />
            Token Usage Over Time
          </h3>
          <ResponsiveContainer width="100%" height={200}>
            <BarChart data={trendsData}>
              <CartesianGrid strokeDasharray="3 3" stroke="#374151" />
              <XAxis
                dataKey="timestamp"
                stroke="#9CA3AF"
                fontSize={12}
                tick={{ fill: '#9CA3AF' }}
              />
              <YAxis
                stroke="#9CA3AF"
                fontSize={12}
                tick={{ fill: '#9CA3AF' }}
              />
              <Tooltip
                contentStyle={{
                  backgroundColor: '#1F2937',
                  border: '1px solid #374151',
                  borderRadius: '8px',
                  color: '#F9FAFB'
                }}
                labelStyle={{ color: '#D1D5DB' }}
              />
              <Bar
                dataKey="tokens"
                fill="#8B5CF6"
                radius={[2, 2, 0, 0]}
              />
            </BarChart>
          </ResponsiveContainer>
        </div>

        {/* Memory Usage */}
        <div className="card p-6">
          <h3 className="text-sm font-semibold text-gray-300 mb-4 flex items-center gap-2">
            <Cpu className="w-4 h-4" />
            Memory Usage (MB)
          </h3>
          <ResponsiveContainer width="100%" height={200}>
            <AreaChart data={trendsData}>
              <CartesianGrid strokeDasharray="3 3" stroke="#374151" />
              <XAxis
                dataKey="timestamp"
                stroke="#9CA3AF"
                fontSize={12}
                tick={{ fill: '#9CA3AF' }}
              />
              <YAxis
                stroke="#9CA3AF"
                fontSize={12}
                tick={{ fill: '#9CA3AF' }}
                tickFormatter={(value) => `${value}MB`}
              />
              <Tooltip
                contentStyle={{
                  backgroundColor: '#1F2937',
                  border: '1px solid #374151',
                  borderRadius: '8px',
                  color: '#F9FAFB'
                }}
                labelStyle={{ color: '#D1D5DB' }}
                formatter={(value) => [`${value}MB`, 'Memory']}
              />
              <Area
                type="monotone"
                dataKey="memory_mb"
                stroke="#F59E0B"
                fill="#F59E0B"
                fillOpacity={0.2}
                strokeWidth={2}
              />
            </AreaChart>
          </ResponsiveContainer>
        </div>
      </div>

      {/* Tool Execution Distribution */}
      <div className="card p-6">
        <h3 className="text-sm font-semibold text-gray-300 mb-4 flex items-center gap-2">
          <BarChart3 className="w-4 h-4" />
          Tool Execution Time Distribution
        </h3>
        <ResponsiveContainer width="100%" height={250}>
          <BarChart data={toolDistribution} layout="horizontal">
            <CartesianGrid strokeDasharray="3 3" stroke="#374151" />
            <XAxis
              type="number"
              stroke="#9CA3AF"
              fontSize={12}
              tick={{ fill: '#9CA3AF' }}
              tickFormatter={(value) => `${value}ms`}
            />
            <YAxis
              type="category"
              dataKey="tool_type"
              stroke="#9CA3AF"
              fontSize={12}
              tick={{ fill: '#9CA3AF' }}
              width={60}
            />
            <Tooltip
              contentStyle={{
                backgroundColor: '#1F2937',
                border: '1px solid #374151',
                borderRadius: '8px',
                color: '#F9FAFB'
              }}
              labelStyle={{ color: '#D1D5DB' }}
              formatter={(value, name) => [
                name === 'avg_execution_time' ? `${value}ms` : value,
                name === 'avg_execution_time' ? 'Avg Time' : 'Count'
              ]}
            />
            <Bar
              dataKey="avg_execution_time"
              fill="#06B6D4"
              radius={[0, 4, 4, 0]}
            />
          </BarChart>
        </ResponsiveContainer>
      </div>

      {/* Performance Recommendations */}
      {recommendations.length > 0 && (
        <div className="card p-6">
          <h3 className="text-sm font-semibold text-gray-300 mb-4 flex items-center gap-2">
            <CheckCircle className="w-4 h-4" />
            Performance Optimization Recommendations
          </h3>
          <div className="space-y-3">
            {recommendations.map((rec, i) => (
              <div
                key={i}
                className="flex items-start gap-3 p-3 rounded-lg bg-gray-800/50 border border-gray-700/50"
              >
                <div className={`w-2 h-2 rounded-full mt-1.5 ${
                  rec.priority === 'high' ? 'bg-red-500' :
                  rec.priority === 'medium' ? 'bg-yellow-500' : 'bg-green-500'
                }`} />
                <div className="flex-1">
                  <p className="text-sm text-gray-300">{rec.text}</p>
                  <span className={`text-xs px-2 py-1 rounded-full mt-1 inline-block ${
                    rec.priority === 'high' ? 'bg-red-500/20 text-red-300' :
                    rec.priority === 'medium' ? 'bg-yellow-500/20 text-yellow-300' :
                    'bg-green-500/20 text-green-300'
                  }`}>
                    {rec.priority} priority
                  </span>
                </div>
              </div>
            ))}
          </div>
        </div>
      )}

      {metrics.length === 0 && (
        <div className="card p-6 text-center text-gray-500">
          <BarChart3 className="w-8 h-8 mx-auto mb-2 text-gray-600" />
          <p>No performance metrics available yet.</p>
          <p className="text-sm mt-1">Charts will populate as the system processes requests.</p>
        </div>
      )}
    </div>
  );
}