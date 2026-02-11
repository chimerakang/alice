import { useEffect, useState } from "react";
import { api } from "@/lib/api";
import type { PerformanceAnalytics } from "@/types/alice";
import { BarChart3, Clock, CheckCircle, AlertTriangle } from "lucide-react";

export default function Performance() {
  const [analytics, setAnalytics] = useState<PerformanceAnalytics | null>(null);

  useEffect(() => {
    api.getPerformanceAnalytics().then(setAnalytics).catch(() => {});
    const interval = setInterval(() => {
      api.getPerformanceAnalytics().then(setAnalytics).catch(() => {});
    }, 15000);
    return () => clearInterval(interval);
  }, []);

  if (!analytics) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="w-8 h-8 border-2 border-primary border-t-transparent rounded-full animate-spin" />
      </div>
    );
  }

  return (
    <div className="space-y-6 animate-fade-in">
      <h2 className="text-lg font-semibold text-white">Performance Analytics</h2>

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
            {analytics.avg_response_time.toFixed(0)}ms
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

      <div className="card p-6 text-center text-gray-500">
        <BarChart3 className="w-8 h-8 mx-auto mb-2 text-gray-600" />
        <p>Recharts trend visualization will be added in #18.</p>
      </div>
    </div>
  );
}
