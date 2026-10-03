"use client";

import Link from "@/components/routing/app-link";
import { Card } from "@kandev/ui/card";
import { Badge } from "@kandev/ui/badge";
import type { TFunction } from "i18next";
import { useTranslation } from "react-i18next";
import { useFeature } from "@/hooks/domains/features/use-feature";
import { linkToTask } from "@/lib/links";
import { NEEDS_YOU_INBOX_HREF } from "@/lib/navigation/needs-you-inbox-destination";
import type {
  OverviewBlockedAccount,
  OverviewEvent,
  OverviewHumanItem,
  OverviewModel,
} from "@/lib/state/slices/office/overview-types";
import { relativeTime } from "./overview-format";
import { MODELS_ANCHOR, NEEDS_HUMAN_ANCHOR } from "./overview-system-cards";

// Catalog keys for the last-24-hours event kinds, not copy.
const EVENT_LABEL_KEYS: Record<OverviewEvent["kind"], string> = {
  server_started: "office:overviewServerStarted",
  task_completed: "common:taskStateCompleted",
  session_failed: "common:sessionStateFailed",
  automation_run: "common:automation",
};

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

/** Questions agents are waiting on and pending approvals. */
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
  if (event.task_id) {
    return linkToTask(
      event.task_id,
      event.session_id ? { sessionId: event.session_id } : undefined,
    );
  }
  return undefined;
}

/** Server starts, completions, failures, and automation runs. */
export function OverviewLast24h({
  events,
  workspaceNames,
}: {
  events: OverviewEvent[];
  workspaceNames: Record<string, string>;
}) {
  const { t } = useTranslation();
  return (
    <SectionCard title={t("sentry:statsPeriodLast24Hours")}>
      {events.length === 0 ? (
        <EmptyRow />
      ) : (
        <div className="divide-y divide-border">
          {events.map((event, index) => {
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

function profileHref(model: OverviewModel): string | undefined {
  if (!model.agent_name) return undefined;
  return `/settings/agents/${encodeURIComponent(model.agent_name)}/profiles/${encodeURIComponent(model.agent_profile_id)}`;
}

/** Agent profiles with session counts, error kinds, and blocked accounts. */
export function OverviewModels({
  models,
  blocked,
}: {
  models: OverviewModel[];
  blocked: OverviewBlockedAccount[];
}) {
  const { t } = useTranslation();
  return (
    <SectionCard id={MODELS_ANCHOR} title={t("office:overviewModels")}>
      {models.length === 0 && blocked.length === 0 ? (
        <EmptyRow />
      ) : (
        <div className="grid gap-2 p-3 md:grid-cols-2 xl:grid-cols-3">
          {models.map((model) => (
            <ModelCard key={model.agent_profile_id} model={model} />
          ))}
          {blocked.map((account) => (
            <BlockedAccountCard
              key={`${account.workspace_id}:${account.provider_id}:${account.scope}:${account.scope_value}`}
              account={account}
            />
          ))}
        </div>
      )}
    </SectionCard>
  );
}

function ModelCard({ model }: { model: OverviewModel }) {
  const { t } = useTranslation();
  const href = profileHref(model);
  const name = model.name || model.agent_profile_id;
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
      {(model.errors ?? []).slice(0, 3).map((error) => (
        <div key={error.kind} className="mt-1 truncate font-mono text-[11px]" title={error.kind}>
          {error.count} × {error.kind}
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
