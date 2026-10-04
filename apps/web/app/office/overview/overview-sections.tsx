"use client";

import Link from "@/components/routing/app-link";
import { Card } from "@kandev/ui/card";
import { Badge } from "@kandev/ui/badge";
import type { TFunction } from "i18next";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { useFeature } from "@/hooks/domains/features/use-feature";
import { linkToTask } from "@/lib/links";
import { NEEDS_YOU_INBOX_HREF } from "@/lib/navigation/needs-you-inbox-destination";
import type {
  OverviewBlockedAccount,
  OverviewBlockedCircuit,
  OverviewEvent,
  OverviewEventKind,
  OverviewHumanItem,
  OverviewModel,
} from "@/lib/state/slices/office/overview-types";
import type { AgentProfileUsage } from "@/lib/api/domains/agent-profile-usage-api";
import { useAgentProfileUsage } from "@/hooks/domains/settings/use-agent-profile-usage";
import {
  circuitReason,
  circuitScope,
  circuitTitle,
  isCurrentBlock,
  relativeTime,
} from "./overview-format";
import { MODELS_ANCHOR, NEEDS_HUMAN_ANCHOR } from "./overview-system-cards";

// Catalog keys for the last-24-hours event kinds, not copy.
const EVENT_LABEL_KEYS: Record<OverviewEvent["kind"], string> = {
  task_created: "office:overviewEventTaskCreated",
  server_started: "office:overviewServerStarted",
  task_completed: "common:taskStateCompleted",
  session_failed: "common:sessionStateFailed",
  automation_run: "common:automation",
};

/**
 * How many rows the section renders. The server keeps more than this per kind
 * so the cap can be applied after a kind filter, rather than one kind crowding
 * the others out of the list before the viewer can choose.
 */
const EVENT_DISPLAY_LIMIT = 50;

function SectionCard({
  id,
  title,
  action,
  children,
}: {
  id?: string;
  title: string;
  action?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <Card className="scroll-mt-4 p-0" id={id}>
      <div className="flex items-center justify-between gap-2 border-b border-border px-4 py-3">
        <h2 className="text-sm font-semibold">{title}</h2>
        {action}
      </div>
      {children}
    </Card>
  );
}

function EmptyRow() {
  const { t } = useTranslation();
  return (
    <div className="px-4 py-6 text-center text-sm text-muted-foreground">
      {t("office:allClear")}
    </div>
  );
}

function humanHref(item: OverviewHumanItem): string {
  if (item.kind === "question" && item.task_id) {
    return linkToTask(item.task_id, item.session_id ? { sessionId: item.session_id } : undefined);
  }
  return `/office/inbox?${new URLSearchParams({ workspaceId: item.workspace_id }).toString()}`;
}

/**
 * Questions agents are waiting on and pending approvals. Repeats of the same
 * question on the same task arrive as one row carrying how many times it was
 * asked; a different question stays its own row.
 */
export function OverviewNeedsHuman({ items }: { items: OverviewHumanItem[] }) {
  const { t } = useTranslation();
  const needsYouEnabled = useFeature("needsYouInbox");
  return (
    <SectionCard
      id={NEEDS_HUMAN_ANCHOR}
      title={t("needsYouInbox:tabLabel")}
      action={
        <Link
          href={needsYouEnabled ? NEEDS_YOU_INBOX_HREF : "/office/inbox"}
          className="text-xs text-primary hover:underline"
        >
          {t("office:overviewOpenInbox")}
        </Link>
      }
    >
      {items.length === 0 ? (
        <EmptyRow />
      ) : (
        <div className="divide-y divide-border">
          {items.map((item) => (
            <Link
              key={`${item.kind}:${item.id}`}
              href={humanHref(item)}
              className="flex items-center gap-3 px-4 py-2 text-sm hover:bg-muted/50"
              data-testid="overview-human-item"
            >
              <Badge variant="outline">
                {item.kind === "question"
                  ? t("task:lateAnswerQuestionLabel")
                  : t("office:approval")}
              </Badge>
              <span className="text-xs text-muted-foreground">{item.workspace_name}</span>
              <span className="min-w-0 flex-1 truncate">
                {item.kind === "question" ? item.task_title : item.approval_type}
              </span>
              {item.count > 1 && (
                <Badge variant="outline" data-testid="overview-human-repeat">
                  {t("office:overviewRepeatedCount", { count: item.count })}
                </Badge>
              )}
              <span className="whitespace-nowrap text-xs text-muted-foreground">
                {relativeTime(item.created_at)}
              </span>
            </Link>
          ))}
        </div>
      )}
    </SectionCard>
  );
}

