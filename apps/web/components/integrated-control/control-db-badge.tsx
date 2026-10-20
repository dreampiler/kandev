import { useTranslation } from "react-i18next";
import { cn } from "@/lib/utils";
import { useIntegratedControl } from "./integrated-control-provider";
import { toneSurfaceClass } from "./control-status-pill";

/**
 * The required-store reading in the control header: a quiet badge while the
 * store is healthy and the warning tone otherwise. A missing or failed read
 * draws nothing, so a failed diagnostics call is not reported as a DB fault.
 */
export function ControlDbBadge() {
  const { t } = useTranslation();
  const diagnostics = useIntegratedControl()?.data?.persistence;
  if (!diagnostics) return null;
  const healthy = diagnostics.state === "healthy";
  return (
    <span
      className={cn(
        "inline-flex shrink-0 items-center whitespace-nowrap rounded border px-1.5 py-0.5 text-[11px] font-medium",
        toneSurfaceClass(healthy ? "idle" : "delayed"),
      )}
      data-testid="control-db-badge"
      data-state={diagnostics.state}
    >
      {t(healthy ? "sidebar:controlSummary.dbHealthy" : "sidebar:controlSummary.dbUnhealthy")}
    </span>
  );
}
