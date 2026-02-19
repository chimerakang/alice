import { useEffect, useState, useMemo, useCallback } from "react";
import { useNavigate } from "react-router-dom";
import { api } from "@/lib/api";
import { useAppStore } from "@/stores/appStore";
import type { StatsResponse, DecisionLog, GitState } from "@/types/alice";
import DateRangeFilter from "@/components/DateRangeFilter";
import type { DateRange } from "@/components/DateRangeFilter";
import StatusBadge from "@/components/StatusBadge";
import {
  Activity,
  Cpu,
  GitBranch,
  Zap,
  Terminal,
  Clock,
  Database,
  Wifi,
  WifiOff,
  ArrowRight,
  Bot,
  CheckCircle2,
  XCircle,
  HardDrive,
  FileCode2,
  AlertTriangle,
  Loader2,
  TrendingDown,
} from "lucide-react";
import {
  AreaChart,
  Area,
  BarChart,
  Bar,
  XAxis,
  YAxis,
  Tooltip,
  ResponsiveContainer,
  CartesianGrid,
  PieChart,
  Pie,
  Cell,
} from "recharts";
import SourceDistributionChart from "@/components/SourceDistributionChart";
import SourcePerformanceChart from "@/components/SourcePerformanceChart";
import { SavingsBanner } from "@/components/SavingsBanner";
import { ModelDistributionChart } from "@/components/ModelDistributionChart";
import { CostTrendChart } from "@/components/CostTrendChart";

// ─── Storage Stats Type ──────────────────────────────
interface StorageStats {
  persistence: boolean;
  timestamp: string;
  database_stats?: {
    database_size_bytes: number;
    decision_logs_count: number;
    performance_metrics_count: number;
    security_events_count: number;
    tool_executions_count: number;
  };
}

// ─── Metric Card ─────────────────────────────────────
function MetricCard({
  label,
  value,
  sub,
  icon: Icon,
  color,
}: {
  label: string;
  value: string | number;
  sub?: string;
  icon: React.ComponentType<{ className?: string }>;
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
      {sub && <p className="text-xs text-gray-500 mt-1">{sub}</p>}
    </div>
  );
}

