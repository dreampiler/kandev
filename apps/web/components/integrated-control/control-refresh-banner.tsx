import { useTranslation } from "react-i18next";
import { useIntegratedControl } from "./integrated-control-provider";
import { formatControlClock } from "./control-refresh-clock";

type ControlData = NonNullable<NonNullable<ReturnType<typeof useIntegratedControl>>["data"]>;

const FAILURE_KEYS = {
  unavailable: "sidebar:controlRefresh.bannerUnavailable",
  disconnected: "sidebar:controlRefresh.bannerDisconnected",
  error: "sidebar:controlRefresh.bannerError",
  denied: "sidebar:integratedControl.readDenied",
  unsupported: "sidebar:controlRefresh.bannerUnsupported",
} as const;

/** Whether any workspace's coordinator, conversation or task list failed to read. */
export function hasPartialControlData(data: ControlData) {
  return (
    Object.values(data.byWorkspace).some((list) => list.error) ||
    Object.values(data.conversationsByWorkspace).some(
      (list) => list.independent.error || list.tasks?.error,
    )
  );
}

/**
 * The one place a control screen reports a failed read. The last values stay
 * on screen underneath; the banner says when they were read and when the next
 * automatic retry runs.
 */
export function ControlRefreshBanner() {
  const { t } = useTranslation();
  const control = useIntegratedControl();
  const failure = control?.failure;
  const data = control?.data;
  if (!failure) {
    if (!data || !hasPartialControlData(data)) return null;
    return (
      <p
        role="status"
        className="shrink-0 border-b bg-amber-500/10 px-4 py-2 text-xs text-amber-800 dark:text-amber-200"
        data-testid="control-refresh-banner"
      >
        {t("sidebar:integratedControl.partialData")}
      </p>
    );
  }
  const details: string[] = [];
  if (data?.receivedAt)
    details.push(
      t("sidebar:controlRefresh.bannerValuesAt", { time: formatControlClock(data.receivedAt) }),
    );
  if (failure.retryAt)
    details.push(
      t("sidebar:controlRefresh.bannerRetryAt", { time: formatControlClock(failure.retryAt) }),
    );
  // Only the failure kind is live: a new retry time is not re-announced every backoff step.
  return (
    <p
      className="shrink-0 border-b bg-amber-500/15 px-4 py-2 text-xs font-medium text-amber-800 tabular-nums dark:text-amber-200"
      data-testid="control-refresh-banner"
      data-kind={failure.kind}
    >
      <span role="status">{t(FAILURE_KEYS[failure.kind])}</span>
      {details.map((detail) => (
        <span key={detail}>
          {" · "}
          {detail}
        </span>
      ))}
    </p>
  );
}
