import { useTranslation } from "react-i18next";
import type { ControlRead } from "@/hooks/domains/coordinator/use-control-resource";
import { formatDateTime } from "@/lib/i18n/formats";
import { useIntegratedControl } from "./integrated-control-provider";

const STATUS_KEYS = {
  loading: "common:loading",
  denied: "sidebar:integratedControl.readDenied",
  unsupported: "sidebar:controlRestore.contractUnavailable",
  error: "sidebar:integratedControl.loadError",
};

/**
 * A read's state. A plain successful read says nothing unless `showTime` asks
 * for its time: the page header carries the one read time a screen needs, so
 * cards stay quiet until a read is loading, failing or retained after a failure.
 */
export function ControlReadState({
  status,
  receivedAt,
  refreshing,
  stale,
  showTime = false,
}: ControlRead<unknown> & { showTime?: boolean }) {
  const { t } = useTranslation();
  if (status === "ready" && !stale && !showTime) return null;
  if (status === "ready" && !receivedAt && !refreshing) return null;
  const key = readStatusKey({ status, refreshing, stale, receivedAt });
  const text = `${t(key)}${receivedAt ? ` · ${formatDateTime(receivedAt)} (${Intl.DateTimeFormat().resolvedOptions().timeZone})` : ""}`;
  return (
    <p
      role={status === "error" || status === "denied" ? "alert" : "status"}
      className="h-8 min-w-0 truncate px-3 py-2 text-xs text-muted-foreground"
      title={text}
    >
      {text}
    </p>
  );
}

function readStatusKey({ status, refreshing, stale, receivedAt }: ControlRead<unknown>) {
  if (refreshing) return receivedAt ? "sidebar:controlRefresh.refreshing" : "common:loading";
  if (stale && receivedAt) return "sidebar:controlRefresh.retainedError";
  if (status === "ready") return "sidebar:controlRefresh.readAt";
  return STATUS_KEYS[status];
}

export function ControlSnapshotReadState() {
  const control = useIntegratedControl();
  let status: ControlRead<unknown>["status"] = "loading";
  if (control?.data) status = "ready";
  if (control?.error) status = "error";
  return (
    <ControlReadState
      status={status}
      refreshing={control?.loading}
      stale={control?.error && Boolean(control.data)}
      receivedAt={control?.data?.receivedAt}
      showTime
    />
  );
}
