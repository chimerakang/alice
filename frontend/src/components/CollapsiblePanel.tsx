import { useState, type ReactNode } from "react";
import { ChevronRight } from "lucide-react";
import { clsx } from "clsx";

interface CollapsiblePanelProps {
  title: ReactNode;
  children: ReactNode;
  defaultOpen?: boolean;
  badge?: ReactNode;
  className?: string;
}

export default function CollapsiblePanel({
  title,
  children,
  defaultOpen = false,
  badge,
  className,
}: CollapsiblePanelProps) {
  const [open, setOpen] = useState(defaultOpen);

  return (
    <div className={clsx("border border-gray-800 rounded-lg overflow-hidden", className)}>
      <button
        onClick={() => setOpen(!open)}
        className="w-full flex items-center gap-2 px-4 py-3 text-left hover:bg-white/5 transition-colors"
      >
        <ChevronRight
          className={clsx(
            "w-4 h-4 text-gray-500 transition-transform duration-200",
            open && "rotate-90"
          )}
        />
        <span className="flex-1 text-sm font-medium text-gray-300">
          {title}
        </span>
        {badge}
      </button>
      {open && (
        <div className="px-4 pb-4 border-t border-gray-800/50">
          {children}
        </div>
      )}
    </div>
  );
}
