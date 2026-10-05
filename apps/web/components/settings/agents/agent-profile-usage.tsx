"use client";

import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { Badge } from "@kandev/ui/badge";
import { formatNumber, formatRelativeTime } from "@/lib/i18n/formats";
import { useAgentProfileUsage } from "@/hooks/domains/settings/use-agent-profile-usage";
import type {
  AgentProfileUsage,
  AgentProfileUsageWindow,
} from "@/lib/api/domains/agent-profile-usage-api";

const FAILURE_REASON_KEYS: Record<string, string> = {
  credential_missing: "agents:profileUsageReasonCredentialMissing",
  unauthorized: "agents:profileUsageReasonUnauthorized",
  http_status: "agents:profileUsageReasonProviderError",
  network: "agents:profileUsageReasonUnreachable",
  decode: "agents:profileUsageReasonUnexpected",
};

const SCOPE_KEYS: Record<string, string> = {
  free_models: "agents:profileUsageScopeFree",
  premium_models: "agents:profileUsageScopePremium",
};

function failureReasonKey(reason: string | undefined): string {
  return FAILURE_REASON_KEYS[reason ?? ""] ?? "agents:profileUsageReasonReadFailed";
}

/**
 * One profile's provider usage on its settings row. Usage belongs to the
 * profile's account, so it is shown here once per profile rather than repeated
 * in every dynamic profile that routes to it. Unknown and unreadable usage are
 * stated explicitly, never shown as zero.
 */
export function AgentProfileUsageLine({ profileId }: { profileId: string }) {
  const usage = useAgentProfileUsage(profileId);
  if (!usage) return null;
  if (usage.state === "unsupported" && !usage.internal && !usage.limit_hits) return null;
  return <AgentProfileUsageView usage={usage} />;
}

export function AgentProfileUsageView({
  usage,
  now = Date.now(),
}: {
  usage: AgentProfileUsage;
  now?: number;
}) {
  const { t } = useTranslation();
  let content: ReactNode;
  if (usage.state === "unavailable") {
    content = (
      <span>
        {t("agents:profileUsageUnavailable", { reason: t(failureReasonKey(usage.reason)) })}
      </span>
    );
  } else if (usage.state === "no_usage_api") {
    content = <RecordedUsage usage={usage} />;
  } else if (usage.windows.length === 0) {
    content = <span>{t("agents:profileUsageNoWindows")}</span>;
  } else {
    content = usage.windows.map((usageWindow) => (
      <UsageWindowBadge key={usageWindow.label} usageWindow={usageWindow} now={now} />
    ));
  }
  return (
    <div className="mt-1 grid gap-1 pl-3.5 text-xs text-muted-foreground">
      {usage.state === "unsupported" ? null : (
        <div className="flex flex-wrap items-center gap-1.5" data-testid="agent-profile-usage">
          {content}
        </div>
      )}
      {usage.stale && usage.fetched_at ? (
        <span data-testid="agent-profile-usage-stale">
          {t("agents:profileUsageStaleObserved", {
            relative: formatRelativeTime(usage.fetched_at, now),
          })}
        </span>
      ) : null}
      <ObservedUsageSource usage={usage} now={now} />
      <InternalUsage usage={usage} now={now} />
    </div>
  );
}

/**
 * Where a substituted reading came from, and how old it is. A window observed on
 * a running agent's own rate-limit stream is a real reading, so it is named
 * rather than left to look like a live provider reading; the stale wording above
 * says the value is not current but not what produced it.
 */
function ObservedUsageSource({ usage, now }: { usage: AgentProfileUsage; now: number }) {
  const { t } = useTranslation();
  if (!usage.observed || !usage.fetched_at) return null;
  return (
    <span data-testid="agent-profile-usage-observed">
      {t("agents:profileUsageObserved", {
        relative: formatRelativeTime(usage.fetched_at, now),
      })}
    </span>
  );
}

/**
 * Kandev's own accumulation for the account, and where recorded limit hits
 * happened. Both exist even when the provider publishes nothing, which is what
 * lets an undisclosed limit be estimated.
 */
function InternalUsage({ usage, now }: { usage: AgentProfileUsage; now: number }) {
  const { t } = useTranslation();
  const turns = (label: string) =>
    formatNumber(usage.internal?.windows.find((window) => window.label === label)?.turns ?? 0);
  const hits = usage.limit_hits;
  return (
    <>
      {usage.internal ? (
        <span data-testid="agent-profile-usage-internal">
          {t("agents:profileUsageInternal", {
            h5: turns("5h"),
            day: turns("day"),
            week: turns("week"),
          })}
        </span>
      ) : null}
      {hits ? (
        <span data-testid="agent-profile-usage-limit-hits">
          {t("agents:profileUsageLimitHits", {
            hits: formatNumber(hits.count),
            turns: formatNumber(hits.median_turns_day),
            relative: formatRelativeTime(hits.last_at, now),
          })}
        </span>
      ) : null}
    </>
  );
}

function UsageWindowBadge({
  usageWindow,
  now,
}: {
  usageWindow: AgentProfileUsageWindow;
  now: number;
}) {
  const { t } = useTranslation();
  const scopeKey = usageWindow.scope ? SCOPE_KEYS[usageWindow.scope] : undefined;
  const reset = usageWindow.reset_at ? formatRelativeTime(usageWindow.reset_at, now) : "";
  const parts = [
    t("agents:profileUsageWindow", {
      label: usageWindow.label,
      percent: formatNumber(usageWindow.utilization_pct / 100, {
        style: "percent",
        maximumFractionDigits: 0,
      }),
    }),
    scopeKey ? t(scopeKey) : "",
    usageWindow.limit_reached ? t("agents:profileUsageLimitReached") : "",
    reset ? t("agents:profileUsageResets", { relative: reset }) : "",
  ].filter(Boolean);
  return (
    <Badge
      variant={usageWindow.limit_reached ? "destructive" : "outline"}
      className="h-auto min-h-5 flex-wrap gap-x-1.5 whitespace-normal text-left font-normal"
      data-testid="agent-profile-usage-window"
    >
      {parts.map((part) => (
        <span key={part}>{part}</span>
      ))}
    </Badge>
  );
}

function RecordedUsage({ usage }: { usage: AgentProfileUsage }) {
  const { t } = useTranslation();
  if (!usage.recorded) return <span>{t("agents:profileUsageNoApi")}</span>;
  return (
    <span>
      {t("agents:profileUsageRecordedToday", {
        turns: formatNumber(usage.recorded.turns),
        tokens: formatNumber(usage.recorded.tokens_total),
      })}
    </span>
  );
}
