import { Calendar, RotateCcw } from "lucide-react";
import { useState, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { clsx } from "clsx";

export interface DateRange {
  startTime?: string; // RFC3339
  endTime?: string;   // RFC3339
}

interface Preset {
  key: "1h" | "6h" | "24h" | "7d" | "30d" | "all";
  hours: number; // 0 = all time
}

const PRESETS: Preset[] = [
  { key: "1h", hours: 1 },
  { key: "6h", hours: 6 },
  { key: "24h", hours: 24 },
  { key: "7d", hours: 168 },
  { key: "30d", hours: 720 },
  { key: "all", hours: 0 },
];

interface DateRangeFilterProps {
  onChange: (range: DateRange) => void;
  className?: string;
  /** Compact mode shows only preset buttons (no datetime inputs) */
  compact?: boolean;
}

/** Convert Date to local datetime-local input value */
function toLocalInput(d: Date): string {
  const offset = d.getTimezoneOffset();
  const local = new Date(d.getTime() - offset * 60000);
  return local.toISOString().slice(0, 16);
}

export default function DateRangeFilter({
  onChange,
  className,
  compact = false,
}: DateRangeFilterProps) {
  const { t } = useTranslation();
  const [activePreset, setActivePreset] = useState<string>("all");
  const [customStart, setCustomStart] = useState("");
  const [customEnd, setCustomEnd] = useState("");

  const applyPreset = useCallback(
    (preset: Preset) => {
      setActivePreset(preset.key);
      setCustomStart("");
      setCustomEnd("");

      if (preset.hours === 0) {
        onChange({});
        return;
      }

      const end = new Date();
      const start = new Date(end.getTime() - preset.hours * 3600_000);
      onChange({
        startTime: start.toISOString(),
        endTime: end.toISOString(),
      });
    },
    [onChange]
  );

  const applyCustom = useCallback(() => {
    if (!customStart && !customEnd) return;
    setActivePreset("");
    onChange({
      startTime: customStart ? new Date(customStart).toISOString() : undefined,
      endTime: customEnd ? new Date(customEnd).toISOString() : undefined,
    });
  }, [customStart, customEnd, onChange]);

  const reset = useCallback(() => {
    setActivePreset("all");
    setCustomStart("");
    setCustomEnd("");
    onChange({});
  }, [onChange]);

  return (
    <div className={clsx("flex flex-wrap items-center gap-2", className)}>
      <Calendar className="w-3.5 h-3.5 text-gray-500 shrink-0" />

      {/* Preset buttons */}
      <div className="flex items-center gap-1">
        {PRESETS.map((p) => (
          <button
            key={p.key}
            onClick={() => applyPreset(p)}
            className={clsx(
              "px-2 py-0.5 text-xs rounded border transition-colors",
              activePreset === p.key
                ? "bg-primary/15 text-primary border-primary/30"
                : "text-gray-400 border-gray-700 hover:border-gray-600 hover:text-gray-300"
            )}
          >
            {t(`date_range.presets.${p.key}`)}
          </button>
        ))}
      </div>

      {/* Custom datetime inputs (hidden in compact mode) */}
      {!compact && (
        <>
          <div className="flex items-center gap-1.5 text-xs text-gray-400">
            <input
              type="datetime-local"
              value={customStart}
              onChange={(e) => setCustomStart(e.target.value)}
              onBlur={applyCustom}
              className="bg-gray-900 border border-gray-700 rounded px-2 py-0.5 text-xs text-gray-300 focus:outline-none focus:border-primary/50 w-[170px]"
              max={customEnd || toLocalInput(new Date())}
            />
            <span>~</span>
            <input
              type="datetime-local"
              value={customEnd}
              onChange={(e) => setCustomEnd(e.target.value)}
              onBlur={applyCustom}
              className="bg-gray-900 border border-gray-700 rounded px-2 py-0.5 text-xs text-gray-300 focus:outline-none focus:border-primary/50 w-[170px]"
              min={customStart}
              max={toLocalInput(new Date())}
            />
          </div>

          {(customStart || customEnd) && (
            <button
              onClick={reset}
              className="p-1 text-gray-500 hover:text-gray-300 transition-colors"
              title={t("date_range.reset")}
            >
              <RotateCcw className="w-3.5 h-3.5" />
            </button>
          )}
        </>
      )}
    </div>
  );
}
