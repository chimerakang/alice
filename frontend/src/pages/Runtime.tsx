import { useEffect, useMemo, useState } from "react";
import { api } from "@/lib/api";
import type { RuntimeEventRecord } from "@/types/alice";
import StatusBadge from "@/components/StatusBadge";
import {
  Activity,
  AlertTriangle,
  CheckCircle2,
  Loader2,
  RefreshCw,
  RotateCcw,
  Search,
  XCircle,
} from "lucide-react";

const EVENT_TYPE_OPTIONS = [
  { value: "RecoveryDecision", label: "Recovery" },
  { value: "IssueQualityGate", label: "Quality Gate" },
  { value: "", label: "All" },
];

function formatTimestamp(value?: string): string {
  if (!value) return "-";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString("zh-TW", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}

function payloadString(event: RuntimeEventRecord, key: string): string {
  const value = event.payload?.[key];
  if (value === undefined || value === null) return "";
  return String(value);
}

function payloadNumber(event: RuntimeEventRecord, key: string): number {
  const value = event.payload?.[key];
  if (typeof value === "number") return value;
  if (typeof value === "string") {
    const parsed = Number(value);
    return Number.isFinite(parsed) ? parsed : 0;
  }
  return 0;
}

function actionVariant(action: string): "success" | "warning" | "error" | "info" | "neutral" {
  switch (action) {
    case "retry":
      return "warning";
    case "fallback":
    case "allow":
      return "info";
    case "cancel":
    case "fail":
      return "error";
    case "none":
    case "skip":
      return "neutral";
    case "needs_clarification":
      return "warning";
    default:
      return "success";
  }
}

function actionIcon(action: string) {
  switch (action) {
    case "retry":
      return RotateCcw;
    case "fallback":
    case "allow":
      return RefreshCw;
    case "cancel":
    case "fail":
      return XCircle;
    case "none":
    case "needs_clarification":
      return AlertTriangle;
    case "skip":
      return CheckCircle2;
    default:
      return CheckCircle2;
  }
}

function RuntimeEventRow({ event }: { event: RuntimeEventRecord }) {
  const action = payloadString(event, "action") || "-";
  const mode = payloadString(event, "mode") || event.type;
  const reason = payloadString(event, "reason") || "-";
  const attempt = payloadNumber(event, "attempt");
  const maxAttempts = payloadNumber(event, "max_attempts");
  const nextAttempt = payloadNumber(event, "next_attempt");
  const unchecked = payloadNumber(event, "unchecked_count");
  const checked = payloadNumber(event, "checked_count");
  const checklistTotal = payloadNumber(event, "checklist_total");
  const commentCount = payloadNumber(event, "comment_count");
  const completionDone = payloadNumber(event, "completion_done");
  const completionTotal = payloadNumber(event, "completion_total");
  const Icon = actionIcon(action);

  return (
    <div className="border-b border-gray-800/70 px-4 py-3 hover:bg-white/[0.03] transition-colors">
      <div className="grid grid-cols-[9rem_1fr_auto] gap-4 items-start">
        <div className="text-xs font-mono text-gray-500">
          {formatTimestamp(event.timestamp)}
        </div>
        <div className="min-w-0 space-y-1">
          <div className="flex items-center gap-2 flex-wrap">
            <StatusBadge variant={actionVariant(action)} size="sm" dot>
              <span className="inline-flex items-center gap-1">
                <Icon className="w-3 h-3" />
                {action}
              </span>
            </StatusBadge>
            <span className="text-sm font-medium text-white">{mode}</span>
            <span className="text-xs text-gray-500">{reason}</span>
          </div>
          <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-gray-500">
            {event.task_id && <span>task {event.task_id.slice(0, 8)}</span>}
            {event.issue ? <span>issue #{event.issue}</span> : null}
            {event.chat_id ? <span>chat {event.chat_id}</span> : null}
            {maxAttempts > 0 && (
              <span>
                attempt {attempt}/{maxAttempts}
                {nextAttempt > 0 ? ` -> ${nextAttempt}` : ""}
              </span>
            )}
            {checklistTotal > 0 && (
              <span>
                checklist {checked}/{checklistTotal}
                {unchecked > 0 ? `, ${unchecked} open` : ""}
              </span>
            )}
            {completionTotal > 0 && (
              <span>
                completed {completionDone}/{completionTotal}
              </span>
            )}
            {commentCount > 0 && <span>{commentCount} comments</span>}
          </div>
        </div>
        <div className="text-[11px] font-mono text-gray-600">{event.type}</div>
      </div>
    </div>
  );
}

export default function Runtime() {
  const [events, setEvents] = useState<RuntimeEventRecord[]>([]);
  const [eventType, setEventType] = useState("RecoveryDecision");
  const [limit, setLimit] = useState(50);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const loadEvents = () => {
    setLoading(true);
    setError(null);
    api
      .getRuntimeEvents({ limit, type: eventType || undefined })
      .then((res) => setEvents(res.events || []))
      .catch((err: Error) => setError(err.message))
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    loadEvents();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [eventType, limit]);

  const counts = useMemo(() => {
    return events.reduce<Record<string, number>>((acc, event) => {
      const action = payloadString(event, "action") || "unknown";
      acc[action] = (acc[action] || 0) + 1;
      return acc;
    }, {});
  }, [events]);

  return (
    <div className="space-y-6">
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold text-white flex items-center gap-2">
            <Activity className="w-6 h-6 text-primary" />
            Runtime Events
          </h1>
          <p className="text-sm text-gray-500 mt-1">
            Persisted runtime decisions for recovery and Hermes issue gates.
          </p>
        </div>
        <button
          onClick={loadEvents}
          className="btn btn-secondary inline-flex items-center gap-2 self-start sm:self-auto"
          disabled={loading}
        >
          {loading ? <Loader2 className="w-4 h-4 animate-spin" /> : <RefreshCw className="w-4 h-4" />}
          Refresh
        </button>
      </div>

      <div className="grid grid-cols-2 md:grid-cols-5 gap-3">
        {["allow", "skip", "needs_clarification", "retry", "fallback"].map((action) => (
          <div key={action} className="card p-4">
            <div className="text-xs text-gray-500 uppercase">{action}</div>
            <div className="text-2xl font-bold font-mono text-white mt-1">
              {counts[action] || 0}
            </div>
          </div>
        ))}
      </div>

      <div className="card overflow-hidden">
        <div className="p-4 border-b border-gray-800/70 flex flex-col md:flex-row md:items-center gap-3">
          <div className="flex items-center gap-2 text-sm text-gray-400">
            <Search className="w-4 h-4" />
            Filter
          </div>
          <div className="flex flex-wrap gap-2">
            {EVENT_TYPE_OPTIONS.map((option) => (
              <button
                key={option.label}
                onClick={() => setEventType(option.value)}
                className={`px-3 py-1.5 rounded-md text-sm border transition-colors ${
                  eventType === option.value
                    ? "bg-primary/15 text-primary border-primary/30"
                    : "text-gray-400 border-gray-700 hover:text-gray-200 hover:border-gray-600"
                }`}
              >
                {option.label}
              </button>
            ))}
            {[25, 50, 100].map((value) => (
              <button
                key={value}
                onClick={() => setLimit(value)}
                className={`px-3 py-1.5 rounded-md text-sm border transition-colors ${
                  limit === value
                    ? "bg-white/10 text-white border-gray-500"
                    : "text-gray-500 border-gray-700 hover:text-gray-300"
                }`}
              >
                {value}
              </button>
            ))}
          </div>
        </div>

        {loading ? (
          <div className="h-52 flex items-center justify-center text-gray-500">
            <Loader2 className="w-5 h-5 animate-spin mr-2" />
            Loading runtime events
          </div>
        ) : error ? (
          <div className="h-52 flex items-center justify-center text-error text-sm">
            {error}
          </div>
        ) : events.length === 0 ? (
          <div className="h-52 flex items-center justify-center text-gray-600 text-sm">
            No runtime events found
          </div>
        ) : (
          <div>
            {events.map((event, index) => (
              <RuntimeEventRow
                key={`${event.timestamp}-${event.type}-${event.task_id || "event"}-${index}`}
                event={event}
              />
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
