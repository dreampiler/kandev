import { useTranslation } from "react-i18next";
import type { OverviewProjectionStatus } from "@/lib/types/control-overview";

export function ControlProjectionNotice({ status }: { status?: OverviewProjectionStatus }) {
  const { t } = useTranslation();
  if (!status || status.state === "known") return null;
  return (
    <p
      role="status"
      className="rounded-md border p-3 text-sm text-muted-foreground"
      data-testid="control-projection-notice"
    >
      {t("sidebar:controlRestore.partialMeasurements", {
        reasons: status.reasons.join(", ") || t("common:unknown"),
      })}
    </p>
  );
}
