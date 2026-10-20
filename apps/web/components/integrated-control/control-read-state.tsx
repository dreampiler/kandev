import { useTranslation } from "react-i18next";
import type { ControlRead } from "@/hooks/domains/coordinator/use-control-resource";
import { useIntegratedControl } from "./integrated-control-provider";
import { formatControlClock } from "./control-refresh-clock";

const STATUS_KEYS = {
  loading: "common:loading",
  denied: "sidebar:integratedControl.readDenied",
  unsupported: "sidebar:controlRestore.contractUnavailable",
  error: "sidebar:integratedControl.loadError",
};

/**
 * A card read's problem, if any. A successful or refreshing read says
 * nothing: the page header carries the one read time a screen needs. While
 * the page banner reports a failed snapshot read, failed cards stay quiet and
 * keep their last values instead of repeating the same failure.
 */
export function ControlReadState({ status, receivedAt, stale }: ControlRead<unknown>) {
  const { t } = useTranslation();
  const bannerShown = Boolean(useIntegratedControl()?.failure);
  if (status === "ready" && !stale) return null;
  if (bannerShown && (status === "error" || stale)) return null;
  const retained = stale && receivedAt;
  const text = retained
    ? t("sidebar:controlRefresh.retainedAt", { time: formatControlClock(receivedAt) })
    : t(STATUS_KEYS[status === "ready" ? "error" : status]);
  return (
    <p
      role={status === "error" || status === "denied" ? "alert" : "status"}
      className="min-w-0 px-3 py-2 text-xs text-muted-foreground tabular-nums"
    >
      {text}
    </p>
  );
}

/** The header's read time: fixed-width digits, never replaced by a refreshing label. */
export function ControlLastRefresh() {
  const { t } = useTranslation();
  const receivedAt = useIntegratedControl()?.data?.receivedAt;
  if (!receivedAt) return null;
  return (
    <span
      className="shrink-0 whitespace-nowrap font-mono text-xs tabular-nums text-muted-foreground"
      data-testid="control-last-refresh"
    >
      {t("sidebar:controlRefresh.lastRefresh", { time: formatControlClock(receivedAt) })}
    </span>
  );
}
