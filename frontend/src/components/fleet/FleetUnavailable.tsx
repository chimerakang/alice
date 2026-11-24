import { useTranslation } from "react-i18next";
import { PowerOff, TriangleAlert } from "lucide-react";
import type { FleetUnavailable as Reason } from "@/stores/fleetStore";

/** Shown in place of a fleet page when the fleet's data cannot be had. */
export default function FleetUnavailable({ reason, error }: { reason: Reason; error: string }) {
  const { t } = useTranslation();
  const disabled = reason === "disabled";
  const Icon = disabled ? PowerOff : TriangleAlert;
  return (
    <div className="border border-gray-800 rounded-lg p-8 text-center space-y-3">
      <Icon className={`w-8 h-8 mx-auto ${disabled ? "text-gray-500" : "text-error"}`} />
      <h2 className="text-lg font-semibold text-white">
        {disabled ? t("fleet.unavailable.disabled_title") : t("fleet.unavailable.error_title")}
      </h2>
      <p className="text-sm text-gray-400 max-w-xl mx-auto">
        {disabled ? t("fleet.unavailable.disabled_body") : error}
      </p>
    </div>
  );
}
