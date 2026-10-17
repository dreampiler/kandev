"use client";

import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@kandev/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@kandev/ui/card";
import { useFeature } from "@/hooks/domains/features/use-feature";
import {
  readProfileExecutionUsage,
  type ProfileExecutionUsage,
} from "@/lib/api/domains/provider-usage-api";

export function ProviderUsageCard({ profileId }: { profileId: string }) {
  const enabled = useFeature("providerUsageLimits");
  return enabled ? <EnabledProviderUsageCard key={profileId} profileId={profileId} /> : null;
}

function EnabledProviderUsageCard({ profileId }: { profileId: string }) {
  const { t, i18n } = useTranslation("agents");
  const [entries, setEntries] = useState<ProfileExecutionUsage[] | null>(null);
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState(false);
  const unknown = t("nativeUsage.unknown");
  const date = (value?: string) => {
    const time = value ? new Date(value) : null;
    return time && Number.isFinite(time.getTime())
      ? time.toLocaleString(i18n.resolvedLanguage)
      : unknown;
  };
  const refresh = async () => {
    setBusy(true);
    setFailed(false);
    try {
      const result = await readProfileExecutionUsage(profileId);
      setEntries(result.executions);
    } catch {
      setEntries(null);
      setFailed(true);
    } finally {
      setBusy(false);
    }
  };
  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between gap-3">
        <CardTitle>{t("nativeUsage.title")}</CardTitle>
        <Button type="button" variant="outline" disabled={busy} onClick={() => void refresh()}>
          {t("nativeUsage.refresh")}
        </Button>
      </CardHeader>
      <CardContent className="space-y-4" aria-busy={busy}>
        <p className="text-sm text-muted-foreground">{t("nativeUsage.description")}</p>
        {failed && <p role="alert">{t("nativeUsage.failed")}</p>}
        {!entries?.length && !failed && <p className="text-sm">{unknown}</p>}
        {entries?.map(({ execution_id, report }) => (
          <div key={execution_id} className="space-y-2 rounded-md border p-3">
            <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">
              <dt>{t("nativeUsage.account")}</dt>
              <dd className="break-all">{report.current?.account_ref ?? unknown}</dd>
              <dt>{t("nativeUsage.organization")}</dt>
              <dd className="break-all">{report.current?.organization_ref ?? unknown}</dd>
              <dt>{t("nativeUsage.model")}</dt>
              <dd>{report.model || unknown}</dd>
              <dt>{t("nativeUsage.observedAt")}</dt>
              <dd>{date(report.current?.fetched_at)}</dd>
            </dl>
            {!report.source_known && <p className="text-sm">{unknown}</p>}
            {report.binding_changed && <p role="alert">{t("nativeUsage.bindingChanged")}</p>}
            {report.limited && (
              <p role="status" className="text-sm">
                {t("nativeUsage.limited")} · {report.blocking_window} · {t("nativeUsage.reset")}:{" "}
                {date(report.reset_at)}
              </p>
            )}
            {report.current?.windows.map((window, index) => (
              <div key={`${window.label}:${window.model ?? ""}:${index}`} className="text-sm">
                {window.label}:{" "}
                {window.utilization_observed ? `${window.utilization_pct}%` : unknown}
                {" · "}
                {t("nativeUsage.reset")}:{" "}
                {window.reset_at_observed ? date(window.reset_at) : unknown}
              </div>
            ))}
          </div>
        ))}
      </CardContent>
    </Card>
  );
}