// ─── Git Status Card (multi-project) ─────────────────
function GitStatusCard({ projectGitStates }: { projectGitStates: Map<string, GitState> }) {
  const [selected, setSelected] = useState(0);

  const entries = useMemo(() => Array.from(projectGitStates.entries()), [projectGitStates]);

  if (entries.length === 0) {
    return (
      <div className="card p-5">
        <h3 className="text-sm font-semibold text-gray-400 mb-3 flex items-center gap-2">
          <GitBranch className="w-4 h-4 text-info" />
          Git Status
        </h3>
        <p className="text-sm text-gray-500">No git information available</p>
      </div>
    );
  }

  const [projectPath, gitState] = entries[selected] || entries[0];
  const projectName = projectPath.split("/").filter(Boolean).pop() || projectPath;
  const modifiedCount = gitState?.modified_files?.length || 0;

  return (
    <div className="card p-5">
      <h3 className="text-sm font-semibold text-gray-400 mb-3 flex items-center gap-2">
        <GitBranch className="w-4 h-4 text-info" />
        Git Status
        {gitState && (
          <StatusBadge variant={gitState.is_dirty ? "warning" : "success"} size="sm" dot>
            {gitState.is_dirty ? "Dirty" : "Clean"}
          </StatusBadge>
        )}
      </h3>

      {/* Project tabs */}
      {entries.length > 1 && (
        <div className="flex items-center gap-1 mb-3 flex-wrap">
          {entries.map(([p], i) => (
            <button
              key={p}
              onClick={() => setSelected(i)}
              className={`px-2 py-0.5 text-[10px] rounded-full border transition-colors ${
                selected === i
                  ? "bg-primary/15 text-primary border-primary/30"
                  : "text-gray-500 border-gray-700 hover:border-gray-600 hover:text-gray-300"
              }`}
            >
              {p.split("/").filter(Boolean).pop() || p}
            </button>
          ))}
        </div>
      )}

      {gitState ? (
        <div className="space-y-2 text-sm">
          {entries.length <= 1 && (
            <div className="flex items-center justify-between">
              <span className="text-gray-500">Project</span>
              <span className="text-gray-300 text-xs">{projectName}</span>
            </div>
          )}
          <div className="flex items-center justify-between">
            <span className="text-gray-500">Branch</span>
            <span className="font-mono text-primary-light">{gitState.branch}</span>
          </div>
          <div className="flex items-center justify-between">
            <span className="text-gray-500">Commit</span>
            <span className="font-mono text-gray-300">{gitState.commit_hash?.slice(0, 8)}</span>
          </div>
          {gitState.remote_url && (
            <div className="flex items-center justify-between">
              <span className="text-gray-500">Remote</span>
              <span className="text-gray-400 text-xs truncate max-w-[200px]">
                {gitState.remote_url.replace(/^https?:\/\//, "").replace(/\.git$/, "")}
              </span>
            </div>
          )}
          {modifiedCount > 0 && (
            <div className="flex items-center justify-between">
              <span className="text-gray-500">Modified</span>
              <span className="text-warning font-mono">{modifiedCount} files</span>
            </div>
          )}
        </div>
      ) : (
        <p className="text-sm text-gray-500">Loading...</p>
      )}
    </div>
  );
}

// ─── System Status Card ──────────────────────────────
function SystemStatusCard({
  wsConnected,
  stats,
  storageStats,
}: {
  wsConnected: boolean;
  stats: StatsResponse | null;
  storageStats: StorageStats | null;
}) {
  const uptimeStr = useMemo(() => {
    if (!stats?.uptime_seconds) return "—";
    const s = stats.uptime_seconds;
    if (s < 3600) return `${Math.floor(s / 60)}m`;
    if (s < 86400) return `${Math.floor(s / 3600)}h ${Math.floor((s % 3600) / 60)}m`;
    return `${Math.floor(s / 86400)}d ${Math.floor((s % 86400) / 3600)}h`;
  }, [stats?.uptime_seconds]);

  const dbSize = useMemo(() => {
    if (!storageStats?.database_stats?.database_size_bytes) return "—";
    const bytes = storageStats.database_stats.database_size_bytes;
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
    return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  }, [storageStats]);

  return (
    <div className="card p-5">
      <h3 className="text-sm font-semibold text-gray-400 mb-3 flex items-center gap-2">
        <Activity className="w-4 h-4 text-success" />
        System Status
      </h3>
      <div className="space-y-2.5">
        <div className="flex items-center justify-between text-sm">
          <span className="text-gray-500 flex items-center gap-1.5">
            {wsConnected ? <Wifi className="w-3 h-3 text-success" /> : <WifiOff className="w-3 h-3 text-error" />}
            WebSocket
          </span>
          <span className={wsConnected ? "text-success" : "text-error"}>
            {wsConnected ? "Connected" : "Disconnected"}
          </span>
        </div>
        <div className="flex items-center justify-between text-sm">
          <span className="text-gray-500 flex items-center gap-1.5">
            <Bot className="w-3 h-3" />
            Telegram
          </span>
          <span className="text-success">Active</span>
        </div>
        <div className="flex items-center justify-between text-sm">
          <span className="text-gray-500 flex items-center gap-1.5">
            <Clock className="w-3 h-3" />
            Uptime
          </span>
          <span className="text-gray-300 font-mono">{uptimeStr}</span>
        </div>
        <div className="flex items-center justify-between text-sm">
          <span className="text-gray-500 flex items-center gap-1.5">
            <Database className="w-3 h-3" />
            Storage
          </span>
          <span className="text-gray-300 font-mono">{dbSize}</span>
        </div>
        {storageStats && (
          <div className="pt-2 border-t border-gray-800 grid grid-cols-2 gap-2">
            <div className="text-center">
              <div className="text-lg font-bold font-mono text-white">
                {storageStats.database_stats?.decision_logs_count ?? 0}
              </div>
              <div className="text-[10px] text-gray-500 uppercase">Decisions</div>
            </div>
            <div className="text-center">
              <div className="text-lg font-bold font-mono text-white">
                {storageStats.database_stats?.security_events_count ?? 0}
              </div>
              <div className="text-[10px] text-gray-500 uppercase">Sec Events</div>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

// ─── Recent Decisions Card ───────────────────────────
function RecentDecisionsCard({ decisions }: { decisions: DecisionLog[] }) {
  const navigate = useNavigate();

  return (
    <div className="card p-5">
      <div className="flex items-center justify-between mb-4">
        <h3 className="text-sm font-semibold text-gray-400 flex items-center gap-2">
          <Zap className="w-4 h-4 text-accent" />
          Recent AI Decisions
        </h3>
        <button
          onClick={() => navigate("/timeline")}
          className="text-xs text-primary hover:text-primary-light flex items-center gap-1 transition-colors"
        >
          View all <ArrowRight className="w-3 h-3" />
        </button>
      </div>
      {decisions.length === 0 ? (
        <p className="text-sm text-gray-500">No decisions yet</p>
      ) : (
        <div className="space-y-3">
          {decisions.slice(0, 5).map((d) => {
            const isSuccess = d.outcome?.success;
            const toolCount = d.tool_calls?.length || 0;
            const promptPreview = d.user_prompt
              ? d.user_prompt.slice(0, 80) + (d.user_prompt.length > 80 ? "…" : "")
              : "";

            return (
              <button
                key={d.id}
                onClick={() => navigate("/timeline")}
                className="w-full text-left p-3 rounded-lg border border-gray-800/60 hover:border-gray-700 hover:bg-white/[0.02] transition-all group"
              >
                <div className="flex items-start gap-2">
                  {isSuccess ? (
                    <CheckCircle2 className="w-3.5 h-3.5 text-success mt-0.5 shrink-0" />
                  ) : (
                    <XCircle className="w-3.5 h-3.5 text-error mt-0.5 shrink-0" />
                  )}
                  <div className="min-w-0 flex-1">
                    <p className="text-sm text-gray-200 leading-snug truncate group-hover:text-white transition-colors">
                      {promptPreview || "No prompt"}
                    </p>
                    <div className="flex items-center gap-3 mt-1 text-xs text-gray-500">
                      <span>{d.task_type || "general"}</span>
                      <span>{toolCount} tools</span>
                      {d.duration_ms > 0 && <span>{(d.duration_ms / 1000).toFixed(1)}s</span>}
                      {d.project_path && (
                        <span className="text-gray-600 truncate max-w-[120px]">
                          {d.project_path.split("/").pop()}
                        </span>
                      )}
                    </div>
                  </div>
                </div>
              </button>
            );
          })}
        </div>
      )}
    </div>
  );
}

// ─── Chart: Activity Timeline (Area) ─────────────────
function ActivityChart({ decisions }: { decisions: DecisionLog[] }) {
  const chartData = useMemo(() => {
    if (decisions.length === 0) return [];

    const now = Date.now();
    const buckets: Record<string, { total: number; success: number; error: number }> = {};

    for (let i = 23; i >= 0; i--) {
      const ts = new Date(now - i * 3600000);
      const key = ts.toLocaleTimeString("en", { hour: "2-digit", hour12: false });
      buckets[key] = { total: 0, success: 0, error: 0 };
    }

    for (const d of decisions) {
      const ts = typeof d.timestamp === "string"
        ? new Date(d.timestamp)
        : new Date((d.timestamp as unknown as { seconds: number }).seconds * 1000);
      if (now - ts.getTime() > 24 * 3600000) continue;

      const key = ts.toLocaleTimeString("en", { hour: "2-digit", hour12: false });
      if (buckets[key]) {
        buckets[key].total++;
        if (d.outcome?.success) buckets[key].success++;
        else buckets[key].error++;
      }
    }

    return Object.entries(buckets).map(([hour, v]) => ({
      hour,
      ...v,
    }));
  }, [decisions]);

  if (chartData.length === 0) {
    return <EmptyChart label="No activity data in the last 24h" />;
  }

  return (
    <ResponsiveContainer width="100%" height={180}>
      <AreaChart data={chartData} margin={{ top: 5, right: 5, left: -20, bottom: 0 }}>
        <defs>
          <linearGradient id="activityGrad" x1="0" y1="0" x2="0" y2="1">
            <stop offset="5%" stopColor="#6366f1" stopOpacity={0.3} />
            <stop offset="95%" stopColor="#6366f1" stopOpacity={0} />
          </linearGradient>
        </defs>
        <CartesianGrid strokeDasharray="3 3" stroke="#1f2937" />
        <XAxis dataKey="hour" tick={{ fill: "#6b7280", fontSize: 10 }} tickLine={false} axisLine={false} />
        <YAxis tick={{ fill: "#6b7280", fontSize: 10 }} tickLine={false} axisLine={false} allowDecimals={false} />
        <Tooltip
          contentStyle={{ backgroundColor: "#111827", border: "1px solid #374151", borderRadius: "0.5rem", fontSize: 12 }}
          labelStyle={{ color: "#9ca3af" }}
        />
        <Area type="monotone" dataKey="total" stroke="#6366f1" fill="url(#activityGrad)" strokeWidth={2} />
      </AreaChart>
    </ResponsiveContainer>
  );
}

// ─── Chart: Token Usage (Bar) ────────────────────────
function TokenChart({ decisions }: { decisions: DecisionLog[] }) {
  const chartData = useMemo(() => {
    if (decisions.length === 0) return [];

    const now = Date.now();
    const buckets: Record<string, { input: number; output: number }> = {};

    for (let i = 6; i >= 0; i--) {
      const ts = new Date(now - i * 86400000);
      const key = ts.toLocaleDateString("en", { month: "short", day: "numeric" });
      buckets[key] = { input: 0, output: 0 };
    }

    for (const d of decisions) {
      const ts = typeof d.timestamp === "string"
        ? new Date(d.timestamp)
        : new Date((d.timestamp as unknown as { seconds: number }).seconds * 1000);
      if (now - ts.getTime() > 7 * 86400000) continue;

      const key = ts.toLocaleDateString("en", { month: "short", day: "numeric" });
      if (buckets[key]) {
        buckets[key].input += d.tokens_input || 0;
        buckets[key].output += d.tokens_output || 0;
      }
    }

    return Object.entries(buckets).map(([day, v]) => ({
      day,
      ...v,
    }));
  }, [decisions]);

  if (chartData.length === 0) {
    return <EmptyChart label="No token data in the last 7 days" />;
  }

  return (
    <ResponsiveContainer width="100%" height={180}>
      <BarChart data={chartData} margin={{ top: 5, right: 5, left: -20, bottom: 0 }}>
        <CartesianGrid strokeDasharray="3 3" stroke="#1f2937" />
        <XAxis dataKey="day" tick={{ fill: "#6b7280", fontSize: 10 }} tickLine={false} axisLine={false} />
        <YAxis tick={{ fill: "#6b7280", fontSize: 10 }} tickLine={false} axisLine={false} />
        <Tooltip
          contentStyle={{ backgroundColor: "#111827", border: "1px solid #374151", borderRadius: "0.5rem", fontSize: 12 }}
          labelStyle={{ color: "#9ca3af" }}
        />
        <Bar dataKey="input" fill="#6366f1" radius={[3, 3, 0, 0]} name="Input" />
        <Bar dataKey="output" fill="#22d3ee" radius={[3, 3, 0, 0]} name="Output" />
      </BarChart>
    </ResponsiveContainer>
  );
}

// ─── Chart: Tool Success Rate (Pie) ──────────────────
function ToolSuccessChart({ toolExecutions, decisions }: { toolExecutions: any[]; decisions: DecisionLog[] }) {
  const data = useMemo(() => {
    let success = 0;
    let error = 0;

    const isSuccess = (s: string) =>
      s === "STATUS_SUCCESS" || s === "3" || s === "executed" || s === "success";
    const isError = (s: string) =>
      s === "STATUS_ERROR" || s === "4" || s === "error";

    // Extract tool calls from decisions (primary source — tool_executions table is not populated)
    for (const d of decisions) {
      for (const t of d.tool_calls || []) {
        const s = String(t.status);
        if (isSuccess(s)) success++;
        else if (isError(s)) error++;
      }
    }

    // Also count any live WebSocket tool executions not yet in decisions
    if (toolExecutions && toolExecutions.length > 0) {
      for (const t of toolExecutions) {
        const s = String(t.status);
        if (isSuccess(s)) success++;
        else if (isError(s)) error++;
      }
    }

    if (success + error === 0) return [];
    return [
      { name: "Success", value: success, color: "#22c55e" },
      { name: "Error", value: error, color: "#ef4444" },
    ];
  }, [toolExecutions, decisions]);

  if (data.length === 0) {
    return <EmptyChart label="No tool execution data" />;
  }

  const total = data.reduce((s, d) => s + d.value, 0);
  const rate = total > 0 ? ((data[0].value / total) * 100).toFixed(0) : "0";

  return (
    <div className="flex items-center gap-4">
      <ResponsiveContainer width={100} height={100}>
        <PieChart>
          <Pie
            data={data}
            cx="50%"
            cy="50%"
            innerRadius={30}
            outerRadius={45}
            dataKey="value"
            strokeWidth={0}
          >
            {data.map((d, i) => (
              <Cell key={i} fill={d.color} />
            ))}
          </Pie>
        </PieChart>
      </ResponsiveContainer>
      <div className="space-y-1">
        <div className="text-2xl font-bold font-mono text-white">{rate}%</div>
        <div className="text-xs text-gray-500">Tool success rate</div>
        <div className="flex items-center gap-3 text-xs">
          <span className="flex items-center gap-1">
            <span className="w-2 h-2 rounded-full bg-success" /> {data[0].value} ok
          </span>
          <span className="flex items-center gap-1">
            <span className="w-2 h-2 rounded-full bg-error" /> {data[1]?.value || 0} err
          </span>
        </div>
      </div>
    </div>
  );
}

// ─── Empty Chart Placeholder ─────────────────────────
function EmptyChart({ label }: { label: string }) {
  return (
    <div className="flex items-center justify-center h-[180px] text-sm text-gray-600">
      {label}
    </div>
  );
}

// ─── Main Dashboard ──────────────────────────────────
export default function Dashboard() {
  const { stats, setStats, decisions, wsConnected, toolExecutions, setToolExecutions } = useAppStore();
  const [loading, setLoading] = useState(true);
  const [projectGitStates, setProjectGitStates] = useState<Map<string, GitState>>(new Map());
  const [recentDecisions, setRecentDecisions] = useState<DecisionLog[]>([]);
  const [storageStats, setStorageStats] = useState<StorageStats | null>(null);
  const [dateRange, setDateRange] = useState<DateRange>({});
  const [tokenChartDecisions, setTokenChartDecisions] = useState<DecisionLog[]>([]);

  const handleDateRangeChange = useCallback((range: DateRange) => {
    setDateRange(range);
  }, []);

  // Calculate hours from dateRange for cost/savings charts
  const chartHours = useMemo(() => {
    if (!dateRange.startTime || !dateRange.endTime) {
      return 168; // Default to 7 days if no range selected
    }
    const start = new Date(dateRange.startTime);
    const end = new Date(dateRange.endTime);
    const hours = Math.round((end.getTime() - start.getTime()) / (1000 * 60 * 60));
    return Math.max(1, hours); // Minimum 1 hour
  }, [dateRange]);

  // Fetch decisions + basic stats to discover project paths
  useEffect(() => {
    const load = async () => {
      const results = await Promise.allSettled([
        api.getStats(),
        api.getDecisionsByRange({
          limit: 2000,
          startTime: dateRange.startTime,
          endTime: dateRange.endTime,
        }),
        api.getStorageStats(),
        // Load tool execution history for ToolSuccessChart
        api.getRecentTools({
          limit: 500,
          startTime: dateRange.startTime,
          endTime: dateRange.endTime,
        }),
      ]);

      if (results[0].status === "fulfilled") setStats(results[0].value);
      if (results[1].status === "fulfilled") {
        const raw = results[1].value;
        setRecentDecisions(raw.decisions || []);
      }
      if (results[2].status === "fulfilled") {
        setStorageStats(results[2].value as StorageStats);
      }
      if (results[3].status === "fulfilled") {
        const toolsData = results[3].value;
        if (toolsData.executions) {
          setToolExecutions(toolsData.executions);
        }
      }

      setLoading(false);
    };

    load();
    const interval = setInterval(load, 30000);
    return () => clearInterval(interval);
  }, [setStats, dateRange]);

  // Fetch 7-day data specifically for Token Usage chart
  useEffect(() => {
    const loadTokenChartData = async () => {
      const now = new Date();
      const sevenDaysAgo = new Date(now.getTime() - 7 * 24 * 60 * 60 * 1000);

      try {
        const response = await api.getDecisionsByRange({
          limit: 2000,
          startTime: sevenDaysAgo.toISOString(),
          endTime: now.toISOString(),
        });
        setTokenChartDecisions(response.decisions || []);
      } catch (error) {
        console.error("Failed to load token chart data:", error);
      }
    };

    loadTokenChartData();
    const interval = setInterval(loadTokenChartData, 60000); // Refresh every minute
    return () => clearInterval(interval);
  }, []);

  // Merge store decisions with API-fetched ones for charts
  const allDecisions = useMemo(() => {
    const seen = new Set<string>();
    const merged: DecisionLog[] = [];
    for (const d of [...decisions, ...recentDecisions]) {
      if (!seen.has(d.id)) {
        seen.add(d.id);
        merged.push(d);
      }
    }
    return merged;
  }, [decisions, recentDecisions]);

  // Discover project paths from decisions (normalize trailing slashes)
  const projectPaths = useMemo(() => {
    const paths = new Set<string>();
    for (const d of allDecisions) {
      if (d.project_path) paths.add(d.project_path.replace(/\/+$/, ""));
    }

    // Always include current directory as default project
    if (paths.size === 0) {
      paths.add(".");
    }

    return Array.from(paths).sort();
  }, [allDecisions]);

  // Fetch git status for each discovered project
  useEffect(() => {
    if (projectPaths.length === 0) return;

    const fetchAll = async () => {
      const map = new Map<string, GitState>();
      const results = await Promise.allSettled(
        projectPaths.map((p) => api.getGitStatus(p)),
      );
      for (let i = 0; i < results.length; i++) {
        if (results[i].status === "fulfilled") {
          const raw = (results[i] as PromiseFulfilledResult<unknown>).value as
            | { git_state?: GitState }
            | GitState;
          const gs = "git_state" in raw && raw.git_state ? raw.git_state : (raw as GitState);
          if (gs?.branch) map.set(projectPaths[i], gs);
        }
      }
      setProjectGitStates(map);
    };

    fetchAll();
    const interval = setInterval(fetchAll, 30000);
    return () => clearInterval(interval);
  }, [projectPaths]);

  // Compute time-filtered stats from decisions (respects DateRangeFilter)
  const filteredStats = useMemo(() => {
    const sessionIds = new Set<string>();
    let toolCount = 0;
    let successCount = 0;
    let totalTokens = 0;
    let totalCost = 0;

    for (const d of allDecisions) {
      if (d.session_id) sessionIds.add(d.session_id);
      toolCount += d.tool_calls?.length || 0;
      if (d.outcome?.success) successCount++;
      totalTokens += (d.tokens_input || 0) + (d.tokens_output || 0);
      totalCost += d.cost_usd || 0;
    }

    const total = allDecisions.length;
    const successRate = total > 0 ? (successCount / total) * 100 : 100;

    return {
      sessions: sessionIds.size,
      tools: toolCount,
      decisions: total,
      successRate,
      totalTokens,
      totalCost,
    };
  }, [allDecisions]);

  if (loading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Loader2 className="w-8 h-8 text-primary animate-spin" />
      </div>
    );
  }

  return (
    <div className="space-y-6 animate-fade-in">
      {/* ── Time Range Filter ── */}
      <div className="flex items-center justify-between">
        <DateRangeFilter onChange={handleDateRangeChange} compact />
      </div>

      {/* ── Row 1: Metric Cards ── */}
      <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-6 gap-3">
        <MetricCard
          label="Sessions"
          value={filteredStats.sessions}
          sub={`${filteredStats.sessions} total`}
          icon={Cpu}
          color="text-success"
        />
        <MetricCard
          label="Tools"
          value={filteredStats.tools}
          icon={Terminal}
          color="text-primary"
        />
        <MetricCard
          label="Decisions"
          value={filteredStats.decisions}
          icon={Zap}
          color="text-accent"
        />
        <MetricCard
          label="Success"
          value={`${filteredStats.successRate.toFixed(0)}%`}
          icon={Activity}
          color="text-success"
        />
        <MetricCard
          label="Tokens"
          value={filteredStats.totalTokens > 1000 ? `${(filteredStats.totalTokens / 1000).toFixed(1)}k` : filteredStats.totalTokens}
          icon={FileCode2}
          color="text-warning"
        />
        <MetricCard
          label="Cost"
          value={`$${filteredStats.totalCost.toFixed(2)}`}
          icon={HardDrive}
          color="text-primary-light"
        />
      </div>

      {/* ── Row 2: Git + System Status + Tool Success ── */}
      <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
        <GitStatusCard projectGitStates={projectGitStates} />
        <SystemStatusCard
          wsConnected={wsConnected}
          stats={stats}
          storageStats={storageStats}
        />
        <div className="card p-5">
          <h3 className="text-sm font-semibold text-gray-400 mb-3 flex items-center gap-2">
            <CheckCircle2 className="w-4 h-4 text-success" />
            Tool Execution
          </h3>
          <ToolSuccessChart toolExecutions={toolExecutions} decisions={allDecisions} />
        </div>
      </div>

      {/* ── Row 3: Charts ── */}
      <div className="grid grid-cols-1 lg:grid-cols-2 xl:grid-cols-4 gap-4">
        <div className="card p-5">
          <h3 className="text-sm font-semibold text-gray-400 mb-3 flex items-center gap-2">
            <Activity className="w-4 h-4 text-primary" />
            Activity (24h)
          </h3>
          <ActivityChart decisions={allDecisions} />
        </div>
        <div className="card p-5">
          <h3 className="text-sm font-semibold text-gray-400 mb-3 flex items-center gap-2">
            <Zap className="w-4 h-4 text-warning" />
            Token Usage (7d)
          </h3>
          <TokenChart decisions={tokenChartDecisions} />
        </div>
        <div className="card p-5">
          <h3 className="text-sm font-semibold text-gray-400 mb-3 flex items-center gap-2">
            <Bot className="w-4 h-4 text-cyan-400" />
            Source Distribution
          </h3>
          <SourceDistributionChart decisions={allDecisions} />
        </div>
        <div className="card p-5">
          <h3 className="text-sm font-semibold text-gray-400 mb-3 flex items-center gap-2">
            <Cpu className="w-4 h-4 text-purple-400" />
            Source Performance
          </h3>
          <SourcePerformanceChart decisions={allDecisions} />
        </div>
      </div>

      {/* ── Row 4: Smart Routing Savings (Issue #74) ── */}
      <div className="space-y-4">
        <SavingsBanner hours={chartHours} />

        <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
          <div className="card p-5">
            <h3 className="text-sm font-semibold text-gray-400 mb-3 flex items-center gap-2">
              <Zap className="w-4 h-4 text-warning" />
              Model Distribution
            </h3>
            <ModelDistributionChart hours={chartHours} />
          </div>
          <div className="card p-5">
            <h3 className="text-sm font-semibold text-gray-400 mb-3 flex items-center gap-2">
              <TrendingDown className="w-4 h-4 text-success" />
              Cost Trend
            </h3>
            <CostTrendChart hours={chartHours} />
          </div>
        </div>
      </div>

      {/* ── Row 6: Recent Decisions + Recent Tools ── */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        <RecentDecisionsCard decisions={allDecisions} />

        <div className="card p-5">
          <h3 className="text-sm font-semibold text-gray-400 mb-4 flex items-center gap-2">
            <Terminal className="w-4 h-4 text-primary" />
            Recent Tool Executions
            {wsConnected && (
              <span className="status-dot status-dot-success ml-auto" />
            )}
          </h3>
          {toolExecutions.length === 0 ? (
            <p className="text-sm text-gray-500">
              Waiting for tool executions...
            </p>
          ) : (
            <div className="space-y-2 max-h-[280px] overflow-y-auto">
              {toolExecutions.slice(0, 10).map((t, i) => {
                const ts = String(t.status);
                const isOk = ts === "STATUS_SUCCESS" || ts === "3" || ts === "executed" || ts === "success";
                const isErr = ts === "STATUS_ERROR" || ts === "4" || ts === "error";
                return (
                  <div
                    key={t.id || i}
                    className="flex items-center justify-between text-sm py-1.5 border-b border-gray-800/60 last:border-0"
                  >
                    <span className="font-mono text-primary-light text-xs">
                      {t.tool_name}
                    </span>
                    <div className="flex items-center gap-2">
                      {isErr && <AlertTriangle className="w-3 h-3 text-error" />}
                      <span
                        className={`font-mono text-xs ${
                          isOk ? "text-success" : isErr ? "text-error" : "text-warning"
                        }`}
                      >
                        {t.duration_ms}ms
                      </span>
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
