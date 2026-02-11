import { useState } from "react";
import { clsx } from "clsx";
import { ChevronRight, FilePlus, FileMinus, FileCode, Copy, Check } from "lucide-react";
import type { DiffFile } from "@/types/alice";

/** Status icon + color for a diff file */
function fileStatusInfo(status: DiffFile["status"]) {
  switch (status) {
    case "added":
      return { Icon: FilePlus, color: "text-success", label: "A" };
    case "deleted":
      return { Icon: FileMinus, color: "text-error", label: "D" };
    default:
      return { Icon: FileCode, color: "text-warning", label: "M" };
  }
}

/** Parse a diff chunk string into colored lines */
function DiffLines({ chunks }: { chunks: string }) {
  const lines = chunks.split("\n");
  // Find where the actual hunk content starts (after @@)
  let inHunk = false;

  return (
    <pre className="text-xs font-mono leading-5 overflow-x-auto">
      {lines.map((line, i) => {
        // Skip file headers (---, +++, index, etc.)
        if (
          line.startsWith("---") ||
          line.startsWith("+++") ||
          line.startsWith("index ") ||
          line.startsWith("new file") ||
          line.startsWith("deleted file") ||
          line.startsWith("old mode") ||
          line.startsWith("new mode") ||
          line.startsWith("similarity")
        ) {
          return null;
        }

        // Hunk header
        if (line.startsWith("@@")) {
          inHunk = true;
          return (
            <div key={i} className="bg-primary/5 text-primary/70 px-3 py-0.5">
              {line}
            </div>
          );
        }

        if (!inHunk && !line.startsWith("+") && !line.startsWith("-")) {
          return null;
        }

        const isAdd = line.startsWith("+");
        const isDel = line.startsWith("-");

        return (
          <div
            key={i}
            className={clsx(
              "px-3",
              isAdd && "bg-success/8 text-success/90",
              isDel && "bg-error/8 text-error/90",
              !isAdd && !isDel && "text-gray-500"
            )}
          >
            {line || " "}
          </div>
        );
      })}
    </pre>
  );
}

/** Single collapsible file diff */
function FileDiff({ file }: { file: DiffFile }) {
  const [open, setOpen] = useState(false);
  const [copied, setCopied] = useState(false);
  const { Icon, color, label } = fileStatusInfo(file.status);

  const handleCopy = (e: React.MouseEvent) => {
    e.stopPropagation();
    navigator.clipboard.writeText(file.chunks);
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  };

  return (
    <div className="border border-gray-800 rounded-md overflow-hidden">
      <button
        onClick={() => setOpen(!open)}
        className="w-full flex items-center gap-2 px-3 py-2 text-left hover:bg-white/3 transition-colors text-xs"
      >
        <ChevronRight
          className={clsx(
            "w-3 h-3 text-gray-600 transition-transform shrink-0",
            open && "rotate-90"
          )}
        />
        <Icon className={clsx("w-3.5 h-3.5 shrink-0", color)} />
        <span className="font-mono text-gray-300 flex-1 truncate">
          {file.path}
        </span>
        <span className={clsx("font-mono text-[10px] px-1.5 rounded", color)}>
          {label}
        </span>
        {file.additions > 0 && (
          <span className="text-success font-mono">+{file.additions}</span>
        )}
        {file.deletions > 0 && (
          <span className="text-error font-mono">-{file.deletions}</span>
        )}
        <button
          onClick={handleCopy}
          className="p-0.5 hover:bg-gray-700 rounded ml-1"
          title="Copy diff"
        >
          {copied ? (
            <Check className="w-3 h-3 text-success" />
          ) : (
            <Copy className="w-3 h-3 text-gray-600" />
          )}
        </button>
      </button>
      {open && file.chunks && (
        <div className="border-t border-gray-800/50 bg-gray-950 max-h-96 overflow-y-auto">
          <DiffLines chunks={file.chunks} />
        </div>
      )}
    </div>
  );
}

interface DiffViewerProps {
  files: DiffFile[];
  className?: string;
}

export default function DiffViewer({ files, className }: DiffViewerProps) {
  if (files.length === 0) {
    return (
      <p className="text-sm text-gray-500">No file changes detected</p>
    );
  }

  const totalAdds = files.reduce((s, f) => s + f.additions, 0);
  const totalDels = files.reduce((s, f) => s + f.deletions, 0);

  return (
    <div className={clsx("space-y-2", className)}>
      {/* Summary */}
      <div className="flex items-center gap-3 text-xs text-gray-500">
        <span>{files.length} file{files.length !== 1 ? "s" : ""} changed</span>
        {totalAdds > 0 && <span className="text-success">+{totalAdds}</span>}
        {totalDels > 0 && <span className="text-error">-{totalDels}</span>}
      </div>

      {/* File list */}
      <div className="space-y-1">
        {files.map((f) => (
          <FileDiff key={f.path} file={f} />
        ))}
      </div>
    </div>
  );
}