function eventHref(event: OverviewEvent): string | undefined {
  if (event.kind === "automation_run" && event.workspace_id && event.automation_id) {
    return `/settings/workspaces/${encodeURIComponent(event.workspace_id)}/automations/${encodeURIComponent(event.automation_id)}`;
  }
  if (event.task_id) {
    return linkToTask(
      event.task_id,
      event.session_id ? { sessionId: event.session_id } : undefined,
    );
  }
  return undefined;
}

/**
 * What happened in the last 24 hours: a new task, a completion, a session
 * failure, an automation run, or a server start. The chips carry how many of
 * each kind arrived and filter the list; the row cap applies after that filter.
 */
export function OverviewLast24h({
  events,
  workspaceNames,
}: {
  events: OverviewEvent[];
  workspaceNames: Record<string, string>;
}) {
  const { t } = useTranslation();
  const [kind, setKind] = useState<EventFilter>("all");
  const counts = useMemo(() => countEventKinds(events), [events]);
  const visible = useMemo(
    () =>
      (kind === "all" ? events : events.filter((event) => event.kind === kind)).slice(
        0,
        EVENT_DISPLAY_LIMIT,
      ),
    [events, kind],
  );
  return (
    <SectionCard
      title={t("sentry:statsPeriodLast24Hours")}
      action={<EventKindChips counts={counts} selected={kind} onSelect={setKind} />}
    >
      {visible.length === 0 ? (
        <EmptyRow />
      ) : (
        <div className="divide-y divide-border">
          {visible.map((event, index) => {
            const href = eventHref(event);
            const content = (
              <>
                <span className="w-20 shrink-0 whitespace-nowrap text-xs text-muted-foreground">
                  {relativeTime(event.at)}
                </span>
                <Badge variant="outline" className="shrink-0">
                  {t(EVENT_LABEL_KEYS[event.kind] ?? "common:unknown")}
                </Badge>
                {event.workspace_id && (
                  <span className="shrink-0 text-xs text-muted-foreground">
                    {workspaceNames[event.workspace_id]}
                  </span>
                )}
                <span className="min-w-0 flex-1 truncate">{event.title}</span>
                {event.detail && (
                  <span
                    className="max-w-[40%] truncate font-mono text-[11px] text-muted-foreground"
                    title={event.detail}
                  >
                    {event.detail}
                  </span>
                )}
              </>
            );
            const key = `${event.kind}:${event.task_id ?? ""}:${event.session_id ?? ""}:${index}`;
            return href ? (
              <Link
                key={key}
                href={href}
                className="flex items-center gap-3 px-4 py-2 text-sm hover:bg-muted/50"
                data-testid="overview-event"
              >
                {content}
              </Link>
            ) : (
              <div key={key} className="flex items-center gap-3 px-4 py-2 text-sm">
                {content}
              </div>
            );
          })}
        </div>
      )}
    </SectionCard>
  );
}

/** The chip selection: every kind, or one kind. */
type EventFilter = OverviewEventKind | "all";

type EventKindCounts = { kind: EventFilter; count: number }[];

/** Counts each kind that actually arrived, newest kinds first, all included. */
function countEventKinds(events: OverviewEvent[]): EventKindCounts {
  const counts = new Map<OverviewEventKind, number>();
  for (const event of events) {
    counts.set(event.kind, (counts.get(event.kind) ?? 0) + 1);
  }
  return [
    { kind: "all", count: events.length },
    ...[...counts.entries()].map(([kind, count]) => ({ kind, count })),
  ];
}

function EventKindChips({
  counts,
  selected,
  onSelect,
}: {
  counts: EventKindCounts;
  selected: EventFilter;
  onSelect: (kind: EventFilter) => void;
}) {
  const { t } = useTranslation();
  return (
    <div className="flex flex-wrap gap-1.5" data-testid="overview-event-kinds">
      {counts.map(({ kind, count }) => {
        const active = kind === selected;
        return (
          <button
            key={kind}
            type="button"
            aria-pressed={active}
            onClick={() => onSelect(kind)}
            className={`min-h-11 cursor-pointer rounded-full border px-2.5 text-xs sm:min-h-0 ${
              active
                ? "border-primary bg-primary/10 text-foreground"
                : "border-border text-muted-foreground"
            }`}
            data-testid={`overview-event-kind-${kind}`}
          >
            {kind === "all" ? t("office:overviewEventAllKinds") : t(EVENT_LABEL_KEYS[kind])}
            <span className="ml-1 tabular-nums">{count}</span>
          </button>
        );
      })}
    </div>
  );
}

function profileHref(model: OverviewModel): string | undefined {
  if (!model.agent_name) return undefined;
  return `/settings/agents/${encodeURIComponent(model.agent_name)}/profiles/${encodeURIComponent(model.agent_profile_id)}`;
}

