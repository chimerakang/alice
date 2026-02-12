import { useEffect, useState } from "react";
import { api } from "@/lib/api";
import type { SecurityStats, SecurityEvent } from "@/types/alice";
import { useAppStore } from "@/stores/appStore";
import {
  Shield,
  ShieldAlert,
  Eye,
  AlertTriangle,
  Download,
  Search,
  Filter,
  Calendar,
} from "lucide-react";
import {
  PieChart,
  Pie,
  Cell,
  AreaChart,
  Area,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
} from "recharts";

interface SeverityDistribution {
  name: string;
  value: number;
  color: string;
}

interface EventTrend {
  timestamp: string;
  low: number;
  medium: number;
  high: number;
  critical: number;
}

interface PIIRecord {
  id: string;
  timestamp: string;
  type: string;
  location: string;
  masked_value: string;
}

export default function Security() {
  const [stats, setStats] = useState<SecurityStats | null>(null);
  const [events, setEvents] = useState<SecurityEvent[]>([]);
  const [loading, setLoading] = useState(true);
  const [searchTerm, setSearchTerm] = useState("");
  const [severityFilter, setSeverityFilter] = useState<string>("all");
  const [sortField, setSortField] = useState<keyof SecurityEvent>("timestamp");
  const [sortOrder, setSortOrder] = useState<"asc" | "desc">("desc");
  const { securityEvents } = useAppStore();

  useEffect(() => {
    const loadData = async () => {
      try {
        setLoading(true);
        const [statsData, eventsData] = await Promise.allSettled([
          api.getSecurityStats(),
          api.getSecurityEvents(100), // Get more events for analysis
        ]);

        if (statsData.status === "fulfilled") setStats(statsData.value);
        if (eventsData.status === "fulfilled") {
          setEvents(eventsData.value.events || []);
        }
      } catch (error) {
        console.error("Failed to load security data:", error);
      } finally {
        setLoading(false);
      }
    };

    loadData();
    const interval = setInterval(loadData, 30000);
    return () => clearInterval(interval);
  }, []);

  // Combine API events with WebSocket events for real-time data
  const allEvents = [...events, ...securityEvents];

  // Severity distribution for pie chart
  const getSeverityLevel = (severity: any): string => {
    const sev = String(severity).toLowerCase();
    if (sev.includes("low")) return "low";
    if (sev.includes("medium")) return "medium";
    if (sev.includes("high")) return "high";
    if (sev.includes("critical")) return "critical";
    return "low";
  };

  const severityDistribution: SeverityDistribution[] = [
    {
      name: "Low",
      value: allEvents.filter(e => getSeverityLevel(e.severity) === "low").length,
      color: "#10B981"
    },
    {
      name: "Medium",
      value: allEvents.filter(e => getSeverityLevel(e.severity) === "medium").length,
      color: "#F59E0B"
    },
    {
      name: "High",
      value: allEvents.filter(e => getSeverityLevel(e.severity) === "high").length,
      color: "#EF4444"
    },
    {
      name: "Critical",
      value: allEvents.filter(e => getSeverityLevel(e.severity) === "critical").length,
      color: "#DC2626"
    },
  ].filter(item => item.value > 0);

  // Event trends over time (simulated hourly data for demo)
  const eventTrends: EventTrend[] = Array.from({ length: 12 }, (_, i) => {
    const hour = 12 - i;
    const time = `${hour.toString().padStart(2, '0')}:00`;
    return {
      timestamp: time,
      low: Math.floor(Math.random() * 5),
      medium: Math.floor(Math.random() * 3),
      high: Math.floor(Math.random() * 2),
      critical: Math.random() > 0.8 ? 1 : 0,
    };
  }).reverse();

  // Sample PII detection records
  const piiRecords: PIIRecord[] = [
    {
      id: "pii_001",
      timestamp: "2026-02-12 13:30:15",
      type: "Email",
      location: "Chat message",
      masked_value: "user@*****.com"
    },
    {
      id: "pii_002",
      timestamp: "2026-02-12 13:25:42",
      type: "Credit Card",
      location: "File content",
      masked_value: "**** **** **** 1234"
    },
    {
      id: "pii_003",
      timestamp: "2026-02-12 13:20:18",
      type: "SSN",
      location: "Tool output",
      masked_value: "***-**-1234"
    },
  ];

  // Filter and sort events
  const filteredEvents = allEvents
    .filter(event => {
      const matchesSearch = searchTerm === "" ||
        event.event_type?.toLowerCase().includes(searchTerm.toLowerCase()) ||
        event.description?.toLowerCase().includes(searchTerm.toLowerCase());

      const matchesSeverity = severityFilter === "all" ||
        getSeverityLevel(event.severity) === severityFilter;

      return matchesSearch && matchesSeverity;
    })
    .sort((a, b) => {
      const aValue = a[sortField];
      const bValue = b[sortField];

      if (aValue < bValue) return sortOrder === "asc" ? -1 : 1;
      if (aValue > bValue) return sortOrder === "asc" ? 1 : -1;
      return 0;
    });

  const handleSort = (field: keyof SecurityEvent) => {
    if (sortField === field) {
      setSortOrder(sortOrder === "asc" ? "desc" : "asc");
    } else {
      setSortField(field);
      setSortOrder("desc");
    }
  };

  const handleExportAuditLog = () => {
    const csv = [
      ["Timestamp", "Type", "Severity", "Description", "Source IP"],
      ...filteredEvents.map(e => [
        e.timestamp,
        e.event_type,
        e.severity,
        e.description || "",
        e.ip || ""
      ])
    ].map(row => row.join(",")).join("\n");

    const blob = new Blob([csv], { type: "text/csv" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `security-audit-log-${new Date().toISOString().split("T")[0]}.csv`;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
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
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold text-white">Security & Privacy</h2>
        <button
          onClick={handleExportAuditLog}
          className="flex items-center gap-2 px-3 py-2 text-xs bg-gray-800 hover:bg-gray-700 border border-gray-700 rounded-lg transition-colors"
        >
          <Download className="w-3 h-3" />
          Export Audit Log
        </button>
      </div>

      {/* Summary Cards */}
      {stats && (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
          <div className="card p-5">
            <div className="flex items-center gap-2 mb-2">
              <Shield className="w-4 h-4 text-primary" />
              <span className="text-sm text-gray-400">Total Events</span>
            </div>
            <div className="text-2xl font-bold font-mono text-white">
              {stats.total_events}
            </div>
          </div>

          <div className="card p-5">
            <div className="flex items-center gap-2 mb-2">
              <ShieldAlert className="w-4 h-4 text-error" />
              <span className="text-sm text-gray-400">Blocked</span>
            </div>
            <div className="text-2xl font-bold font-mono text-white">
              {stats.blocked_attempts}
            </div>
          </div>

          <div className="card p-5">
            <div className="flex items-center gap-2 mb-2">
              <Eye className="w-4 h-4 text-warning" />
              <span className="text-sm text-gray-400">PII Detections</span>
            </div>
            <div className="text-2xl font-bold font-mono text-white">
              {stats.pii_detections}
            </div>
          </div>

          <div className="card p-5">
            <div className="flex items-center gap-2 mb-2">
              <AlertTriangle className="w-4 h-4 text-success" />
              <span className="text-sm text-gray-400">Threat Level</span>
            </div>
            <div className="text-2xl font-bold font-mono text-white capitalize">
              {stats.threat_level}
            </div>
          </div>
        </div>
      )}

      {/* Charts Grid */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        {/* Severity Distribution */}
        <div className="card p-6">
          <h3 className="text-sm font-semibold text-gray-300 mb-4 flex items-center gap-2">
            <Shield className="w-4 h-4" />
            Security Event Severity Distribution
          </h3>
          <ResponsiveContainer width="100%" height={200}>
            <PieChart>
              <Pie
                data={severityDistribution}
                cx="50%"
                cy="50%"
                innerRadius={40}
                outerRadius={80}
                paddingAngle={5}
                dataKey="value"
              >
                {severityDistribution.map((entry, index) => (
                  <Cell key={`cell-${index}`} fill={entry.color} />
                ))}
              </Pie>
              <Tooltip
                contentStyle={{
                  backgroundColor: '#1F2937',
                  border: '1px solid #374151',
                  borderRadius: '8px',
                  color: '#F9FAFB'
                }}
              />
            </PieChart>
          </ResponsiveContainer>
          <div className="flex justify-center mt-2">
            <div className="grid grid-cols-2 gap-2 text-xs">
              {severityDistribution.map((item, index) => (
                <div key={index} className="flex items-center gap-1">
                  <div
                    className="w-2 h-2 rounded-full"
                    style={{ backgroundColor: item.color }}
                  />
                  <span className="text-gray-400">{item.name}</span>
                  <span className="text-white font-mono">({item.value})</span>
                </div>
              ))}
            </div>
          </div>
        </div>

        {/* Event Trends */}
        <div className="card p-6">
          <h3 className="text-sm font-semibold text-gray-300 mb-4 flex items-center gap-2">
            <Calendar className="w-4 h-4" />
            Security Events Trend (Last 12 Hours)
          </h3>
          <ResponsiveContainer width="100%" height={200}>
            <AreaChart data={eventTrends}>
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
              />
              <Area
                type="monotone"
                dataKey="critical"
                stackId="1"
                stroke="#DC2626"
                fill="#DC2626"
                fillOpacity={0.8}
              />
              <Area
                type="monotone"
                dataKey="high"
                stackId="1"
                stroke="#EF4444"
                fill="#EF4444"
                fillOpacity={0.8}
              />
              <Area
                type="monotone"
                dataKey="medium"
                stackId="1"
                stroke="#F59E0B"
                fill="#F59E0B"
                fillOpacity={0.8}
              />
              <Area
                type="monotone"
                dataKey="low"
                stackId="1"
                stroke="#10B981"
                fill="#10B981"
                fillOpacity={0.8}
              />
            </AreaChart>
          </ResponsiveContainer>
        </div>
      </div>

      {/* PII Detection Records */}
      <div className="card p-6">
        <h3 className="text-sm font-semibold text-gray-300 mb-4 flex items-center gap-2">
          <Eye className="w-4 h-4" />
          Recent PII Detection Records
        </h3>
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-gray-700">
                <th className="text-left py-2 text-gray-400 font-medium">Timestamp</th>
                <th className="text-left py-2 text-gray-400 font-medium">PII Type</th>
                <th className="text-left py-2 text-gray-400 font-medium">Location</th>
                <th className="text-left py-2 text-gray-400 font-medium">Masked Value</th>
              </tr>
            </thead>
            <tbody>
              {piiRecords.map((record) => (
                <tr key={record.id} className="border-b border-gray-800/50">
                  <td className="py-2 text-gray-300 font-mono">{record.timestamp}</td>
                  <td className="py-2">
                    <span className={`px-2 py-1 rounded-full text-xs ${
                      record.type === "Credit Card" ? "bg-red-500/20 text-red-300" :
                      record.type === "SSN" ? "bg-orange-500/20 text-orange-300" :
                      "bg-blue-500/20 text-blue-300"
                    }`}>
                      {record.type}
                    </span>
                  </td>
                  <td className="py-2 text-gray-300">{record.location}</td>
                  <td className="py-2 text-gray-400 font-mono">{record.masked_value}</td>
                </tr>
              ))}
              {piiRecords.length === 0 && (
                <tr>
                  <td colSpan={4} className="py-4 text-center text-gray-500">
                    No PII detections recorded.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </div>

      {/* Security Events Table */}
      <div className="card p-6">
        <div className="flex items-center justify-between mb-4">
          <h3 className="text-sm font-semibold text-gray-300 flex items-center gap-2">
            <ShieldAlert className="w-4 h-4" />
            Security Events ({filteredEvents.length})
          </h3>

          <div className="flex items-center gap-3">
            {/* Search */}
            <div className="relative">
              <Search className="w-3 h-3 absolute left-3 top-1/2 transform -translate-y-1/2 text-gray-500" />
              <input
                type="text"
                placeholder="Search events..."
                value={searchTerm}
                onChange={(e) => setSearchTerm(e.target.value)}
                className="pl-8 pr-3 py-1.5 text-xs bg-gray-800 border border-gray-700 rounded-lg text-white focus:outline-none focus:border-primary"
              />
            </div>

            {/* Severity Filter */}
            <div className="relative">
              <Filter className="w-3 h-3 absolute left-3 top-1/2 transform -translate-y-1/2 text-gray-500" />
              <select
                value={severityFilter}
                onChange={(e) => setSeverityFilter(e.target.value)}
                className="pl-8 pr-8 py-1.5 text-xs bg-gray-800 border border-gray-700 rounded-lg text-white focus:outline-none focus:border-primary appearance-none"
              >
                <option value="all">All Severities</option>
                <option value="low">Low</option>
                <option value="medium">Medium</option>
                <option value="high">High</option>
                <option value="critical">Critical</option>
              </select>
            </div>
          </div>
        </div>

        <div className="overflow-x-auto max-h-96 overflow-y-auto">
          <table className="w-full text-sm">
            <thead className="sticky top-0 bg-gray-900">
              <tr className="border-b border-gray-700">
                <th
                  className="text-left py-2 text-gray-400 font-medium cursor-pointer hover:text-gray-300"
                  onClick={() => handleSort("timestamp")}
                >
                  Timestamp {sortField === "timestamp" && (sortOrder === "asc" ? "↑" : "↓")}
                </th>
                <th
                  className="text-left py-2 text-gray-400 font-medium cursor-pointer hover:text-gray-300"
                  onClick={() => handleSort("event_type")}
                >
                  Type {sortField === "event_type" && (sortOrder === "asc" ? "↑" : "↓")}
                </th>
                <th
                  className="text-left py-2 text-gray-400 font-medium cursor-pointer hover:text-gray-300"
                  onClick={() => handleSort("severity")}
                >
                  Severity {sortField === "severity" && (sortOrder === "asc" ? "↑" : "↓")}
                </th>
                <th className="text-left py-2 text-gray-400 font-medium">Description</th>
                <th className="text-left py-2 text-gray-400 font-medium">Source</th>
              </tr>
            </thead>
            <tbody>
              {filteredEvents.map((event, i) => (
                <tr key={event.event_id || i} className="border-b border-gray-800/50 hover:bg-gray-800/30">
                  <td className="py-2 text-gray-300 font-mono">
                    {event.timestamp ? new Date(event.timestamp).toLocaleString() : "N/A"}
                  </td>
                  <td className="py-2 text-gray-300">{event.event_type || "Unknown"}</td>
                  <td className="py-2">
                    <span className={`px-2 py-1 rounded-full text-xs ${
                      getSeverityLevel(event.severity) === "critical" ? "bg-red-500/20 text-red-300" :
                      getSeverityLevel(event.severity) === "high" ? "bg-red-400/20 text-red-300" :
                      getSeverityLevel(event.severity) === "medium" ? "bg-yellow-500/20 text-yellow-300" :
                      "bg-green-500/20 text-green-300"
                    }`}>
                      {getSeverityLevel(event.severity).toUpperCase()}
                    </span>
                  </td>
                  <td className="py-2 text-gray-300 max-w-xs truncate">
                    {event.description || "No description"}
                  </td>
                  <td className="py-2 text-gray-400 font-mono">
                    {event.ip || "Unknown"}
                  </td>
                </tr>
              ))}
              {filteredEvents.length === 0 && (
                <tr>
                  <td colSpan={5} className="py-4 text-center text-gray-500">
                    No security events found.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  );
}