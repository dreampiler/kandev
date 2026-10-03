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
  if (!usage || usage.state === "unsupported") return null;
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
    <div
      className="mt-1 flex flex-wrap items-center gap-1.5 pl-3.5 text-xs text-muted-foreground"
      data-testid="agent-profile-usage"
    >
      {content}
    </div>
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
