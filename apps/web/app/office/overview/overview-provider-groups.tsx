"use client";

import { useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import type {
  OverviewAccountBlock,
  OverviewAccountGroup,
  OverviewAccountUsage,
} from "./overview-accounts";
import { accountKindKey, blockSubjectText } from "./overview-accounts";
import type { OverviewModel } from "@/lib/state/slices/office/overview-types";
import {
  internalUsageEntry,
  occurredTime,
  relativeTime,
  usageUnavailableReason,
  usageWindowLabel,
} from "./overview-format";
import { statusTextClass } from "./overview-status-colors";

/**
 * One provider account as a single readable line: which account it is, what its
 * windows are used for, whether it is blocked right now, and when usage resets.
 * The models that run on it stay one disclosure away rather than being replaced
 * by the summary.
 */
export function ProviderAccountGroup({
  group,
  renderModel,
}: {
  group: OverviewAccountGroup;
  renderModel: (model: OverviewModel) => ReactNode;
}) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const label = t(accountKindKey(group.kind));
  return (
    <div className="rounded-md border border-border p-3 text-sm" data-testid="overview-account">
      <button
        type="button"
        aria-expanded={open}
        onClick={() => setOpen((value) => !value)}
        className="flex min-h-11 w-full cursor-pointer flex-wrap items-baseline gap-x-3 text-left"
      >
        <span className="font-medium">{label}</span>
        <span className="text-xs text-muted-foreground">
          {t("office:overviewAccountModels", { count: group.models.length })}
        </span>
      </button>
      <div className="mt-1 space-y-0.5 text-xs">
        <AccountUsageLine usage={group.usage} />
        <AccountBlockLine block={group.block} />
      </div>
      {open ? (
        <div className="mt-2 grid gap-2 md:grid-cols-2 xl:grid-cols-3">
          {group.models.map(renderModel)}
        </div>
      ) : null}
      {!group.accountId ? (
        <p className="mt-1 text-xs text-muted-foreground">
          {t("office:overviewAccountUnknownReason")}
        </p>
      ) : null}
    </div>
  );
}

/**
 * The account's windows with their reset instants. A read that did not answer, or
 * an account whose provider publishes no usage API, is named as such rather than
 * shown as unused.
 */
function AccountUsageLine({ usage }: { usage: OverviewAccountUsage }) {
  const { t } = useTranslation();
  if (usage.pending) {
    return (
      <div className="text-muted-foreground" data-testid="overview-account-usage-pending">
        {t("office:overviewUsagePending")}
      </div>
    );
  }
  if (usage.unavailableReason) {
    return (
      <div className="text-muted-foreground" data-testid="overview-account-usage-unavailable">
        {t("office:overviewUsageUnavailable", {
          reason: usageUnavailableReason(t, usage.unavailableReason),
        })}
      </div>
    );
  }
  if (usage.windows.length === 0) {
    return (
      <div className="text-muted-foreground" data-testid="overview-account-usage-none">
        {t("office:overviewAccountNoWindows")}
      </div>
    );
  }
  return (
    <div className="space-y-0.5" data-testid="overview-account-usage">
      {usage.windows.map((window) => (
        <div key={`${window.label}:${window.reset_at ?? ""}`} className="flex flex-wrap gap-x-2">
          <span>{usageWindowLabel(t, window.label)}</span>
          <span className="tabular-nums">{Math.round(window.utilization_pct)}%</span>
          {window.limit_reached ? (
            <span className={statusTextClass("error")}>
              {t("office:overviewUsageLimitReached")}
            </span>
          ) : null}
          {window.reset_at ? (
            <span className="text-muted-foreground" title={relativeTime(window.reset_at)}>
              {t("office:overviewUsageResets", { time: occurredTime(window.reset_at) })}
            </span>
          ) : (
            <span className="text-muted-foreground">{t("office:overviewAccountNoReset")}</span>
          )}
        </div>
      ))}
      {usage.observed ? (
        <div className="text-muted-foreground">{t("office:overviewUsageObservedSource")}</div>
      ) : null}
      {usage.stale ? (
        <div className="text-muted-foreground">{t("office:overviewUsageStaleSource")}</div>
      ) : null}
      {usage.internal && usage.internal.length > 0 ? (
        <div className="text-muted-foreground" data-testid="overview-account-internal">
          {t("office:overviewAccountInternalUsage", {
            windows: usage.internal.map((entry) => internalUsageEntry(t, entry)).join(" · "),
          })}
        </div>
      ) : null}
      {usage.limitHitCount ? (
        <div className="text-muted-foreground" data-testid="overview-account-limit-hits">
          {t("office:overviewAccountLimitHits", { count: usage.limitHitCount })}
        </div>
      ) : null}
    </div>
  );
}

/** Whether the account is blocked now, what is blocked, and when it clears. */
function AccountBlockLine({ block }: { block: OverviewAccountBlock }) {
  const { t } = useTranslation();
  if (block.state === "clear") {
    return (
      <div className="text-muted-foreground" data-testid="overview-account-clear">
        {t("office:overviewAccountNotBlocked")}
      </div>
    );
  }
  const until = block.until ? (
    <span className="text-muted-foreground" title={relativeTime(block.until)}>
      {t("office:overviewClearsAt", { time: occurredTime(block.until) })}
    </span>
  ) : (
    <span className="text-muted-foreground">{t("office:overviewBlockedNoClearTime")}</span>
  );
  return (
    <div
      className={`flex flex-wrap gap-x-2 ${statusTextClass("error")}`}
      data-testid="overview-account-blocked"
    >
      <span>
        {block.state === "account"
          ? t("office:overviewAccountBlocked")
          : t("office:overviewAccountModelsBlocked")}
      </span>
      <span className="text-muted-foreground">
        {block.subjects.map((subject) => blockSubjectText(t, subject)).join(" · ")}
      </span>
      {until}
    </div>
  );
}
