"use client";

import Link from "@/components/routing/app-link";
import { useState } from "react";
import type { TFunction } from "i18next";
import { useTranslation } from "react-i18next";
import type { AgentProfileUsage } from "@/lib/api/domains/agent-profile-usage-api";
import {
  useAgentProfileUsage,
  useAgentProfileUsageList,
} from "@/hooks/domains/settings/use-agent-profile-usage";
import { profileUsageFailureReasonKey } from "@/components/settings/agents/agent-profile-usage";
import type {
  OverviewBlockedAccount,
  OverviewBlockedCircuit,
  OverviewModel,
} from "@/lib/state/slices/office/overview-types";
import { groupModelsByAccount } from "./overview-accounts";
import { ProviderAccountGroup } from "./overview-provider-groups";
import {
  circuitReason,
  circuitScope,
  circuitTitle,
  durationFromMinutes,
  isCurrentBlock,
  occurredTime,
  relativeTime,
} from "./overview-format";
import { statusTextClass } from "./overview-status-colors";
import { MODELS_ANCHOR } from "./overview-system-cards";
import { SectionCard, SectionCardEmpty } from "./overview-section-card";

function profileHref(model: OverviewModel): string | undefined {
  if (!model.agent_name) return undefined;
  return `/settings/agents/${encodeURIComponent(model.agent_name)}/profiles/${encodeURIComponent(model.agent_profile_id)}`;
}

/**
 * Agent profiles with session counts, error kinds, and usage, split so a
 * dynamic profile (which routes through ordered concrete profiles) is never
 * read as one model, plus every account or resource the router is avoiding.
 *
 * Concrete models are grouped by the provider account they run on, because
 * usage windows, block state and reset times belong to that account rather than
 * to any one model. Every model card is still there inside its group.
 */
export function OverviewModels({
  models,
  blocked,
  circuits,
  circuitsKnown,
}: {
  models: OverviewModel[];
  blocked: OverviewBlockedAccount[];
  circuits: OverviewBlockedCircuit[];
  circuitsKnown: boolean;
}) {
  const { t } = useTranslation();
  const { byProfile } = useAgentProfileUsageList();
  const dynamic = models.filter((model) => model.kind === "dynamic");
  const accounts = groupModelsByAccount(models, byProfile, circuits);
  const nothingBlocked = blocked.length === 0 && circuits.length === 0 && circuitsKnown;
  return (
    <SectionCard id={MODELS_ANCHOR} title={t("office:overviewModels")}>
      {models.length === 0 && nothingBlocked ? (
        <SectionCardEmpty />
      ) : (
        <div className="space-y-3 p-3">
          {dynamic.length > 0 ? (
            <div className="space-y-2">
              <h3 className="text-xs font-medium text-muted-foreground">
                {t("office:overviewDynamicProfiles")}
              </h3>
              <div className="grid gap-2 md:grid-cols-2 xl:grid-cols-3">
                {dynamic.map((model) => (
                  <ModelCard key={model.agent_profile_id} model={model} />
                ))}
              </div>
            </div>
          ) : null}
          {accounts.map((group) => (
            <ProviderAccountGroup
              key={group.accountId || "unknown"}
              group={group}
              renderModel={(model) => <ModelCard key={model.agent_profile_id} model={model} />}
            />
          ))}
          <div className="space-y-2">
            <h3 className="text-xs font-medium text-muted-foreground">
              {t("office:overviewBlockedModels")}
            </h3>
            <BlockedModelGrid blocked={blocked} circuits={circuits} circuitsKnown={circuitsKnown} />
          </div>
        </div>
      )}
    </SectionCard>
  );
}

function BlockedModelGrid({
  blocked,
  circuits,
  circuitsKnown,
}: {
  blocked: OverviewBlockedAccount[];
  circuits: OverviewBlockedCircuit[];
  circuitsKnown: boolean;
}) {
  const { t } = useTranslation();
  const current = circuits.filter(isCurrentBlock);
  const cleared = circuits.filter((circuit) => !isCurrentBlock(circuit));
  if (!circuitsKnown) {
    return (
      <p className="text-xs text-muted-foreground" data-testid="overview-circuits-unknown">
        {t("office:overviewCircuitsUnknown")}
      </p>
    );
  }
  if (blocked.length === 0 && current.length === 0 && cleared.length === 0) {
    return <p className="text-xs text-muted-foreground">{t("office:overviewNoBlockedModels")}</p>;
  }
  return (
    <div className="space-y-3">
      {blocked.length === 0 && current.length === 0 ? null : (
        <div className="grid gap-2 md:grid-cols-2 xl:grid-cols-3">
          {blocked.map((account) => (
            <BlockedAccountCard
              key={`${account.workspace_id}:${account.provider_id}:${account.scope}:${account.scope_value}`}
              account={account}
            />
          ))}
          {current.map((circuit) => (
            <BlockedCircuitCard key={circuit.resource_key} circuit={circuit} />
          ))}
        </div>
      )}
      {cleared.length > 0 ? <ClearedCircuitList circuits={cleared} /> : null}
    </div>
  );
}

