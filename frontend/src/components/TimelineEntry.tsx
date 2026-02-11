import { useState } from "react";
import { clsx } from "clsx";
import {
  ChevronRight,
  Terminal,
  CheckCircle2,
  XCircle,
  Clock,
  Coins,
  MessageSquare,
} from "lucide-react";
import StatusBadge from "./StatusBadge";
import type { DecisionLog, ToolExecution } from "@/types/alice";

function formatTimestamp(ts: string | { seconds: number; nanos?: number }): string {
  let date: Date;
  if (typeof ts === "string") {
    date = new Date(ts);
  } else if (ts && typeof ts === "object" && "seconds" in ts) {
    date = new Date(ts.seconds * 1000);
  } else {
    return "—";
  }
  return date.toLocaleTimeString("zh-TW", {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}

function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`;
  return `${(ms / 60000).toFixed(1)}m`;
}

function truncate(text: string, max: number): string {
  if (text.length <= max) return text;
  return text.slice(0, max) + "...";
}

function ToolCallItem({ tool }: { tool: ToolExecution }) {
  const [expanded, setExpanded] = useState(false);
  const s = String(tool.status);
  const isSuccess = s === "STATUS_SUCCESS" || s === "2";
  const isError = s === "STATUS_ERROR" || s === "4";

  return (
    <div className="border border-gray-800/60 rounded-md">
      <button
        onClick={() => setExpanded(!expanded)}
        className="w-full flex items-center gap-2 px-3 py-2 text-left hover:bg-white/3 transition-colors text-xs"
      >
        <ChevronRight
          className={clsx(
            "w-3 h-3 text-gray-600 transition-transform shrink-0",
            expanded && "rotate-90"
          )}
        />
        <Terminal className="w-3 h-3 text-gray-500 shrink-0" />
        <span className="font-mono text-primary-light flex-1 truncate">
          {tool.tool_name}
        </span>
        {isSuccess && <CheckCircle2 className="w-3 h-3 text-success shrink-0" />}
        {isError && <XCircle className="w-3 h-3 text-error shrink-0" />}
        {tool.duration_ms > 0 && (
          <span className="text-gray-500 tabular-nums">{formatDuration(tool.duration_ms)}</span>
        )}
      </button>
      {expanded && tool.input && (
        <div className="px-3 pb-2 border-t border-gray-800/40">
          <pre className="mt-2 bg-gray-900 border border-gray-800 rounded p-2 text-xs font-mono text-gray-400 overflow-x-auto max-h-40">
            {typeof tool.input === "string"
              ? tool.input
              : JSON.stringify(tool.input, null, 2)}
          </pre>
          {tool.error && (
            <pre className="mt-1 bg-error/5 border border-error/20 rounded p-2 text-xs font-mono text-error overflow-x-auto">
              {tool.error}
            </pre>
          )}
        </div>
      )}
    </div>
  );
}

interface TimelineEntryProps {
  decision: DecisionLog;
  onSelect?: (decision: DecisionLog) => void;
}

export default function TimelineEntry({ decision, onSelect }: TimelineEntryProps) {
  const [expanded, setExpanded] = useState(false);

  const isSuccess = decision.outcome?.success !== false;
  const toolCount = decision.tool_calls?.length || 0;
  const hasError = decision.tool_calls?.some(
    (t) => String(t.status) === "STATUS_ERROR" || String(t.status) === "4"
  );

  return (
    <div className="relative flex gap-4">
      {/* Timeline dot + connector */}
      <div className="flex flex-col items-center shrink-0 pt-1">
        <div
          className={clsx(
            "w-3 h-3 rounded-full border-2 z-10",
            isSuccess && !hasError
              ? "border-success bg-success/20"
              : hasError
                ? "border-error bg-error/20"
                : "border-warning bg-warning/20"
          )}
        />
        <div className="w-px flex-1 bg-gray-800 mt-1" />
      </div>

      {/* Content */}
      <div className="flex-1 pb-6 min-w-0">
        {/* Header */}
        <div className="flex items-start justify-between gap-2 mb-2">
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-2 mb-1">
              <span className="text-xs text-gray-500 font-mono tabular-nums">
                {formatTimestamp(decision.timestamp)}
              </span>
              <StatusBadge
                variant={isSuccess && !hasError ? "success" : hasError ? "error" : "warning"}
                dot
              >
                {isSuccess && !hasError ? "Success" : hasError ? "Has Errors" : "Warning"}
              </StatusBadge>
            </div>
            {/* User prompt */}
            <p className="text-sm text-white font-medium leading-snug">
              <MessageSquare className="w-3.5 h-3.5 text-primary inline mr-1.5 -mt-0.5" />
              {truncate(decision.user_prompt || "—", 120)}
            </p>
          </div>

          {/* Stats */}
          <div className="flex items-center gap-3 text-xs text-gray-500 shrink-0">
            {toolCount > 0 && (
              <span className="flex items-center gap-1">
                <Terminal className="w-3 h-3" />
                {toolCount}
              </span>
            )}
            {decision.duration_ms > 0 && (
              <span className="flex items-center gap-1">
                <Clock className="w-3 h-3" />
                {formatDuration(decision.duration_ms)}
              </span>
            )}
            {(decision.tokens_input > 0 || decision.tokens_output > 0) && (
              <span className="flex items-center gap-1">
                <Coins className="w-3 h-3" />
                {((decision.tokens_input + decision.tokens_output) / 1000).toFixed(1)}k
              </span>
            )}
          </div>
        </div>

        {/* Expandable tool calls */}
        {toolCount > 0 && (
          <div>
            <button
              onClick={() => setExpanded(!expanded)}
              className="flex items-center gap-1.5 text-xs text-gray-400 hover:text-gray-300 transition-colors mb-2"
            >
              <ChevronRight
                className={clsx(
                  "w-3 h-3 transition-transform",
                  expanded && "rotate-90"
                )}
              />
              {toolCount} tool call{toolCount > 1 ? "s" : ""}
            </button>
            {expanded && (
              <div className="space-y-1 ml-1">
                {decision.tool_calls.map((tool, i) => (
                  <ToolCallItem key={tool.id || i} tool={tool} />
                ))}
              </div>
            )}
          </div>
        )}

        {/* View detail link */}
        {onSelect && (
          <button
            onClick={() => onSelect(decision)}
            className="text-xs text-primary/70 hover:text-primary transition-colors mt-1"
          >
            View full detail →
          </button>
        )}
      </div>
    </div>
  );
}
