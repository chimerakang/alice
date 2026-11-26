import { useTranslation } from "react-i18next";
import type { FleetTask } from "@/types/fleet";

/** Says who chose a task's model: the owner, or the commander and why. */
export default function ModelChoice({ task }: { task: FleetTask }) {
  const { t } = useTranslation();
  if (!task.model || !task.model_source) return null;
  if (task.model_source === "manual") return <>{t("fleet.model_choice.manual")}</>;
  const reason = task.model_reason
    ? t(`fleet.model_choice.reason.${task.model_reason}`, { detail: task.model_reason_detail ?? "" })
    : "";
  return (
    <>
      {t("fleet.model_choice.commander", {
        tier: t(`fleet.model_choice.tier.${task.model_tier ?? "smart"}`, { defaultValue: task.model_tier ?? "" }),
        reason,
      })}
    </>
  );
}
