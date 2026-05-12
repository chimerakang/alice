import { Fragment, useState } from "react";
import { useTranslation } from "react-i18next";
import { ChevronDown } from "lucide-react";
import { clsx } from "clsx";
import type { UnifiedReviewSubTaskResult } from "@/types/alice";

function truncateSubTaskId(value: string, maxLength = 60): string {
  const normalized = value.trim();
  if (normalized.length <= maxLength) return normalized;
  return `${normalized.slice(0, maxLength).trimEnd()}…`;
}

interface ReviewSubTaskTableProps {
  subTaskResults: UnifiedReviewSubTaskResult[];
  className?: string;
  retryEvidenceHref?: string;
}

export default function ReviewSubTaskTable({ subTaskResults, className, retryEvidenceHref }: ReviewSubTaskTableProps) {
  const { t } = useTranslation();
  const [expandedSubTasks, setExpandedSubTasks] = useState<string[]>([]);

  if (subTaskResults.length === 0) {
    return null;
  }

  const toggleSubTask = (subTaskId: string) => {
    setExpandedSubTasks((current) =>
      current.includes(subTaskId) ? current.filter((id) => id !== subTaskId) : [...current, subTaskId]
    );
  };

  return (
    <div className={clsx("mt-4 space-y-2", className)}>
      <div className="text-xs text-gray-500 uppercase">{t("reviews.subtask_section_title")}</div>
      <div className="overflow-hidden rounded-md border border-gray-800/60">
        <table className="w-full border-collapse text-xs">
          <thead className="bg-black/20">
            <tr className="text-gray-500">
              <th className="px-3 py-2 w-12 text-left font-medium">{t("reviews.subtask_col_index")}</th>
              <th className="px-3 py-2 text-left font-medium">{t("reviews.subtask_col_desc")}</th>
              <th className="px-3 py-2 w-24 text-right font-medium">{t("reviews.subtask_col_score")}</th>
            </tr>
          </thead>
          <tbody>
            {subTaskResults.map((subTask, index) => {
              const expanded = expandedSubTasks.includes(subTask.sub_task_id);
              const tags = subTask.issue_tags || [];
              const isLowScore = subTask.score < 70;

              return (
                <Fragment key={subTask.sub_task_id}>
                  <tr
                    className={clsx(
                      "border-t border-gray-800/60 hover:bg-white/5 cursor-pointer align-top",
                      isLowScore && "bg-red-950/10",
                    )}
                    onClick={() => toggleSubTask(subTask.sub_task_id)}
                    aria-expanded={expanded}
                  >
                    <td className="px-3 py-2 font-mono text-gray-500">{index + 1}</td>
                    <td className="px-3 py-2">
                      <div className="flex items-start gap-2">
                        <ChevronDown
                          className={clsx(
                            "mt-0.5 h-3.5 w-3.5 shrink-0 text-gray-500 transition-transform",
                            expanded && "rotate-180"
                          )}
                        />
                        <button
                          type="button"
                          className="min-w-0 text-left"
                          onClick={(event) => {
                            event.stopPropagation();
                            toggleSubTask(subTask.sub_task_id);
                          }}
                        >
                          <span className="block truncate font-mono text-primary-light" title={subTask.sub_task_id}>
                            {truncateSubTaskId(subTask.sub_task_id)}
                          </span>
                        </button>
                      </div>
                    </td>
                    <td className="px-3 py-2 text-right font-mono text-gray-300">{subTask.score}/100</td>
                  </tr>
                  <tr className={clsx("border-t border-gray-800/40", !expanded && "hidden")}>
                    <td className="px-3 pb-3" colSpan={3}>
                      <div className="rounded-md border border-gray-800/60 bg-black/20 p-3">
                        <div className="mb-2">
                          <div className="text-[10px] uppercase text-gray-500 mb-1">{t("reviews.subtask_feedback")}</div>
                          <p className="text-sm text-gray-300 whitespace-pre-wrap">{subTask.feedback || "—"}</p>
                        </div>
                        {isLowScore && retryEvidenceHref ? (
                          <div className="mb-2">
                            <div className="text-[10px] uppercase text-gray-500 mb-1">{t("reviews.subtask_retry_action")}</div>
                            <a
                              href={retryEvidenceHref}
                              className="inline-flex items-center rounded-md border border-orange-700/60 bg-orange-950/30 px-2 py-1 text-xs text-orange-200 hover:border-orange-500 hover:text-orange-100"
                            >
                              {t("reviews.subtask_retry_evidence")}
                            </a>
                          </div>
                        ) : null}
                        <div>
                          <div className="text-[10px] uppercase text-gray-500 mb-1">{t("reviews.subtask_issue_tags")}</div>
                          {tags.length > 0 ? (
                            <div className="flex flex-wrap gap-1">
                              {tags.map((tag) => (
                                <span key={tag} className="px-1.5 py-0.5 rounded bg-gray-800 text-[10px] text-gray-400">
                                  {tag}
                                </span>
                              ))}
                            </div>
                          ) : (
                            <span className="text-xs text-gray-500">{t("reviews.subtask_no_issue_tags")}</span>
                          )}
                        </div>
                      </div>
                    </td>
                  </tr>
                </Fragment>
              );
            })}
          </tbody>
        </table>
      </div>
    </div>
  );
}
