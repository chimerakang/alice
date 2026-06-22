import { useEffect, useState, useCallback, useMemo } from "react";
import { useTranslation } from "react-i18next";
import i18n from "@/i18n";
import { api } from "@/lib/api";
import type { SecurityStats, SecurityEvent } from "@/types/alice";
import { useAppStore } from "@/stores/appStore";
import DateRangeFilter from "@/components/DateRangeFilter";
import type { DateRange } from "@/components/DateRangeFilter";
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
  chat_id?: number;
  user_id?: number;
  message_type?: string;
  match_count?: number;
  redacted_snippet?: string;
  severity?: string;
  event_id?: string; // Reference to original SecurityEvent for modal
}

function formatTimestamp(value?: string): string {
  if (!value) return "-";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  const locale = i18n.language === "zh-TW" ? "zh-TW" : "en-US";
  return date.toLocaleString(locale);
}

export default function Security() {
  const { t } = useTranslation();
  const locale = i18n.language === "zh-TW" ? "zh-TW" : "en-US";
  const [stats, setStats] = useState<SecurityStats | null>(null);
  const [events, setEvents] = useState<SecurityEvent[]>([]);
  const [loading, setLoading] = useState(true);
  const [searchTerm, setSearchTerm] = useState("");
  const [severityFilter, setSeverityFilter] = useState<string>("all");
  const [sortField, setSortField] = useState<keyof SecurityEvent>("timestamp");
  const [sortOrder, setSortOrder] = useState<"asc" | "desc">("desc");
  const { securityEvents } = useAppStore();
  const [dateRange, setDateRange] = useState<DateRange>({});
  const [selectedEvent, setSelectedEvent] = useState<SecurityEvent | null>(null);

  const handleDateRangeChange = useCallback((range: DateRange) => {
    setDateRange(range);
  }, []);

  useEffect(() => {
    const loadData = async () => {
      try {
        // Get time range to use for API calls (includes default logic)
        const timeRange = getTimeRangeInfo();
        const startTime = timeRange.startTime?.toISOString();
        const endTime = timeRange.endTime?.toISOString();

        const [statsData, eventsData] = await Promise.allSettled([
          api.getSecurityStats({
            startTime,
            endTime,
          }),
          api.getSecurityEvents({
            limit: 200,
            startTime,
            endTime,
          }),
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
  }, [dateRange]);

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
      name: t("security.severity.low"),
      value: allEvents.filter(e => getSeverityLevel(e.severity) === "low").length,
      color: "#10B981"
    },
    {
      name: t("security.severity.medium"),
      value: allEvents.filter(e => getSeverityLevel(e.severity) === "medium").length,
      color: "#F59E0B"
    },
    {
      name: t("security.severity.high"),
      value: allEvents.filter(e => getSeverityLevel(e.severity) === "high").length,
      color: "#EF4444"
    },
    {
      name: t("security.severity.critical"),
      value: allEvents.filter(e => getSeverityLevel(e.severity) === "critical").length,
      color: "#DC2626"
    },
  ].filter(item => item.value > 0);

  // Determine time range and bucket granularity
  const getTimeRangeInfo = () => {
    const locale = i18n.language === "zh-TW" ? "zh-TW" : "en-US";
    const now = new Date();
    let startTime = dateRange.startTime ? new Date(dateRange.startTime) : undefined;
    let endTime = dateRange.endTime ? new Date(dateRange.endTime) : undefined;
    let label = t("security.time_ranges.last_12_hours");
    let bucketMs = 60 * 60 * 1000; // 1 hour default

    if (!startTime && !endTime) {
      // Default: last 12 hours
      startTime = new Date(now.getTime() - 12 * 60 * 60 * 1000);
      endTime = now;
      label = t("security.time_ranges.last_12_hours");
      bucketMs = 60 * 60 * 1000; // 1 hour
    } else if (startTime && endTime) {
      const spanMs = endTime.getTime() - startTime.getTime();
      const spanHours = spanMs / (60 * 60 * 1000);
      const spanDays = spanMs / (24 * 60 * 60 * 1000);

      if (spanHours <= 1) {
        label = t("security.time_ranges.last_1_hour");
        bucketMs = 5 * 60 * 1000; // 5 minutes
      } else if (spanHours <= 6) {
        label = t("security.time_ranges.last_6_hours");
        bucketMs = 30 * 60 * 1000; // 30 minutes
      } else if (spanHours <= 24) {
        label = t("security.time_ranges.last_24_hours");
        bucketMs = 60 * 60 * 1000; // 1 hour
      } else if (spanDays <= 7) {
        label = t("security.time_ranges.last_7_days");
        bucketMs = 4 * 60 * 60 * 1000; // 4 hours
      } else if (spanDays <= 30) {
        label = t("security.time_ranges.last_30_days");
        bucketMs = 24 * 60 * 60 * 1000; // 1 day
      } else {
        // Custom range
        const startStr = startTime.toLocaleDateString(locale);
        const endStr = endTime.toLocaleDateString(locale);
        label = `${startStr} - ${endStr}`;
        bucketMs = 24 * 60 * 60 * 1000; // 1 day
      }
    }

    return { startTime, endTime, label, bucketMs };
  };

  const { endTime: rangeEnd, label: timeRangeLabel } = getTimeRangeInfo();

  // Event trends over time (from real data)
  const eventTrends: EventTrend[] = useMemo(() => {
    if (allEvents.length === 0) return [];

    const buckets: Record<number, EventTrend> = {};
    const { startTime, bucketMs: bucketSize } = getTimeRangeInfo();

    // Initialize buckets based on time range
    if (startTime && rangeEnd) {
      let currentTime = new Date(startTime.getTime());
      const endTime = new Date(rangeEnd.getTime());

      while (currentTime.getTime() <= endTime.getTime()) {
        const bucketKey = Math.floor(currentTime.getTime() / bucketSize) * bucketSize;

        if (!buckets[bucketKey]) {
          const d = new Date(bucketKey);
          let timestamp = "";

          if (bucketSize === 5 * 60 * 1000) {
            // 5 min: HH:MM
            timestamp = `${d.getHours().toString().padStart(2, '0')}:${d.getMinutes().toString().padStart(2, '0')}`;
          } else if (bucketSize === 30 * 60 * 1000) {
            // 30 min: HH:MM
            timestamp = `${d.getHours().toString().padStart(2, '0')}:${d.getMinutes().toString().padStart(2, '0')}`;
          } else if (bucketSize === 60 * 60 * 1000) {
            // 1 hour: M/D HH:00
            timestamp = `${d.getMonth() + 1}/${d.getDate()} ${d.getHours().toString().padStart(2, '0')}:00`;
          } else if (bucketSize === 4 * 60 * 60 * 1000) {
            // 4 hours: M/D HH:00
            timestamp = `${d.getMonth() + 1}/${d.getDate()} ${d.getHours().toString().padStart(2, '0')}:00`;
          } else {
            // 1 day: M/D
            timestamp = `${d.getMonth() + 1}/${d.getDate()}`;
          }

          buckets[bucketKey] = { timestamp, low: 0, medium: 0, high: 0, critical: 0 };
        }

        currentTime = new Date(currentTime.getTime() + bucketSize);
      }
    }

    allEvents.forEach(event => {
      const d = new Date(event.timestamp);
      if (isNaN(d.getTime())) return;

      const bucketKey = Math.floor(d.getTime() / bucketSize) * bucketSize;
      if (buckets[bucketKey]) {
        const sev = getSeverityLevel(event.severity);
        buckets[bucketKey][sev as keyof Omit<EventTrend, 'timestamp'>]++;
      }
    });

    return Object.values(buckets);
  }, [allEvents, dateRange, t, locale]);

  // Extract real PII detection records from security events
  const piiRecords: PIIRecord[] = useMemo(() => {
    return allEvents
      .filter(e => e.event_type?.includes("pii"))
      .map((e, i) => {
        // Extract PII type from details first (most reliable), fallback to description
        const piiTypeFromDetails = e.details?.pii_type as string | undefined;
        const typeMatch = e.description?.match(/PII detected(?:\s+in\s+\w+\s+\w+)?:\s*(.+)/);
        const piiType = piiTypeFromDetails || (typeMatch ? typeMatch[1] : e.event_type || t("security.table.unknown"));

        // Derive location from message_type and source_type
        let location = t("security.source.system");
        const messageType = e.details?.message_type as string | undefined;
        const sourceType = e.details?.source_type as string | undefined;

        if (messageType === "text" && sourceType === "telegram") {
          location = t("security.location.telegram_message");
        } else if (messageType === "photo") {
          location = t("security.location.telegram_photo");
        } else if (messageType === "voice") {
          location = t("security.location.voice_message");
        } else if (messageType === "batch") {
          location = t("security.location.batch_photos");
        } else if (sourceType === "agent") {
          location = t("security.location.agent_logs");
        }

        // Get match count and snippet from details
        const matchCount = e.details?.matches as number | undefined;
        const redactedSnippet = e.details?.redacted_snippet as string | undefined;
        const chatId = e.details?.chat_id as number | undefined;
        const userId = e.details?.user_id as number | undefined;

        return {
          id: e.event_id || `pii_${i}`,
          event_id: e.event_id, // Reference to original event for modal
          timestamp: formatTimestamp(e.timestamp),
          type: piiType,
          location,
          masked_value: redactedSnippet || t("security.table.redacted"),
          chat_id: chatId,
          user_id: userId,
          message_type: messageType,
          match_count: matchCount,
          redacted_snippet: redactedSnippet,
          severity: e.severity,
        };
      })
      .slice(0, 20); // Show last 20
  }, [allEvents, t, locale]);

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
      const aValue = a[sortField] ?? "";
      const bValue = b[sortField] ?? "";

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
      [t("security.csv.timestamp"), t("security.csv.type"), t("security.csv.severity"), t("security.csv.description"), t("security.csv.source_ip")],
      ...filteredEvents.map(e => [
        e.timestamp,
        e.event_type,
        e.severity,
        e.description || "",
        e.ip || (e.event_type?.includes("telegram") ? t("security.source.telegram") : t("security.source.system"))
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
        <h2 className="text-lg font-semibold text-white">{t("security.title")}</h2>
        <button
          onClick={handleExportAuditLog}
          className="flex items-center gap-2 px-3 py-2 text-xs bg-gray-800 hover:bg-gray-700 border border-gray-700 rounded-lg transition-colors"
        >
          <Download className="w-3 h-3" />
          {t("security.actions.export_audit_log")}
        </button>
      </div>

      {/* Date Range Filter */}
      <DateRangeFilter onChange={handleDateRangeChange} />

      {/* Summary Cards */}
      {stats && (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
          <div className="card p-5">
            <div className="flex items-center gap-2 mb-2">
              <Shield className="w-4 h-4 text-primary" />
              <span className="text-sm text-gray-400">{t("security.summary.total_events")}</span>
            </div>
            <div className="text-2xl font-bold font-mono text-white">
              {stats.total_events}
            </div>
          </div>

          <div className="card p-5">
            <div className="flex items-center gap-2 mb-2">
              <ShieldAlert className="w-4 h-4 text-error" />
              <span className="text-sm text-gray-400">{t("security.summary.blocked")}</span>
            </div>
            <div className="text-2xl font-bold font-mono text-white">
              {stats.blocked_attempts}
            </div>
          </div>

          <div className="card p-5">
            <div className="flex items-center gap-2 mb-2">
              <Eye className="w-4 h-4 text-warning" />
              <span className="text-sm text-gray-400">{t("security.summary.pii_detections")}</span>
            </div>
            <div className="text-2xl font-bold font-mono text-white">
              {stats.pii_detections}
            </div>
          </div>

          <div className="card p-5">
            <div className="flex items-center gap-2 mb-2">
              <AlertTriangle className="w-4 h-4 text-success" />
              <span className="text-sm text-gray-400">{t("security.summary.threat_level")}</span>
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
            {t("security.charts.severity_distribution")}
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
            {t("security.charts.events_trend", { range: timeRangeLabel })}
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
          {t("security.charts.recent_pii_records")}
        </h3>
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-gray-700">
                <th className="text-left py-2 text-gray-400 font-medium">{t("security.table.timestamp")}</th>
                <th className="text-left py-2 text-gray-400 font-medium">{t("security.table.pii_type")}</th>
                <th className="text-left py-2 text-gray-400 font-medium">{t("security.table.location")}</th>
                <th className="text-left py-2 text-gray-400 font-medium">{t("security.table.source")}</th>
                <th className="text-left py-2 text-gray-400 font-medium">{t("security.table.matches")}</th>
                <th className="text-left py-2 text-gray-400 font-medium">{t("security.table.preview")}</th>
              </tr>
            </thead>
            <tbody>
              {piiRecords.map((record) => (
                <tr
                  key={record.id}
                  className="border-b border-gray-800/50 hover:bg-gray-800/20 cursor-pointer transition-colors"
                  onClick={() => {
                    // Find the original SecurityEvent by event_id or id from allEvents (includes WebSocket events)
                    const recordId = record.event_id;
                    if (recordId) {
                      const sourceEvent = allEvents.find(e => (e.event_id === recordId) || (e.id === recordId));
                      if (sourceEvent) {
                        setSelectedEvent(sourceEvent);
                      }
                    }
                  }}
                >
                  <td className="py-2 text-gray-300 font-mono text-xs">{record.timestamp}</td>
                  <td className="py-2">
                    <span className={`px-2 py-1 rounded-full text-xs font-medium ${
                      record.type === "Credit Card" ? "bg-red-500/20 text-red-300" :
                      record.type === "SSN" ? "bg-orange-500/20 text-orange-300" :
                      record.type === "Email" ? "bg-purple-500/20 text-purple-300" :
                      "bg-blue-500/20 text-blue-300"
                    }`}>
                      {record.type}
                    </span>
                  </td>
                  <td className="py-2 text-gray-300 text-xs">{record.location}</td>
                  <td className="py-2 text-gray-400 text-xs">
                    {record.chat_id ? t("security.table.chat", { chat: record.chat_id }) : t("security.table.unknown")}
                  </td>
                  <td className="py-2 text-gray-300 text-xs font-mono">
                    {record.match_count ? t("security.table.matches_found", { count: record.match_count }) : "-"}
                  </td>
                  <td className="py-2 text-gray-400 text-xs font-mono max-w-xs truncate">
                    {record.redacted_snippet ? `"${record.redacted_snippet.substring(0, 50)}..."` : t("security.table.redacted")}
                  </td>
                </tr>
              ))}
              {piiRecords.length === 0 && (
                <tr>
                  <td colSpan={6} className="py-4 text-center text-gray-500">
                    {t("security.empty.no_pii")}
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
            {t("security.table.events", { count: filteredEvents.length })}
          </h3>

          <div className="flex items-center gap-3">
            {/* Search */}
            <div className="relative">
              <Search className="w-3 h-3 absolute left-3 top-1/2 transform -translate-y-1/2 text-gray-500" />
              <input
                type="text"
                placeholder={t("security.search_placeholder")}
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
                <option value="all">{t("security.severity.all")}</option>
                <option value="low">{t("security.severity.low")}</option>
                <option value="medium">{t("security.severity.medium")}</option>
                <option value="high">{t("security.severity.high")}</option>
                <option value="critical">{t("security.severity.critical")}</option>
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
                  {t("security.table.timestamp")} {sortField === "timestamp" && (sortOrder === "asc" ? "↑" : "↓")}
                </th>
                <th
                  className="text-left py-2 text-gray-400 font-medium cursor-pointer hover:text-gray-300"
                  onClick={() => handleSort("event_type")}
              >
                  {t("security.table.type")} {sortField === "event_type" && (sortOrder === "asc" ? "↑" : "↓")}
                </th>
                <th
                  className="text-left py-2 text-gray-400 font-medium cursor-pointer hover:text-gray-300"
                  onClick={() => handleSort("severity")}
              >
                  {t("security.table.severity")} {sortField === "severity" && (sortOrder === "asc" ? "↑" : "↓")}
                </th>
                <th className="text-left py-2 text-gray-400 font-medium">{t("security.table.description")}</th>
                <th className="text-left py-2 text-gray-400 font-medium">{t("security.table.source")}</th>
              </tr>
            </thead>
            <tbody>
              {filteredEvents.map((event, i) => (
                <tr
                  key={event.event_id || i}
                  className="border-b border-gray-800/50 hover:bg-gray-800/30 cursor-pointer transition-colors"
                  onClick={() => setSelectedEvent(event)}
                >
                  <td className="py-2 text-gray-300 font-mono">
                    {formatTimestamp(event.timestamp)}
                  </td>
                  <td className="py-2 text-gray-300">{event.event_type || t("security.table.unknown")}</td>
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
                    {event.description || t("security.table.no_description")}
                  </td>
                  <td className="py-2 text-gray-400 font-mono">
                    {event.ip || (
                      event.event_type?.includes("telegram") ? t("security.source.telegram") :
                      event.event_type?.includes("rate_limit") ? t("security.source.http") :
                      event.event_type?.includes("blocked") ? t("security.source.http") :
                      event.event_type?.includes("pii") ? t("security.source.telegram") :
                      t("security.source.system")
                    )}
                  </td>
                </tr>
              ))}
              {filteredEvents.length === 0 && (
                <tr>
                  <td colSpan={5} className="py-4 text-center text-gray-500">
                    {t("security.empty.no_events")}
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </div>

      {/* Event Details Modal */}
      {selectedEvent && (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center p-4 z-50">
          <div className="bg-gray-900 border border-gray-700 rounded-lg max-w-2xl w-full max-h-96 overflow-y-auto">
            <div className="sticky top-0 bg-gray-900 border-b border-gray-700 p-4 flex items-center justify-between">
              <h3 className="text-lg font-semibold text-white">{t("security.modal.title")}</h3>
              <button
                onClick={() => setSelectedEvent(null)}
                className="text-gray-400 hover:text-white text-xl"
              >
                ✕
              </button>
            </div>

            <div className="p-6 space-y-4">
              <div>
                <label className="text-xs font-semibold text-gray-400 uppercase">{t("security.modal.timestamp")}</label>
                <p className="text-white font-mono text-sm mt-1">
                  {formatTimestamp(selectedEvent.timestamp)}
                </p>
              </div>

              <div>
                <label className="text-xs font-semibold text-gray-400 uppercase">{t("security.modal.event_type")}</label>
                <p className="text-white font-mono text-sm mt-1">{selectedEvent.event_type || t("security.table.unknown")}</p>
              </div>

              <div>
                <label className="text-xs font-semibold text-gray-400 uppercase">{t("security.modal.severity")}</label>
                <p className="text-white font-mono text-sm mt-1">
                  {getSeverityLevel(selectedEvent.severity).toUpperCase()}
                </p>
              </div>

              <div>
                <label className="text-xs font-semibold text-gray-400 uppercase">{t("security.modal.description")}</label>
                <p className="text-gray-300 text-sm mt-1">{selectedEvent.description || t("security.table.no_description")}</p>
              </div>

              {selectedEvent.details && (
                <>
                  {(selectedEvent.details.chat_id || selectedEvent.details.user_id) && (
                    <div className="bg-gray-800/50 border border-gray-700 rounded p-3">
                      <label className="text-xs font-semibold text-gray-400 uppercase">{t("security.modal.affected_chat_user")}</label>
                      <div className="text-white text-sm mt-2 space-y-1 font-mono">
                        {selectedEvent.details.chat_id && (
                          <p>{t("security.modal.chat_id")}: <span className="text-cyan-400">{selectedEvent.details.chat_id}</span></p>
                        )}
                        {selectedEvent.details.user_id && (
                          <p>{t("security.modal.user_id")}: <span className="text-cyan-400">{selectedEvent.details.user_id}</span></p>
                        )}
                      </div>
                    </div>
                  )}

                  {selectedEvent.details.message_type && (
                    <div>
                      <label className="text-xs font-semibold text-gray-400 uppercase">{t("security.modal.message_type")}</label>
                      <p className="text-white text-sm mt-1">{selectedEvent.details.message_type}</p>
                    </div>
                  )}

                  {selectedEvent.details.project_path && (
                    <div>
                      <label className="text-xs font-semibold text-gray-400 uppercase">{t("security.modal.project")}</label>
                      <p className="text-white text-sm mt-1 font-mono break-all">{selectedEvent.details.project_path}</p>
                    </div>
                  )}

                  {selectedEvent.details.message_id && (
                    <div>
                      <label className="text-xs font-semibold text-gray-400 uppercase">{t("security.modal.message_info")}</label>
                      <p className="text-white text-sm mt-1 font-mono">{t("security.modal.message_id")}: <span className="text-cyan-400">{selectedEvent.details.message_id}</span></p>
                    </div>
                  )}

                  {selectedEvent.details.redacted_snippet && (
                    <div className="bg-gray-800/50 border border-gray-700 rounded p-3">
                      <label className="text-xs font-semibold text-gray-400 uppercase block mb-2">
                        {t("security.modal.redacted_preview_title")}
                      </label>
                      <p className="text-gray-300 text-sm font-mono whitespace-pre-wrap break-words">
                        {selectedEvent.details.redacted_snippet}
                      </p>
                      <p className="text-gray-500 text-xs mt-2">
                        {t("security.modal.redacted_preview_hint")}
                      </p>
                    </div>
                  )}

                  {selectedEvent.details.matches && (
                    <div>
                      <label className="text-xs font-semibold text-gray-400 uppercase">{t("security.modal.pii_matches_found")}</label>
                      <p className="text-orange-400 font-mono text-sm mt-1">{t("security.modal.instances", { count: selectedEvent.details.matches })}</p>
                    </div>
                  )}

                  {selectedEvent.details.pattern && (
                    <div>
                      <label className="text-xs font-semibold text-gray-400 uppercase">{t("security.modal.pii_pattern_detected")}</label>
                      <p className="text-white text-sm mt-1">{selectedEvent.details.pattern}</p>
                    </div>
                  )}
                </>
              )}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