/**
 * Circuits the router already recovered from. They stay reachable for their
 * strike history but are not current blocks, so they are folded away by default
 * and never counted as blocked.
 */
function ClearedCircuitList({ circuits }: { circuits: OverviewBlockedCircuit[] }) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  return (
    <div className="space-y-2" data-testid="overview-cleared-circuits">
      <button
        type="button"
        aria-expanded={open}
        className="cursor-pointer text-xs font-medium text-muted-foreground"
        onClick={() => setOpen((value) => !value)}
      >
        {t("office:overviewRecentlyClearedCircuits", { count: circuits.length })}
      </button>
      {open ? (
        <div className="grid gap-2 md:grid-cols-2 xl:grid-cols-3">
          {circuits.map((circuit) => (
            <BlockedCircuitCard key={circuit.resource_key} circuit={circuit} />
          ))}
        </div>
      ) : null}
    </div>
  );
}

/**
 * One resource circuit, named by the profile and model it covers, with the
 * reason it is blocked and the instant it is expected to clear. A cleared
 * circuit is not a current block, so it wears the neutral treatment rather
 * than the blocked one.
 */
function BlockedCircuitCard({ circuit }: { circuit: OverviewBlockedCircuit }) {
  const { t } = useTranslation();
  const scope = circuitScope(t, circuit.scope);
  const reason = circuitReason(t, circuit.code);
  const blocking = isCurrentBlock(circuit);
  return (
    <div
      className={`rounded-md border p-3 text-sm ${
        blocking ? "border-status-error/40 bg-status-error/5" : "border-tile-border bg-tile"
      }`}
      data-testid="overview-blocked-circuit"
    >
      <span className="font-medium">{circuitTitle(t, circuit)}</span>
      <div className="mt-1 text-xs text-muted-foreground">
        {[scope, reason].filter(Boolean).join(" · ")}
      </div>
      <div
        className={`mt-1 text-xs ${blocking ? statusTextClass("error") : "text-muted-foreground"}`}
        title={circuit.until ? relativeTime(circuit.until) : undefined}
      >
        {circuit.until
          ? t("office:overviewClearsAt", { time: occurredTime(circuit.until) })
          : t("office:overviewBlockedNoClearTime")}
      </div>
    </div>
  );
}

function ModelCard({ model }: { model: OverviewModel }) {
  const { t } = useTranslation();
  const href = profileHref(model);
  const name = model.name || model.agent_profile_id;
  const usage = useAgentProfileUsage(model.agent_profile_id);
  return (
    <div className="rounded-md border border-border p-3 text-sm" data-testid="overview-model">
      {href ? (
        <Link href={href} className="font-medium hover:underline">
          {name}
        </Link>
      ) : (
        <span className="font-medium">{name}</span>
      )}
      <div className="mt-1 flex flex-wrap gap-x-3 text-xs text-muted-foreground">
        <span>
          {t("common:sessionStateRunning")} <span className="tabular-nums">{model.running}</span>
        </span>
        <span>
          {t("office:overviewSessions24h")}{" "}
          <span className="tabular-nums">{model.sessions_24h}</span>
        </span>
        {model.failed_24h > 0 && (
          <span className={statusTextClass("error")}>
            {t("common:sessionStateFailed")}{" "}
            <span className="tabular-nums">{model.failed_24h}</span>
          </span>
        )}
      </div>
      {model.kind !== "dynamic" && <ModelUsageLine usage={usage} />}
      {(model.errors ?? []).slice(0, 3).map((error) => (
        <div key={error.kind} className="mt-1 truncate font-mono text-[11px]" title={error.kind}>
          <span className="tabular-nums">{error.count}</span> × {error.kind}
        </div>
      ))}
    </div>
  );
}

const USAGE_STATE_KEYS: Record<string, string> = {
  unavailable: "office:overviewUsageStateUnavailable",
  no_usage_api: "office:overviewUsageStateNoUsageApi",
  unsupported: "office:overviewUsageStateUnsupported",
};

// Providers name a usage window by its own length ("5-hour", "7-day",
// "30-day"), optionally scoped to one model ("7-day (Sonnet)"). The length is
// read rather than the word, so the same window reads in the active locale
// instead of as the provider's English label. A name that is not a length
// ("opaque", a provider's own wording) is left verbatim.
const WINDOW_LABEL = /^(\d+)-(hour|day|week|month)s?(?:\s*\((.+)\))?$/;
const WINDOW_UNIT_MINUTES: Record<string, number> = {
  hour: 60,
  day: 24 * 60,
  week: 7 * 24 * 60,
  month: 30 * 24 * 60,
};

