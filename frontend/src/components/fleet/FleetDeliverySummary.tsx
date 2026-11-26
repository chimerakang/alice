import { CheckCircle2, ExternalLink, XCircle } from "lucide-react";
import { useTranslation } from "react-i18next";
import { formatDuration } from "@/lib/fleet";
import type { FleetDelivery, FleetDeliveryItem } from "@/types/fleet";

const stateTone: Record<FleetDelivery["verification"], string> = {
  passed: "text-success",
  failed: "text-error",
  not_run: "text-warning",
  not_configured: "text-warning",
};

function Items({ title, items, testId }: { title: string; items: FleetDeliveryItem[]; testId: string }) {
  const { t } = useTranslation();
  if (items.length === 0) return null;
  return (
    <div data-testid={testId}>
      <p className="text-xs font-medium text-gray-400">{title}</p>
      <ul className="list-disc pl-5 text-sm text-gray-300 space-y-0.5">
        {items.map((it, i) => (
          <li key={`${it.code}-${i}`} className="break-words">
            {t(`fleet.delivery.item.${it.code}`, { detail: it.detail.split("\n")[0].slice(0, 160) })}
          </li>
        ))}
      </ul>
    </div>
  );
}

/**
 * What the reviewer is about to approve: the delivered changes, how each check
 * came out and in which run, what is not done, and what to do next. It reads
 * one summary from the server, so it matches what Telegram says.
 */
export default function FleetDeliverySummary({ delivery: d, onShowDiff }: { delivery: FleetDelivery; onShowDiff?: () => void }) {
  const { t } = useTranslation();
  const passed = d.checks.filter((c) => c.passed).length;
  return (
    <section className="rounded-md border border-gray-800 bg-gray-900/40 p-4 space-y-3" data-testid="fleet-delivery">
      <h2 className="text-sm font-semibold text-white">{t("fleet.delivery.title")}</h2>

      {d.diff_known && (
        <p className="text-sm text-gray-300" data-testid="fleet-delivery-changes">
          {t("fleet.delivery.changes", { files: d.files + d.untracked, ins: d.insertions, del: d.deletions })}
        </p>
      )}

      <div data-testid="fleet-delivery-verify" data-state={d.verification}>
        <p className={`text-sm font-medium ${stateTone[d.verification]}`}>{t(`fleet.delivery.verify_${d.verification}`)}</p>
        {d.checks.length > 0 && (
          <div className="mt-1 space-y-1">
            <p className="text-xs text-gray-500">
              {d.checks_previous
                ? t("fleet.delivery.checks_previous", { run: d.checks_run })
                : t("fleet.delivery.checks_of_run", { run: d.checks_run })}
              {" · "}
              {passed}/{d.checks.length}
            </p>
            <ul className="text-xs space-y-0.5" data-testid="fleet-delivery-checks">
              {d.checks.map((c, i) => (
                <li key={i} className="flex items-center gap-2 text-gray-300">
                  {c.passed ? <CheckCircle2 className="w-3.5 h-3.5 text-success" /> : <XCircle className="w-3.5 h-3.5 text-error" />}
                  <code className="break-all">{c.command || c.name}</code>
                  <span className="text-gray-600 tabular-nums">{formatDuration(c.duration_ms)}</span>
                </li>
              ))}
            </ul>
          </div>
        )}
      </div>

      {d.stop_reason && (
        <p className="text-sm text-warning break-words" data-testid="fleet-delivery-stop">
          {t("fleet.delivery.stop")}: {d.stop_reason}
        </p>
      )}

      <div data-testid="fleet-delivery-note">
        <p className="text-xs font-medium text-gray-400">{t("fleet.delivery.note")}</p>
        {d.note_missing ? (
          <p className="text-sm text-gray-500">{t("fleet.delivery.note_missing")}</p>
        ) : (
          <p className="text-sm text-gray-300 whitespace-pre-wrap break-words max-h-48 overflow-auto">{d.note}</p>
        )}
      </div>

      <Items title={t("fleet.delivery.completed")} items={d.completed} testId="fleet-delivery-completed" />
      <Items title={t("fleet.delivery.pending")} items={d.pending} testId="fleet-delivery-pending" />
      <Items title={t("fleet.delivery.limits")} items={d.limits} testId="fleet-delivery-limits" />

      {d.runs.length > 0 && (
        <div data-testid="fleet-delivery-runs">
          <p className="text-xs font-medium text-gray-400">{t("fleet.delivery.runs")}</p>
          <ol className="text-xs text-gray-400 space-y-0.5">
            {d.runs.map((r) => (
              <li key={r.number} data-outcome={r.outcome}>
                {t("fleet.delivery.run", { n: r.number })} · {t(`fleet.delivery.outcome_${r.outcome}`)}
                {r.attempt > 0 && ` (${t("fleet.delivery.round", { n: r.attempt })})`}
                {r.stop_reason && ` — ${r.stop_reason.split("\n")[0]}`}
              </li>
            ))}
          </ol>
        </div>
      )}

      <div className="flex flex-wrap items-center gap-4 text-xs">
        {onShowDiff && (
          <button type="button" onClick={onShowDiff} className="text-primary hover:underline">
            {t("fleet.delivery.view_diff")}
          </button>
        )}
        {d.pr_url && (
          <a href={d.pr_url} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 text-primary hover:underline">
            <ExternalLink className="w-3 h-3" />
            {t("fleet.delivery.view_pr", { n: d.pr_number })}
          </a>
        )}
      </div>
      <p className="text-xs text-gray-500" data-testid="fleet-delivery-next">
        {t(`fleet.delivery.next_${d.next}`)}
      </p>
    </section>
  );
}
