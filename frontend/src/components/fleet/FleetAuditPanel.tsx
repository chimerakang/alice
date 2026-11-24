import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { CheckCircle2, XCircle } from "lucide-react";
import { fleetApi, FleetApiError } from "@/lib/fleetApi";
import type { FleetAuditEntry, FleetProject } from "@/types/fleet";

/** The rows, split out so they can be rendered on their own in tests. */
export function FleetAuditTable({ entries }: { entries: FleetAuditEntry[] }) {
  const { t } = useTranslation();
  if (entries.length === 0) return <p className="text-sm text-gray-500">{t("fleet.audit.empty")}</p>;
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm" data-testid="fleet-audit-table">
        <thead>
          <tr className="text-left text-xs text-gray-500">
            <th className="py-1.5 pr-4 font-medium">{t("fleet.audit.time")}</th>
            <th className="py-1.5 pr-4 font-medium">{t("fleet.audit.actor")}</th>
            <th className="py-1.5 pr-4 font-medium">{t("fleet.audit.via")}</th>
            <th className="py-1.5 pr-4 font-medium">{t("fleet.audit.command")}</th>
            <th className="py-1.5 pr-4 font-medium">{t("fleet.audit.target")}</th>
            <th className="py-1.5 font-medium">{t("fleet.audit.result")}</th>
          </tr>
        </thead>
        <tbody>
          {entries.map((e) => (
            <tr key={e.id} className="border-t border-gray-800/60 align-top" data-audit-ok={e.ok}>
              <td className="py-1.5 pr-4 whitespace-nowrap font-mono text-xs tabular-nums text-gray-500">
                {e.created_at ? new Date(e.created_at).toLocaleString([], { hour12: false }) : ""}
              </td>
              <td className="py-1.5 pr-4 text-gray-300">{e.actor}</td>
              <td className="py-1.5 pr-4 text-gray-400">{t(`fleet.audit.via_${e.via}`, { defaultValue: e.via })}</td>
              <td className="py-1.5 pr-4 font-mono text-xs text-primary-light">{e.command}</td>
              <td className="py-1.5 pr-4 text-xs text-gray-400">
                {e.project_id}
                {e.task_id && (
                  <>
                    {" · "}
                    <Link to={`/fleet/tasks/${e.task_id}`} className="font-mono hover:text-primary">
                      {e.task_id}
                    </Link>
                  </>
                )}
                {e.detail && <span className="block text-gray-500 break-words">{e.detail}</span>}
              </td>
              <td className="py-1.5 text-xs">
                {e.ok ? (
                  <span className="inline-flex items-center gap-1 text-success">
                    <CheckCircle2 className="w-3.5 h-3.5" />
                    {t("fleet.audit.ok")}
                  </span>
                ) : (
                  <span className="inline-flex items-start gap-1 text-error">
                    <XCircle className="w-3.5 h-3.5 shrink-0 mt-0.5" />
                    <span className="break-words">{e.error || t("fleet.audit.refused")}</span>
                  </span>
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

/**
 * The fleet's audit log on the Security page: who gave which command, when,
 * through what, and how it went; filterable by project and by task. Shows
 * nothing when the fleet is off.
 */
export default function FleetAuditPanel() {
  const { t } = useTranslation();
  const [project, setProject] = useState("");
  const [task, setTask] = useState("");
  const [projects, setProjects] = useState<FleetProject[]>([]);
  const [loaded, setLoaded] = useState<{ key: string; entries: FleetAuditEntry[]; error: string; off: boolean } | null>(null);
  const key = `${project}|${task.trim()}`;

  useEffect(() => {
    fleetApi.listProjects().then(setProjects).catch(() => {});
  }, []);

  useEffect(() => {
    let stale = false;
    // Typing a task ID should not fire a request per keystroke.
    const timer = setTimeout(() => {
      fleetApi
        .getAudit({ project: project || undefined, task: task.trim() || undefined, limit: 100 })
        .then((entries) => !stale && setLoaded({ key, entries, error: "", off: false }))
        .catch((err: unknown) => {
          if (stale) return;
          const off = err instanceof FleetApiError && err.disabled;
          setLoaded({ key, entries: [], error: err instanceof Error ? err.message : String(err), off });
        });
    }, 250);
    return () => {
      stale = true;
      clearTimeout(timer);
    };
  }, [key, project, task]);

  if (loaded?.off) return null;
  const input = "rounded-md border border-gray-700 bg-black/40 px-2 py-1.5 text-sm text-gray-200 placeholder:text-gray-600";

  return (
    <section className="card p-5 space-y-4" data-testid="fleet-audit">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <h3 className="text-base font-semibold text-white">{t("fleet.audit.title")}</h3>
          <p className="text-xs text-gray-500">{t("fleet.audit.subtitle")}</p>
        </div>
        <div className="flex flex-wrap gap-2">
          <select value={project} onChange={(e) => setProject(e.target.value)} aria-label={t("fleet.audit.filter_project")} className={input}>
            <option value="">{t("fleet.board.all_projects")}</option>
            {projects.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name || p.id}
              </option>
            ))}
          </select>
          <input value={task} onChange={(e) => setTask(e.target.value)} placeholder={t("fleet.audit.filter_task")} className={`${input} font-mono w-40`} />
        </div>
      </div>
      {!loaded && <p className="text-sm text-gray-500">{t("common.loading")}</p>}
      {loaded?.error && <p className="text-sm text-error break-words">{loaded.error}</p>}
      {loaded && !loaded.error && <FleetAuditTable entries={loaded.entries} />}
    </section>
  );
}