/** One usage window's name in the active locale, or the provider's own wording. */
export function usageWindowLabel(label: string): string {
  const match = WINDOW_LABEL.exec(label.trim());
  if (!match) return label;
  const unit = WINDOW_UNIT_MINUTES[match[2]];
  if (unit === undefined) return label;
  const length = durationFromMinutes(Number(match[1]) * unit);
  // A scoped window keeps the model name the provider gave it: that is a
  // product name, not copy this catalog translates.
  return match[3] ? `${length} (${match[3]})` : length;
}

/**
 * Why a profile's usage could not be read, in the active locale. A code this
 * catalog does not name stays as the code inside an explicit unknown reading,
 * because an untranslated diagnostic is truthful where a borrowed name would
 * claim something the provider never said.
 */
function usageUnavailableReason(t: TFunction, usage: AgentProfileUsage): string {
  const reasonKey = profileUsageFailureReasonKey(usage.reason);
  if (reasonKey) return t(reasonKey);
  const stateKey = USAGE_STATE_KEYS[usage.state];
  if (stateKey) return t(stateKey);
  return t("office:overviewUsageStateUnknown", { code: usage.reason ?? usage.state });
}

/**
 * A concrete profile's provider usage. An absent or unusable read is reported
 * as such; it is never shown as zero usage or as nothing wrong.
 */
function ModelUsageLine({ usage }: { usage?: AgentProfileUsage }) {
  const { t } = useTranslation();
  if (!usage) {
    return (
      <div
        className="mt-1 text-xs text-muted-foreground"
        data-testid="overview-model-usage-pending"
      >
        {t("office:overviewUsagePending")}
      </div>
    );
  }
  if (usage.state !== "ok") {
    return (
      <div
        className="mt-1 text-xs text-muted-foreground"
        data-testid="overview-model-usage-unavailable"
      >
        {t("office:overviewUsageUnavailable", { reason: usageUnavailableReason(t, usage) })}
      </div>
    );
  }
  const windows = usage.windows ?? [];
  if (windows.length === 0 && !usage.stale) return null;
  return (
    <div className="mt-1 space-y-0.5 text-xs" data-testid="overview-model-usage">
      {windows.map((window) => (
        <div key={`${window.label}:${window.reset_at ?? ""}`} className="flex flex-wrap gap-x-2">
          <span>{usageWindowLabel(window.label)}</span>
          <span className="tabular-nums">{Math.round(window.utilization_pct)}%</span>
          {window.limit_reached && (
            <span className={statusTextClass("error")}>
              {t("office:overviewUsageLimitReached")}
            </span>
          )}
          {window.reset_at && (
            <span className="text-muted-foreground" title={relativeTime(window.reset_at)}>
              {t("office:overviewUsageResets", { time: occurredTime(window.reset_at) })}
            </span>
          )}
        </div>
      ))}
      {usage.stale && usage.fetched_at && (
        <div className="text-muted-foreground" data-testid="overview-model-usage-stale">
          {t("office:overviewUsageStaleObserved", {
            relative: relativeTime(usage.fetched_at),
          })}
        </div>
      )}
      {usage.observed && usage.fetched_at && (
        <div className="text-muted-foreground" data-testid="overview-model-usage-observed">
          {t(
            usage.source === "measured_call"
              ? "office:overviewUsageMeasuredCall"
              : "office:overviewUsageObserved",
            { relative: relativeTime(usage.fetched_at) },
          )}
        </div>
      )}
    </div>
  );
}

function blockedAccountState(t: TFunction, account: OverviewBlockedAccount): string {
  if (account.state === "user_action_required") return t("office:needsAction");
  if (account.retry_at) {
    return t("office:overviewClearsAt", { time: occurredTime(account.retry_at) });
  }
  return t("office:overviewBlockedAccounts");
}

function BlockedAccountCard({ account }: { account: OverviewBlockedAccount }) {
  const { t } = useTranslation();
  return (
    <div
      className="rounded-md border border-status-error/40 bg-status-error/5 p-3 text-sm"
      data-testid="overview-blocked-account"
    >
      <Link
        href={`/office/workspace/routing?${new URLSearchParams({ workspaceId: account.workspace_id }).toString()}`}
        className="font-medium hover:underline"
      >
        {account.provider_id}
        {account.scope_value ? ` · ${account.scope_value}` : ""}
      </Link>
      <div
        className={`mt-1 text-xs ${statusTextClass("error")}`}
        title={account.retry_at ? relativeTime(account.retry_at) : undefined}
      >
        {blockedAccountState(t, account)}
        {account.error_code ? ` · ${account.error_code}` : ""}
      </div>
    </div>
  );
}
