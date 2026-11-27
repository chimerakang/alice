import { clsx } from "clsx";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Clock, Coins, GitPullRequest, Search, Ship } from "lucide-react";
import StatusBadge from "@/components/StatusBadge";
import LivenessDot from "@/components/fleet/LivenessDot";
import { firstLine, formatCost, formatDuration, livenessStyle, statusTone, taskElapsedMs } from "@/lib/fleet";
import { useNow } from "@/hooks/useNow";
import type { FleetTask } from "@/types/fleet";

/** One task on the board. The whole card opens the task's page. */
export default function FleetTaskCard({ task, showProject = true }: { task: FleetTask; showProject?: boolean }) {
  const { t } = useTranslation();
  const now = useNow();
  const ShapeIcon = task.shape === "scout" ? Search : Ship;
  return (
    <Link
      to={`/fleet/tasks/${task.id}`}
      className={clsx(
        "block rounded-lg border bg-surface/60 p-3 transition-colors hover:bg-surface-hover",
        livenessStyle(task.liveness).ring,
      )}
      data-task-id={task.id}
    >
      <div className="flex items-center justify-between gap-2 mb-1.5">
        <StatusBadge variant={statusTone(task.status)} size="sm">
          {t(`fleet.status.${task.status}`)}
        </StatusBadge>
        <LivenessDot liveness={task.liveness} />
      </div>
      <p className="text-sm text-gray-100 leading-snug break-words line-clamp-3">
        <ShapeIcon className="w-3.5 h-3.5 inline mr-1.5 -mt-0.5 text-gray-500" />
        {task.issue_number > 0 && <span className="text-primary mr-1">#{task.issue_number}</span>}
        {firstLine(task.goal) || "—"}
      </p>
      {(task.startup_recovered || task.startup_resume_capped) && (
          <p className="text-xs text-gray-400">
            {t(task.startup_resume_capped ? "fleet.task.startup_resume_capped" : "fleet.task.startup_recovered")}
          </p>
        )}
        {task.status_reason && task.status !== "queued" && (
        <p className="mt-1 text-xs text-gray-500 line-clamp-2 break-words">{firstLine(task.status_reason)}</p>
      )}
      <div className="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1 text-[11px] text-gray-500">
        {showProject && <span className="text-gray-400 truncate max-w-[10rem]">{task.project_name || task.project_id}</span>}
        <span className="flex items-center gap-1 tabular-nums">
          <Clock className="w-3 h-3" />
          {formatDuration(taskElapsedMs(task, now))}
        </span>
        <span className="flex items-center gap-1 tabular-nums">
          <Coins className="w-3 h-3" />
          {formatCost(task.cost_usd)}
        </span>
        {task.pr_number > 0 && (
          <span className="flex items-center gap-1">
            <GitPullRequest className="w-3 h-3" />#{task.pr_number}
          </span>
        )}
      </div>
    </Link>
  );
}
