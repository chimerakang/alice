import { clsx } from "clsx";
import { useTranslation } from "react-i18next";
import { livenessStyle } from "@/lib/fleet";
import type { FleetLiveness } from "@/types/fleet";

/**
 * What is known about a task's agent process. "unknown" is drawn hollow and
 * dashed so that it can never be mistaken for "idle".
 */
export default function LivenessDot({ liveness, withLabel = false }: { liveness: FleetLiveness; withLabel?: boolean }) {
  const { t } = useTranslation();
  const style = livenessStyle(liveness);
  const label = t(`fleet.liveness.${liveness}`);
  return (
    <span className="inline-flex items-center gap-1.5" title={label} data-liveness={liveness}>
      <span className={clsx("w-2 h-2 rounded-full shrink-0", style.dot, style.pulse && "animate-pulse")} />
      {withLabel && <span className="text-xs text-gray-400">{label}</span>}
    </span>
  );
}