/**
 * Agent profiles with session counts, error kinds, and usage, split so a
 * dynamic profile (which routes through ordered concrete profiles) is never
 * read as one model, plus every account or resource the router is avoiding.
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
  const dynamic = models.filter((model) => model.kind === "dynamic");
  const concrete = models.filter((model) => model.kind !== "dynamic");
  const groups: { key: string; title: string; cards: OverviewModel[] }[] = [
    { key: "dynamic", title: t("office:overviewDynamicProfiles"), cards: dynamic },
    { key: "concrete", title: t("office:overviewConcreteModels"), cards: concrete },
  ];
  const nothingBlocked = blocked.length === 0 && circuits.length === 0 && circuitsKnown;
  return (
    <SectionCard id={MODELS_ANCHOR} title={t("office:overviewModels")}>
      {models.length === 0 && nothingBlocked ? (
        <EmptyRow />
      ) : (
        <div className="space-y-3 p-3">
          {groups.map((group) =>
            group.cards.length === 0 ? null : (
              <div key={group.key} className="space-y-2">
                <h3 className="text-xs font-medium text-muted-foreground">{group.title}</h3>
                <div className="grid gap-2 md:grid-cols-2 xl:grid-cols-3">
                  {group.cards.map((model) => (
                    <ModelCard key={model.agent_profile_id} model={model} />
                  ))}
                </div>
              </div>
            ),
          )}
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
 * reason it is blocked and the instant it is expected to clear.
 */
function BlockedCircuitCard({ circuit }: { circuit: OverviewBlockedCircuit }) {
  const { t } = useTranslation();
  const scope = circuitScope(t, circuit.scope);
  const reason = circuitReason(t, circuit.code);
  return (
    <div
      className="rounded-md border border-amber-500/40 p-3 text-sm"
      data-testid="overview-blocked-circuit"
    >
      <span className="font-medium">{circuitTitle(t, circuit)}</span>
      <div className="mt-1 text-xs text-muted-foreground">
        {[scope, reason].filter(Boolean).join(" · ")}
      </div>
      <div className="mt-1 text-xs text-muted-foreground">
        {circuit.until
          ? t("office:overviewClearsAt", { time: relativeTime(circuit.until) })
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
          {t("common:sessionStateRunning")} {model.running}
        </span>
        <span>
          {t("office:overviewSessions24h")} {model.sessions_24h}
        </span>
        {model.failed_24h > 0 && (
          <span className="text-destructive">
            {t("common:sessionStateFailed")} {model.failed_24h}
          </span>
        )}
      </div>
      {model.kind !== "dynamic" && <ModelUsageLine usage={usage} />}
      {(model.errors ?? []).slice(0, 3).map((error) => (
        <div key={error.kind} className="mt-1 truncate font-mono text-[11px]" title={error.kind}>
          {error.count} × {error.kind}
        </div>
      ))}
    </div>
  );
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
        {t("office:overviewUsageUnavailable", { reason: usage.reason ?? usage.state })}
      </div>
    );
  }
  const windows = usage.windows ?? [];
  if (windows.length === 0) return null;
  return (
    <div className="mt-1 space-y-0.5 text-xs" data-testid="overview-model-usage">
      {windows.map((window) => (
        <div key={`${window.label}:${window.reset_at ?? ""}`} className="flex flex-wrap gap-x-2">
          <span>{window.label}</span>
          <span className="tabular-nums">{Math.round(window.utilization_pct)}%</span>
          {window.limit_reached && (
            <span className="text-destructive">{t("office:overviewUsageLimitReached")}</span>
          )}
          {window.reset_at && (
            <span className="text-muted-foreground">
              {t("office:overviewUsageResets", { time: relativeTime(window.reset_at) })}
            </span>
          )}
        </div>
      ))}
    </div>
  );
}

function blockedAccountState(t: TFunction, account: OverviewBlockedAccount): string {
  if (account.state === "user_action_required") return t("office:needsAction");
  if (account.retry_at)
    return t("office:overviewClearsAt", { time: relativeTime(account.retry_at) });
  return t("office:overviewBlockedAccounts");
}

function BlockedAccountCard({ account }: { account: OverviewBlockedAccount }) {
  const { t } = useTranslation();
  return (
    <div
      className="rounded-md border border-amber-500/40 p-3 text-sm"
      data-testid="overview-blocked-account"
    >
      <Link
        href={`/office/workspace/routing?${new URLSearchParams({ workspaceId: account.workspace_id }).toString()}`}
        className="font-medium hover:underline"
      >
        {account.provider_id}
        {account.scope_value ? ` · ${account.scope_value}` : ""}
      </Link>
      <div className="mt-1 text-xs text-muted-foreground">
        {blockedAccountState(t, account)}
        {account.error_code ? ` · ${account.error_code}` : ""}
      </div>
    </div>
  );
}
