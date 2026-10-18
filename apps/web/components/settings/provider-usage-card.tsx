"use client";

import { useTranslation } from "react-i18next";
import { Button } from "@kandev/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@kandev/ui/card";
import Link from "@/components/routing/app-link";
import { useFeature } from "@/hooks/domains/features/use-feature";
import { useProfileExecutionUsage } from "@/hooks/domains/settings/use-profile-execution-usage";
import type { ProfileExecutionUsage } from "@/lib/api/domains/provider-usage-api";

type UsageProps = {
  profileId: string;
  autoRead?: boolean;
  hideAccount?: boolean;
  sessionLinks?: Record<string, string>;
};

export function ProviderUsageCard(props: UsageProps) {
  const enabled = useFeature("providerUsageLimits");
  return enabled ? <EnabledProviderUsageCard key={props.profileId} {...props} /> : null;
}

function EnabledProviderUsageCard({
  profileId,
  autoRead = false,
  hideAccount,
  sessionLinks,
}: UsageProps) {
  const { t } = useTranslation();
  const { entries, busy, failed, refresh } = useProfileExecutionUsage(profileId, autoRead);
  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between gap-3">
        <CardTitle>{t("agents:nativeUsage.title")}</CardTitle>
        <Button type="button" variant="outline" disabled={busy} onClick={() => void refresh()}>
          {t("sidebar:controlInspection.readStoredUsage")}
        </Button>
      </CardHeader>
      <CardContent className="space-y-4" aria-busy={busy}>
        <p className="text-sm text-muted-foreground">{t("agents:nativeUsage.description")}</p>
        <p role="status" className="text-sm">
          {usageStatus({ busy, failed, entries }, t)}
        </p>
        {entries?.map((entry) => (
          <ExecutionUsageDetails
            key={entry.execution_id}
            entry={entry}
            hideAccount={hideAccount}
            sessionHref={sessionLinks?.[entry.session_id]}
          />
        ))}
      </CardContent>
    </Card>
  );
}

function ExecutionUsageDetails({
  entry,
  hideAccount,
  sessionHref,
}: {
  entry: ProfileExecutionUsage;
  hideAccount?: boolean;
  sessionHref?: string;
}) {
  const { t, i18n } = useTranslation();
  const { report, execution_id, session_id } = entry;
  const unknown = t("agents:nativeUsage.unknown");
  const date = (value?: string) => {
    const time = value ? new Date(value) : null;
    return time && Number.isFinite(time.getTime())
      ? time.toLocaleString(i18n.resolvedLanguage)
      : unknown;
  };
  return (
    <div className="space-y-2 rounded-md border p-3">
      <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-1 text-sm">
        {!hideAccount && (
          <>
            <dt>{t("agents:nativeUsage.account")}</dt>
            <dd className="break-all">{report.current?.account_ref ?? unknown}</dd>
            <dt>{t("agents:nativeUsage.organization")}</dt>
            <dd className="break-all">{report.current?.organization_ref ?? unknown}</dd>
          </>
        )}
        <dt>{t("agents:nativeUsage.model")}</dt>
        <dd className="break-all">{report.model || unknown}</dd>
        <dt>{t("agents:nativeUsage.observedAt")}</dt>
        <dd>{date(report.current?.fetched_at)}</dd>
        <dt>{t("sidebar:controlInspection.executionSession")}</dt>
        <dd className="break-all">
          {sessionHref ? (
            <Link className="underline" href={sessionHref}>
              {session_id}
            </Link>
          ) : (
            session_id || unknown
          )}
        </dd>
        <dt>{t("sidebar:controlInspection.execution")}</dt>
        <dd className="break-all">{execution_id}</dd>
      </dl>
      {!report.source_known && <p className="text-sm">{t(usageReasonKey(report.reason))}</p>}
      {report.binding_changed && <p role="alert">{t("agents:nativeUsage.bindingChanged")}</p>}
      {report.limited && (
        <p role="status" className="text-sm">
          {t("agents:nativeUsage.limited")} · {report.blocking_window} ·{" "}
          {t("agents:nativeUsage.reset")}: {date(report.reset_at)}
        </p>
      )}
      {report.current?.windows.map((window, index) => (
        <div key={`${window.label}:${window.model ?? ""}:${index}`} className="text-sm">
          {window.label}:{" "}
          {report.source_known &&
          window.utilization_observed &&
          Number.isFinite(window.utilization_pct)
            ? `${window.utilization_pct}%`
            : unknown}
          {" · "}
          {t("agents:nativeUsage.reset")}:{" "}
          {report.source_known && window.reset_at_observed ? date(window.reset_at) : unknown}
        </div>
      ))}
    </div>
  );
}

function usageStatus(
  { busy, failed, entries }: { busy: boolean; failed: boolean; entries?: ProfileExecutionUsage[] },
  t: (key: string) => string,
) {
  if (busy) return t("common:loading");
  if (failed) return t("agents:nativeUsage.failed");
  if (entries?.length === 0) return t("sidebar:controlInspection.noUsage");
  return !entries ? t("agents:nativeUsage.unknown") : null;
}

function usageReasonKey(reason: string): string {
  switch (reason) {
    case "execution_unavailable":
    case "execution_unknown":
      return "sidebar:controlInspection.executionUnavailable";
    case "unsupported_executor":
    case "unsupported_agent":
      return "sidebar:controlInspection.usageUnsupported";
    case "credential_source_unknown":
    case "account_unknown":
    case "account_unavailable":
      return "sidebar:controlInspection.accountUnknown";
    default:
      return "sidebar:controlInspection.usageUnknown";
  }
}
