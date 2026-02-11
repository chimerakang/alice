import { useEffect, useState } from "react";
import { api } from "@/lib/api";
import { useAppStore } from "@/stores/appStore";
import type { StatsResponse } from "@/types/alice";
import {
  Activity,
  Cpu,
  GitBranch,
  Shield,
  Zap,
  Terminal,
} from "lucide-react";

function MetricCard({
  label,
  value,
  icon: Icon,
  color,
}: {
  label: string;
  value: string | number;
  icon: React.ComponentType<{ className?: string }>;
  color: string;
}) {
  return (
    <div className="card p-5">
      <div className="flex items-center justify-between mb-3">
        <span className="text-sm text-gray-400">{label}</span>
        <Icon className={`w-5 h-5 ${color}`} />
      </div>
      <div className="text-3xl font-bold font-mono tracking-tight text-white">
        {value}
      </div>
    </div>
  );
}

export default function Dashboard() {
  const { stats, setStats, agents, toolExecutions, decisions, wsConnected } =
    useAppStore();
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    const load = async () => {
      try {
        const s = await api.getStats();
        setStats(s);
      } catch {
        // API not available yet
      } finally {
        setLoading(false);
      }
    };
    load();
    const interval = setInterval(load, 30000);
    return () => clearInterval(interval);
  }, [setStats]);

  const s: StatsResponse = stats ?? {
    active_sessions: 0,
    total_sessions: 0,
    tools_executed: 0,
    total_projects: 0,
    success_rate: 100,
    uptime_seconds: 0,
    timestamp: "",
    total_tokens_used: 0,
    total_cost_usd: 0,
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="w-8 h-8 border-2 border-primary border-t-transparent rounded-full animate-spin" />
      </div>
    );
  }

  return (
    <div className="space-y-6 animate-fade-in">
      {/* Metric Cards */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        <MetricCard
          label="Active Agents"
          value={agents.length || s.active_sessions}
          icon={Cpu}
          color="text-success"
        />
        <MetricCard
          label="Tools Executed"
          value={toolExecutions.length || s.tools_executed}
          icon={Terminal}
          color="text-primary"
        />
        <MetricCard
          label="Decisions"
          value={decisions.length}
          icon={Zap}
          color="text-accent"
        />
        <MetricCard
          label="Success Rate"
          value={`${s.success_rate.toFixed(0)}%`}
          icon={Activity}
          color="text-success"
        />
      </div>

      {/* Secondary Stats */}
      <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
        <MetricCard
          label="Total Tokens"
          value={s.total_tokens_used.toLocaleString()}
          icon={Zap}
          color="text-warning"
        />
        <MetricCard
          label="Total Cost"
          value={`$${s.total_cost_usd.toFixed(4)}`}
          icon={Activity}
          color="text-primary-light"
        />
        <MetricCard
          label="Projects"
          value={s.total_projects}
          icon={GitBranch}
          color="text-info"
        />
      </div>

      {/* Live Feeds */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        {/* Recent Tools */}
        <div className="card p-5">
          <h3 className="text-sm font-semibold text-gray-400 mb-4 flex items-center gap-2">
            <Terminal className="w-4 h-4 text-primary" />
            Recent Tool Executions
            {wsConnected && (
              <span className="status-dot status-dot-success ml-auto" />
            )}
          </h3>
          {toolExecutions.length === 0 ? (
            <p className="text-gray-500 text-sm">
              Waiting for tool executions...
            </p>
          ) : (
            <div className="space-y-2 max-h-64 overflow-y-auto">
              {toolExecutions.slice(0, 10).map((t, i) => (
                <div
                  key={t.id || i}
                  className="flex items-center justify-between text-sm py-1.5 border-b border-gray-800 last:border-0"
                >
                  <span className="font-mono text-primary-light">
                    {t.tool_name}
                  </span>
                  <span
                    className={
                      t.status === "STATUS_SUCCESS"
                        ? "text-success"
                        : t.status === "STATUS_ERROR"
                          ? "text-error"
                          : "text-warning"
                    }
                  >
                    {t.duration_ms}ms
                  </span>
                </div>
              ))}
            </div>
          )}
        </div>

        {/* Recent Decisions */}
        <div className="card p-5">
          <h3 className="text-sm font-semibold text-gray-400 mb-4 flex items-center gap-2">
            <Shield className="w-4 h-4 text-accent" />
            Recent Decisions
          </h3>
          {decisions.length === 0 ? (
            <p className="text-gray-500 text-sm">
              Waiting for AI decisions...
            </p>
          ) : (
            <div className="space-y-2 max-h-64 overflow-y-auto">
              {decisions.slice(0, 10).map((d, i) => (
                <div
                  key={d.id || i}
                  className="flex items-center justify-between text-sm py-1.5 border-b border-gray-800 last:border-0"
                >
                  <span className="text-gray-300 truncate max-w-[200px]">
                    {d.task_type || "general"}
                  </span>
                  <span
                    className={
                      d.outcome?.success ? "text-success" : "text-error"
                    }
                  >
                    {d.tool_calls?.length || 0} tools
                  </span>
                </div>
              ))}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
