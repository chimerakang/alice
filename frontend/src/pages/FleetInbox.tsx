import { useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Inbox } from "lucide-react";
import StatusBadge from "@/components/StatusBadge";
import FleetActions from "@/components/fleet/FleetActions";
import FleetUnavailable from "@/components/fleet/FleetUnavailable";
import { fleetApi } from "@/lib/fleetApi";
import { firstLine, formatCost, formatDuration, parsePayload, statusTone } from "@/lib/fleet";
import { useFleet } from "@/hooks/useFleet";
import { useNow } from "@/hooks/useNow";
import { selectInbox } from "@/stores/fleetStore";
import type { FleetTask, FleetVerification } from "@/types/fleet";

/** What the owner needs to see to decide, fetched per waiting task. */
interface Context {
  /** The last thing the agent said: its question, or its summary. */
  said: string;
  /** The failing checks of the latest round. */
  failed: FleetVerification[];
  stat: string;
}

function InboxItem({ task }: { task: FleetTask }) {
  const { t } = useTranslation();
  const [context, setContext] = useState<Context | null>(null);

  // Reloaded when the task changes: a new question is a new context.
  useEffect(() => {
    let stale = false;
    (async () => {
      try {
        const [lastText, checks, diff] = await Promise.all([
          fleetApi.getLastTaskEvent(task.id, "agent.text"),
          fleetApi.getTaskVerifications(task.id),
          task.shape === "ship" ? fleetApi.getTaskDiff(task.id) : Promise.resolve(null),
        ]);
        if (stale) return;
        const last = lastText ? parsePayload(lastText).text : "";
        const latest = checks.reduce((max, c) => Math.max(max, c.attempt), 0);
        setContext({
          said: typeof last === "string" ? last.trim() : "",
          failed: checks.filter((c) => c.attempt === latest && !c.passed),
          stat: (diff?.stat ?? "").trim().split("\n").pop()?.trim() ?? "",
        });
      } catch {
        if (!stale) setContext({ said: "", failed: [], stat: "" });
      }
    })();
    return () => {
      stale = true;
    };
  }, [task.id, task.updated_at, task.shape]);

  const now = useNow();
  const waited = task.updated_at ? Math.max(0, now - Date.parse(task.updated_at)) : 0;

  return (
    <article className="rounded-lg border border-gray-800 bg-surface/40 p-4 space-y-3" data-inbox-task={task.id}>
      <div className="flex flex-wrap items-center gap-2">
        <StatusBadge variant={statusTone(task.status)} size="sm">
          {t(`fleet.status.${task.status}`)}
        </StatusBadge>
        <span className="text-xs text-gray-500">
          {task.project_name || task.project_id} · {t(`fleet.shape.${task.shape}`)} · {t(`fleet.trust.${task.trust_mode}`)} ·{" "}
          {t("fleet.inbox.waiting", { time: formatDuration(waited) })} · {formatCost(task.cost_usd)}
        </span>
        <Link to={`/fleet/tasks/${task.id}`} className="ml-auto text-xs text-primary/80 hover:text-primary">
          {t("fleet.inbox.open")} →
        </Link>
      </div>
      <h2 className="text-sm font-medium text-white break-words">
        {task.issue_number > 0 && <span className="text-primary mr-1.5">#{task.issue_number}</span>}
        {firstLine(task.goal)}
      </h2>
      {task.status_reason && <p className="text-xs text-warning break-words">{task.status_reason}</p>}

      {context === null && <p className="text-xs text-gray-600">{t("common.loading")}</p>}
      {context?.said && (
        <blockquote className="max-h-48 overflow-y-auto border-l-2 border-primary/40 pl-3 text-sm text-gray-300 whitespace-pre-wrap break-words">
          {context.said}
        </blockquote>
      )}
      {context?.failed.map((check) => (
        <details key={check.id} className="rounded-md border border-error/30 bg-error/5 text-xs">
          <summary className="cursor-pointer px-3 py-1.5 text-error break-all">
            ✗ {check.command || check.name} ({t("fleet.task.exit_code", { code: check.exit_code })})
          </summary>
          <pre className="max-h-48 overflow-auto border-t border-error/20 p-3 text-gray-300 whitespace-pre-wrap break-words">
            {check.output_tail || check.error}
          </pre>
        </details>
      ))}
      {context?.stat && <p className="text-xs font-mono text-gray-500">{context.stat}</p>}
      {task.pr_url && (
        <a href={task.pr_url} target="_blank" rel="noreferrer" className="block text-xs text-primary hover:underline break-all">
          {task.pr_url}
        </a>
      )}

      <FleetActions task={task} compact />
    </article>
  );
}

/** Everything waiting on the owner, each with what is needed to decide. */
export default function FleetInbox() {
  const { t } = useTranslation();
  const { loaded, unavailable, error, tasks } = useFleet();
  const waiting = useMemo(() => selectInbox(tasks), [tasks]);
  const decisions = waiting.filter((task) => task.status === "needs_decision");
  const reviews = waiting.filter((task) => task.status === "review_ready");

  if (!loaded) return <p className="text-sm text-gray-500">{t("common.loading")}</p>;
  if (unavailable) return <FleetUnavailable reason={unavailable} error={error} />;

  return (
    <div className="space-y-6" data-testid="fleet-inbox">
      <div>
        <h1 className="text-2xl font-bold text-white">{t("fleet.inbox.title")}</h1>
        <p className="text-sm text-gray-500">{t("fleet.inbox.subtitle")}</p>
      </div>

      {waiting.length === 0 && (
        <div className="rounded-lg border border-gray-800 p-10 text-center text-sm text-gray-400">
          <Inbox className="mx-auto mb-3 h-8 w-8 text-gray-600" />
          {t("fleet.inbox.empty")}
        </div>
      )}

      {decisions.length > 0 && (
        <section className="space-y-3">
          <h2 className="text-xs font-semibold uppercase tracking-wider text-gray-400">
            {t("fleet.columns.needs_decision")} · {decisions.length}
          </h2>
          {decisions.map((task) => (
            <InboxItem key={task.id} task={task} />
          ))}
        </section>
      )}
      {reviews.length > 0 && (
        <section className="space-y-3">
          <h2 className="text-xs font-semibold uppercase tracking-wider text-gray-400">
            {t("fleet.columns.review_ready")} · {reviews.length}
          </h2>
          {reviews.map((task) => (
            <InboxItem key={task.id} task={task} />
          ))}
        </section>
      )}
    </div>
  );
}
