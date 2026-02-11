import { clsx } from "clsx";
import { Terminal, CheckCircle2, XCircle, Clock } from "lucide-react";
import type { ToolExecution } from "@/types/alice";

function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`;
  return `${(ms / 60000).toFixed(1)}m`;
}

/** Parse timestamp to epoch ms */
function toMs(ts: string | { seconds: number; nanos?: number }): number {
  if (typeof ts === "string") return new Date(ts).getTime();
  if (ts && typeof ts === "object" && "seconds" in ts) {
    return ts.seconds * 1000 + (ts.nanos ?? 0) / 1_000_000;
  }
  return 0;
}

interface ToolCallGanttProps {
  tools: ToolExecution[];
  className?: string;
}

export default function ToolCallGantt({ tools, className }: ToolCallGanttProps) {
  if (tools.length === 0) return null;

  // Calculate time bounds
  const starts = tools.map((t) => toMs(t.timestamp)).filter((t) => t > 0);
  const minTime = Math.min(...starts);
  const maxTime = Math.max(
    ...tools.map((t) => {
      const start = toMs(t.timestamp);
      return start > 0 ? start + Math.max(t.duration_ms || 0, 1) : 0;
    })
  );
  const totalSpan = Math.max(maxTime - minTime, 1);

  return (
    <div className={clsx("space-y-1", className)}>
      {tools.map((tool, i) => {
        const s = String(tool.status);
        const isSuccess = s === "STATUS_SUCCESS" || s === "2";
        const isError = s === "STATUS_ERROR" || s === "4";
        const start = toMs(tool.timestamp);
        const duration = Math.max(tool.duration_ms || 0, 1);

        // Calculate bar position as percentage
        const leftPct = start > 0 ? ((start - minTime) / totalSpan) * 100 : 0;
        const widthPct = Math.max((duration / totalSpan) * 100, 1.5); // min 1.5% for visibility

        return (
          <div key={tool.id || i} className="flex items-center gap-2 text-xs h-6">
            {/* Tool name */}
            <div className="w-28 shrink-0 flex items-center gap-1 truncate">
              <Terminal className="w-3 h-3 text-gray-600 shrink-0" />
              <span className="font-mono text-gray-400 truncate" title={tool.tool_name}>
                {tool.tool_name}
              </span>
            </div>

            {/* Gantt bar */}
            <div className="flex-1 relative h-4 bg-gray-900/50 rounded overflow-hidden">
              <div
                className={clsx(
                  "absolute top-0.5 bottom-0.5 rounded-sm transition-all",
                  isSuccess && "bg-success/40",
                  isError && "bg-error/40",
                  !isSuccess && !isError && "bg-warning/40"
                )}
                style={{
                  left: `${leftPct}%`,
                  width: `${widthPct}%`,
                }}
              />
            </div>

            {/* Status + Duration */}
            <div className="w-20 shrink-0 flex items-center justify-end gap-1">
              {isSuccess && <CheckCircle2 className="w-3 h-3 text-success" />}
              {isError && <XCircle className="w-3 h-3 text-error" />}
              {!isSuccess && !isError && <Clock className="w-3 h-3 text-warning" />}
              <span className="text-gray-500 tabular-nums font-mono">
                {tool.duration_ms > 0 ? formatDuration(tool.duration_ms) : "—"}
              </span>
            </div>
          </div>
        );
      })}

      {/* Time scale */}
      <div className="flex items-center gap-2 text-[10px] text-gray-600 pt-1 border-t border-gray-800/30">
        <div className="w-28 shrink-0" />
        <div className="flex-1 flex justify-between">
          <span>0</span>
          <span>{formatDuration(totalSpan)}</span>
        </div>
        <div className="w-20 shrink-0" />
      </div>
    </div>
  );
}
