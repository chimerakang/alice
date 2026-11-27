import { useState } from "react";
import { clsx } from "clsx";
import { useTranslation } from "react-i18next";
import { fleetApi } from "@/lib/fleetApi";
import type { FleetAcceptanceDecision, FleetAcceptanceItem, FleetDelivery } from "@/types/fleet";

const decisionTone: Record<FleetAcceptanceDecision, string> = {
  approved: "text-success",
  pending: "text-gray-400",
  needs_evidence: "text-warning",
  awaiting_external: "text-warning",
};

interface Props {
  delivery: FleetDelivery;
  /** Called with the delivery summary the server returned after a command. */
  onChange: (d: FleetDelivery) => void;
}

function Item({ item, taskId, canReview, run }: { item: FleetAcceptanceItem; taskId: string; canReview: boolean; run: (f: () => Promise<FleetDelivery>) => void }) {
  const { t } = useTranslation();
  const [note, setNote] = useState("");
  const [fill, setFill] = useState(false);
  const [type, setType] = useState("commit");
  const [ref, setRef] = useState("");
  const decide = (decision: string) => run(() => fleetApi.decideAcceptance(taskId, item.id, decision, note.trim()));
  return (
    <li className="rounded border border-gray-800 p-2 space-y-1" data-testid={`fleet-acceptance-item-${item.id}`} data-decision={item.decision} data-report={item.report}>
      <div className="flex flex-wrap items-baseline gap-2">
        <code className="text-xs text-gray-500">{item.id}</code>
        <span className="text-sm text-gray-200 break-words">{item.text}</span>
      </div>
      <p className="text-xs">
        <span className="text-gray-400">{t(`fleet.acceptance.report.${item.report}`)}</span>
        {item.round > 0 && <span className="text-gray-600"> · {t("fleet.acceptance.round", { n: item.round })}</span>}
        {" · "}
        <span className={decisionTone[item.decision]}>{t(`fleet.acceptance.decision.${item.decision}`)}</span>
      </p>
      {item.reopened && <p className="text-xs text-warning">{t(`fleet.acceptance.reopened.${item.reopened}`)}</p>}
      {item.evidence.length > 0 && (
        <ul className="text-xs text-gray-300 space-y-0.5" data-testid="fleet-acceptance-evidence">
          {item.evidence.map((e, i) => (
            <li key={i} className="break-all">
              <span className="text-gray-500">{t(`fleet.acceptance.evidence.${e.type}`)}</span> <code>{e.ref}</code>
              {e.paths?.length > 0 && <span className="text-gray-500"> ({e.paths.join(", ")})</span>}
              {e.note && <span className="text-gray-500"> — {e.note}</span>}
            </li>
          ))}
        </ul>
      )}
      {item.blocker && (
        <p className="text-xs text-warning break-words" data-testid="fleet-acceptance-blocker">
          {t("fleet.acceptance.blocker", { missing: item.blocker.missing, owner: item.blocker.owner })}
        </p>
      )}
      {item.note && <p className="text-xs text-gray-400 break-words">“{item.note}”</p>}
      {canReview && (
        <div className="space-y-1 pt-1">
          <input
            value={note}
            onChange={(e) => setNote(e.target.value)}
            placeholder={t("fleet.acceptance.note_placeholder")}
            className="w-full rounded border border-gray-700 bg-black/40 px-2 py-1 text-xs text-gray-100 focus:border-primary/50 focus:outline-none"
          />
          <div className="flex flex-wrap gap-2 text-xs">
            {item.decision !== "approved" && item.report === "evidence_submitted" && (
              <button type="button" className="rounded border border-success/30 bg-success/15 px-2 py-0.5 text-success" data-action="approve-item" onClick={() => decide("approved")}>
                {t("fleet.acceptance.approve")}
              </button>
            )}
            <button type="button" className="rounded border border-warning/30 bg-warning/15 px-2 py-0.5 text-warning" data-action="ask-evidence" onClick={() => decide("needs_evidence")}>
              {t("fleet.acceptance.ask")}
            </button>
            <button type="button" className="rounded border border-warning/30 bg-warning/10 px-2 py-0.5 text-warning" data-action="mark-external" onClick={() => decide("awaiting_external")}>
              {t("fleet.acceptance.wait")}
            </button>
            {item.decision !== "pending" && (
              <button type="button" className="rounded border border-gray-600/40 px-2 py-0.5 text-gray-300" onClick={() => decide("pending")}>
                {t("fleet.acceptance.reset")}
              </button>
            )}
            <button type="button" className="rounded border border-primary/30 px-2 py-0.5 text-primary" onClick={() => setFill((v) => !v)}>
              {t("fleet.acceptance.fill")}
            </button>
          </div>
          {fill && (
            <div className="flex flex-wrap gap-2">
              <select value={type} onChange={(e) => setType(e.target.value)} aria-label={t("fleet.acceptance.fill_type")} className="rounded border border-gray-700 bg-black/40 px-1 text-xs text-gray-100">
                {["commit", "pr", "report", "test", "file"].map((k) => (
                  <option key={k} value={k}>
                    {t(`fleet.acceptance.evidence.${k}`)}
                  </option>
                ))}
              </select>
              <input
                value={ref}
                onChange={(e) => setRef(e.target.value)}
                placeholder={t("fleet.acceptance.fill_ref")}
                className="flex-1 min-w-[10rem] rounded border border-gray-700 bg-black/40 px-2 py-1 text-xs text-gray-100 focus:border-primary/50 focus:outline-none"
              />
              <button
                type="button"
                disabled={!ref.trim()}
                className="rounded border border-primary/30 bg-primary/15 px-2 py-0.5 text-xs text-primary disabled:opacity-50"
                onClick={() =>
                  run(() => fleetApi.enterAcceptance(taskId, { items: [{ id: item.id, status: "evidence_submitted", evidence: [{ type, ref: ref.trim(), note: note.trim() }] }] }))
                }
              >
                {t("fleet.acceptance.fill_save")}
              </button>
            </div>
          )}
        </div>
      )}
      {item.history.length > 1 && (
        <details className="text-xs text-gray-500">
          <summary>{t("fleet.acceptance.history")}</summary>
          <ol className="pl-4 list-decimal">
            {item.history.map((h, i) => (
              <li key={i}>
                {h.kind} · {h.status} · {h.actor}
                {h.note && ` — ${h.note}`}
              </li>
            ))}
          </ol>
        </details>
      )}
    </li>
  );
}

