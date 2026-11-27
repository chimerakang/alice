import { useTranslation } from "react-i18next";
import type { FleetDelivery, FleetTask } from "@/types/fleet";

export default function FleetDecisionContext({ task, delivery }: {
  task: FleetTask;
  delivery: FleetDelivery | null;
}) {
  const { t } = useTranslation();
  const reason = delivery?.stop_reason || task.status_reason;
  const interrupted = reason === "runner stopped: context canceled";
  const hasSession = Boolean(task.session_id);
  const note = delivery?.note?.trim();

  return (
    <section className="space-y-2 rounded-md border border-warning/40 bg-warning/5 p-4 text-sm" aria-labelledby="fleet-decision-title">
      <h2 id="fleet-decision-title" className="font-semibold text-warning">{t("fleet.decision_context.title")}</h2>
      <p className="break-words text-gray-100">
        {interrupted ? t("fleet.decision_context.interrupted") : reason || t("fleet.decision_context.reason_unavailable")}
      </p>
      <p className="text-gray-300">
        {delivery ? t(`fleet.delivery.verify_${delivery.verification}`) : t("fleet.decision_context.checks_unavailable")}
        {delivery?.diff_known && ` · ${t("fleet.decision_context.changed_files", { count: delivery.files + delivery.untracked })}`}
      </p>
      {delivery && (
        <p className="whitespace-pre-wrap break-words text-gray-300">
          {note || t("fleet.delivery.note_missing")}
        </p>
      )}
      <p className="text-gray-200">{t(hasSession ? "fleet.decision_context.reply" : "fleet.decision_context.no_session")}</p>
      <ul className="space-y-1 text-xs text-gray-400">
        <li>{t("fleet.decision_context.retry")}</li>
        <li>{t("fleet.decision_context.pass")}</li>
        <li>{t("fleet.decision_context.discard")}</li>
      </ul>
    </section>
  );
}
