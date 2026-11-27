import { useState } from "react";
import { clsx } from "clsx";
import { useTranslation } from "react-i18next";
import ConfirmDialog from "@/components/ConfirmDialog";
import { FleetApiError, fleetApi, getApiToken, setApiToken } from "@/lib/fleetApi";
import { isDestructive, taskActions } from "@/lib/fleet";
import { useFleetStore } from "@/stores/fleetStore";
import type { FleetTask, FleetTaskAction } from "@/types/fleet";

const tone: Record<FleetTaskAction, string> = {
  finish_evidence: "bg-success/15 text-success border-success/30 hover:bg-success/25",
  approve: "bg-success/15 text-success border-success/30 hover:bg-success/25",
  merge: "bg-success/15 text-success border-success/30 hover:bg-success/25",
  pass: "bg-warning/15 text-warning border-warning/30 hover:bg-warning/25",
  retry: "bg-primary/15 text-primary border-primary/30 hover:bg-primary/25",
  reply: "bg-primary/15 text-primary border-primary/30 hover:bg-primary/25",
  cancel: "bg-gray-700/40 text-gray-300 border-gray-600/40 hover:bg-gray-700/70",
  discard: "bg-error/10 text-error border-error/30 hover:bg-error/20",
};

interface Props {
  task: FleetTask;
  /** Called with the task as the server returned it after a command. */
  onDone?: (task: FleetTask) => void;
  compact?: boolean;
}

/**
 * The commands a task's status allows. Replying takes text; cancelling and
 * discarding ask first. A refusal is shown where it happened, and a missing
 * or wrong API token is asked for on the spot.
 */
export default function FleetActions({ task, onDone, compact = false }: Props) {
  const { t } = useTranslation();
  const putTask = useFleetStore((s) => s.putTask);
  const [busy, setBusy] = useState<FleetTaskAction | null>(null);
  const [error, setError] = useState("");
  const [confirm, setConfirm] = useState<FleetTaskAction | null>(null);
  const [replying, setReplying] = useState(false);
  const [text, setText] = useState("");
  const [needToken, setNeedToken] = useState(false);
  const [token, setToken] = useState(getApiToken());

  const actions = taskActions(task);
  if (actions.length === 0) return null;

  async function run(action: FleetTaskAction, body?: { text?: string }) {
    setBusy(action);
    setError("");
    try {
      const updated = await fleetApi.act(task.id, action, body);
      putTask(updated);
      setReplying(false);
      setText("");
      setNeedToken(false);
      onDone?.(updated);
    } catch (err) {
      if (err instanceof FleetApiError && err.unauthorized) setNeedToken(true);
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(null);
    }
  }

  function press(action: FleetTaskAction) {
    if (action === "reply") {
      setReplying((v) => !v);
      return;
    }
    if (isDestructive(action)) {
      setConfirm(action);
      return;
    }
    void run(action);
  }

  return (
    <div className="space-y-2" data-testid="fleet-actions">
      <div className="flex flex-wrap gap-2">
        {actions.map((action) => (
          <button
            key={action}
            type="button"
            disabled={busy !== null}
            onClick={() => press(action)}
            className={clsx(
              "rounded-md border font-medium transition-colors disabled:opacity-50",
              compact ? "px-2.5 py-1 text-xs" : "px-3 py-1.5 text-sm",
              tone[action],
            )}
            data-action={action}
          >
            {busy === action ? t("fleet.actions.working") : t(`fleet.actions.${action}`)}
          </button>
        ))}
      </div>

      {task.status === "needs_decision" && task.session_id && (
        <p className="text-xs text-gray-400" data-testid="fleet-retry-vs-reply">
          {t("fleet.actions.retry_vs_reply_hint")}
        </p>
      )}

      {replying && (
        <div className="space-y-2">
          <textarea
            value={text}
            onChange={(e) => setText(e.target.value)}
            rows={3}
            placeholder={t(`fleet.actions.reply_placeholder_${task.status === "review_ready" ? "review" : "blocked"}`)}
            className="w-full rounded-md border border-gray-700 bg-black/40 p-2 text-sm text-gray-100 placeholder:text-gray-600 focus:border-primary/50 focus:outline-none"
          />
          <button
            type="button"
            disabled={busy !== null}
            onClick={() => void run("reply", { text: text.trim() })}
            className={clsx("rounded-md border px-3 py-1.5 text-sm font-medium disabled:opacity-50", tone.reply)}
          >
            {text.trim() ? t("fleet.actions.send_reply") : t("fleet.actions.carry_on")}
          </button>
        </div>
      )}

      {needToken && (
        <form
          className="flex flex-wrap items-center gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            setApiToken(token.trim());
            setNeedToken(false);
            setError("");
          }}
        >
          <input
            type="password"
            value={token}
            onChange={(e) => setToken(e.target.value)}
            placeholder={t("fleet.token.placeholder")}
            className="flex-1 min-w-[12rem] rounded-md border border-gray-700 bg-black/40 px-2 py-1.5 text-sm text-gray-100 focus:border-primary/50 focus:outline-none"
          />
          <button type="submit" className={clsx("rounded-md border px-3 py-1.5 text-sm", tone.reply)}>
            {t("fleet.token.save")}
          </button>
          <p className="w-full text-xs text-gray-500">{t("fleet.token.hint")}</p>
        </form>
      )}

      {error && !needToken && <p className="text-xs text-error break-words">{error}</p>}

      <ConfirmDialog
        open={confirm !== null}
        title={confirm ? t(`fleet.confirm.${confirm}_title`) : ""}
        message={confirm ? t(`fleet.confirm.${confirm}_body`) : ""}
        confirmLabel={confirm ? t(`fleet.actions.${confirm}`) : ""}
        cancelLabel={t("fleet.confirm.keep")}
        variant="danger"
        onCancel={() => setConfirm(null)}
        onConfirm={() => {
          const action = confirm;
          setConfirm(null);
          if (action) void run(action);
        }}
      />
    </div>
  );
}