/**
 * Item-by-item acceptance, shown beside the task's action buttons: what the
 * agent reported per item, what the reviewer decided, and whether the task may
 * close. Automatic verification is shown separately by the delivery summary.
 */
export default function FleetAcceptancePanel({ delivery: d, onChange }: Props) {
  const { t } = useTranslation();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [criteria, setCriteria] = useState("");
  const a = d.acceptance;
  const canReview = d.status === "review_ready" || d.status === "needs_decision";
  if (!a) return null;

  function run(f: () => Promise<FleetDelivery>) {
    setBusy(true);
    setError("");
    f()
      .then(onChange)
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
      .finally(() => setBusy(false));
  }

  const needsEvidence = d.acceptance_items.some((i) => i.decision === "needs_evidence");
  return (
    <section className="rounded-md border border-gray-800 bg-gray-900/40 p-4 space-y-3" data-testid="fleet-acceptance" aria-busy={busy}>
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <h2 className="text-sm font-semibold text-white">{t("fleet.acceptance.title")}</h2>
        {a.structured && (
          <span className="text-xs text-gray-400" data-testid="fleet-acceptance-progress">
            {t("fleet.acceptance.progress", { approved: a.approved, total: a.total })} · {t("fleet.acceptance.version", { n: a.version })}
          </span>
        )}
      </div>

      <div className="grid gap-1 text-xs" data-testid="fleet-acceptance-split">
        <p>
          <span className="text-gray-500">{t("fleet.acceptance.auto")}:</span>{" "}
          <span data-testid="fleet-acceptance-auto">{t(`fleet.delivery.verify_${d.verification}`)}</span>
        </p>
        <p>
          <span className="text-gray-500">{t("fleet.acceptance.manual")}:</span>{" "}
          {a.structured ? t("fleet.acceptance.progress", { approved: a.approved, total: a.total }) : t("fleet.acceptance.none")}
        </p>
      </div>
      {a.verification_skipped && <p className="text-xs text-warning">{t("fleet.acceptance.skipped")}</p>}
      {a.evidence_only && <p className="text-xs text-gray-400">{t("fleet.acceptance.evidence_only")}</p>}
      {a.external_wait && <p className="text-xs text-warning" data-testid="fleet-acceptance-external">{t("fleet.acceptance.external_wait")}</p>}

      {!a.structured && (
        <div className="space-y-2" data-testid="fleet-acceptance-empty">
          <p className="text-sm text-gray-400">{t("fleet.acceptance.none")}</p>
          {d.note && <p className="text-xs text-gray-500">{t("fleet.delivery.legacy")}</p>}
          {canReview && (
            <>
              <p className="text-xs text-gray-500">{t("fleet.acceptance.none_hint")}</p>
              <textarea
                value={criteria}
                onChange={(e) => setCriteria(e.target.value)}
                rows={3}
                placeholder={t("fleet.acceptance.criteria_text")}
                className="w-full rounded border border-gray-700 bg-black/40 p-2 text-xs text-gray-100 focus:border-primary/50 focus:outline-none"
              />
              <button
                type="button"
                disabled={busy || !criteria.trim()}
                className="rounded border border-primary/30 bg-primary/15 px-3 py-1 text-xs text-primary disabled:opacity-50"
                onClick={() =>
                  run(() =>
                    fleetApi.enterAcceptance(d.task_id, {
                      criteria: criteria
                        .split("\n")
                        .map((s) => s.trim())
                        .filter(Boolean)
                        .map((text, i) => ({ id: `item-${i + 1}`, text })),
                      items: [],
                    }),
                  )
                }
              >
                {t("fleet.acceptance.criteria_save")}
              </button>
            </>
          )}
        </div>
      )}

      {a.proposal && (
        <div className="rounded border border-warning/40 p-2 space-y-1 text-xs" data-testid="fleet-acceptance-proposal">
          <p className="font-medium text-warning">{t("fleet.acceptance.update_title", { n: a.proposal.version })}</p>
          {a.proposal.added.map((c) => (
            <p key={`a${c.id}`} className="text-success">+ {t("fleet.acceptance.update_added")} {c.id}: {c.text}</p>
          ))}
          {a.proposal.changed.map((c) => (
            <p key={`c${c.id}`} className="text-warning">
              ~ {t("fleet.acceptance.update_changed")} {c.id}: <s className="text-gray-500">{c.old}</s> → {c.text}
            </p>
          ))}
          {a.proposal.removed.map((c) => (
            <p key={`r${c.id}`} className="text-error">− {t("fleet.acceptance.update_removed")} {c.id}: {c.text}</p>
          ))}
          <div className="flex gap-2 pt-1">
            <button type="button" disabled={busy} className="rounded border border-warning/30 bg-warning/15 px-2 py-0.5 text-warning" onClick={() => run(() => fleetApi.acceptanceCriteria(d.task_id, "apply"))}>
              {t("fleet.acceptance.update_apply")}
            </button>
            <button type="button" disabled={busy} className="rounded border border-gray-600/40 px-2 py-0.5 text-gray-300" onClick={() => run(() => fleetApi.acceptanceCriteria(d.task_id, "dismiss"))}>
              {t("fleet.acceptance.update_dismiss")}
            </button>
          </div>
        </div>
      )}

      {a.rejected_reports.map((r) => (
        <p key={r.run} className="text-xs text-error break-words" data-testid="fleet-acceptance-rejected">
          {t("fleet.acceptance.rejected", { run: r.run, errors: r.errors.join(", ") })}
        </p>
      ))}

      {d.acceptance_items.length > 0 && (
        <ul className="space-y-2" data-testid="fleet-acceptance-items">
          {d.acceptance_items.map((it) => (
            <Item key={`${it.id}-${it.version}`} item={it} taskId={d.task_id} canReview={canReview} run={run} />
          ))}
        </ul>
      )}

      {a.structured && !a.gate_ok && a.gate_code && (
        <p className="text-xs text-gray-400" data-testid="fleet-acceptance-gate">
          {t(`fleet.acceptance.gate_${a.gate_code}`, { items: a.gate_open.join(", ") })}
        </p>
      )}

      <div className="flex flex-wrap gap-2">
        {needsEvidence && canReview && (
          <button type="button" disabled={busy} data-action="request-evidence" className={clsx("rounded border border-primary/30 bg-primary/15 px-3 py-1 text-xs text-primary disabled:opacity-50")} onClick={() => run(() => fleetApi.requestEvidence(d.task_id))}>
            {busy ? t("fleet.acceptance.working") : t("fleet.acceptance.send_back")}
          </button>
        )}
        {a.structured && canReview && (
          <button type="button" disabled={busy} className="rounded border border-gray-600/40 px-3 py-1 text-xs text-gray-300 disabled:opacity-50" onClick={() => run(() => fleetApi.acceptanceCriteria(d.task_id, "refresh"))}>
            {t("fleet.acceptance.refresh")}
          </button>
        )}
      </div>
      {error && <p className="text-xs text-error break-words">{error}</p>}
    </section>
  );
}
