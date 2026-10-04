"use client";

import { useState, type ReactNode } from "react";
import { Card } from "@kandev/ui/card";
import { Button } from "@kandev/ui/button";
import { Tabs, TabsList, TabsTrigger } from "@kandev/ui/tabs";
import type { TFunction } from "i18next";
import { useTranslation } from "react-i18next";
import { getWorkspaceAggregateRunning } from "@/lib/api/domains/office-overview-api";
import { useOverviewList } from "@/hooks/domains/office/use-overview-list";
import type { OverviewRunningKind, OverviewSystem } from "@/lib/state/slices/office/overview-types";
import { durationSince, relativeTime } from "./overview-format";
import { OverviewQueueTable, OverviewSessionTable, OverviewTaskTable } from "./overview-tables";

const RUNNING_LIST_LIMIT = 100;
export const NEEDS_HUMAN_ANCHOR = "overview-needs-human";
export const MODELS_ANCHOR = "overview-models";

/**
 * One system widget. The title, value, and sub line are stacked and centered
 * on both axes with one gap, so six cards of differing content line up. Before
 * the first accepted read, `loading` replaces the number: a zero would read as
 * a real measurement.
 */
function StatCard({
  title,
  value,
  sub,
  loading,
  expanded,
  onToggle,
  href,
  testId,
}: {
  title: string;
  value: ReactNode;
  sub?: ReactNode;
  loading?: boolean;
  expanded?: boolean;
  onToggle?: () => void;
  href?: string;
  testId: string;
}) {
  const { t } = useTranslation();
  const body = (
    <>
      <div className="text-xs text-muted-foreground">{title}</div>
      <div className="text-2xl font-semibold tabular-nums">
        {loading ? (
          <span
            className="text-base font-normal text-muted-foreground"
            data-testid={`${testId}-analyzing`}
          >
            {t("office:overviewAnalyzing")}
          </span>
        ) : (
          value
        )}
      </div>
      <div className="min-h-4 text-xs text-muted-foreground">{loading ? null : sub}</div>
    </>
  );
  const className =
    "flex h-full w-full flex-col items-center justify-center gap-1 rounded-lg p-4 text-center transition-colors hover:bg-muted/50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring cursor-pointer";
  if (onToggle) {
    return (
      <Card className="p-0" data-testid={testId}>
        <button type="button" className={className} aria-expanded={expanded} onClick={onToggle}>
          {body}
        </button>
      </Card>
    );
  }
  if (href) {
    return (
      <Card className="p-0" data-testid={testId}>
        <a href={href} className={className}>
          {body}
        </a>
      </Card>
    );
  }
  return (
    <Card className="p-0" data-testid={testId}>
      <div className={className}>{body}</div>
    </Card>
  );
}

/**
 * The blocked-model sub line. A source that did not answer reads as unknown
 * rather than as "nothing is blocked"; otherwise the soonest clear instant is
 * shown, which can come from either block source.
 */
function blockedSub(t: TFunction, system: OverviewSystem, loading: boolean): string | undefined {
  if (system.blocked_accounts_total === undefined) {
    return loading ? undefined : t("office:overviewBlockedUnknown");
  }
  if (!system.earliest_unblock_at) return undefined;
  return t("office:overviewClearsAt", { time: relativeTime(system.earliest_unblock_at) });
}

/** The six system cards; three of them open the cross-workspace lists. */
export function OverviewSystemCards({
  system,
  loading,
  refreshSeconds,
}: {
  system: OverviewSystem | undefined;
  loading: boolean;
  refreshSeconds: number;
}) {
  const { t } = useTranslation();
  const [open, setOpen] = useState<OverviewRunningKind | null>(null);
  const toggle = (kind: OverviewRunningKind) =>
    setOpen((current) => (current === kind ? null : kind));
  const values: OverviewSystem = system ?? {
    active_tasks: 0,
    running_sessions: 0,
    waiting_input_sessions: 0,
    session_limit: 0,
    queued_messages: 0,
    undeliverable_messages: 0,
    needs_human: 0,
    blocked_accounts: 0,
    problems: 0,
  };
  const limit = values.session_limit > 0 ? ` / ${values.session_limit}` : "";
  // The blocked total covers dynamic circuits too; an absent total means the
  // circuits source did not answer, which is not the same as none blocked.
  const blocked = values.blocked_accounts_total ?? values.blocked_accounts;
  return (
    <section className="space-y-3">
      <div className="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-6">
        <StatCard
          testId="overview-card-system"
          loading={loading}
          title={t("common:system")}
          value={values.started_at ? durationSince(values.started_at) : t("common:unknown")}
          sub={values.started_at ? t("settings:uptime") : undefined}
        />
        <StatCard
          testId="overview-card-running-tasks"
          loading={loading}
          title={t("office:overviewRunningTasks")}
          value={values.active_tasks}
          sub={
            values.problems > 0
              ? t("office:overviewProblemCount", { count: values.problems })
              : undefined
          }
          expanded={open === "tasks"}
          onToggle={() => toggle("tasks")}
        />
        <StatCard
          testId="overview-card-running-sessions"
          loading={loading}
          title={t("office:overviewRunningSessions")}
          value={`${values.running_sessions}${limit}`}
          sub={t("office:overviewWaitingInput", { count: values.waiting_input_sessions })}
          expanded={open === "sessions"}
          onToggle={() => toggle("sessions")}
        />
        <StatCard
          testId="overview-card-queue"
          loading={loading}
          title={t("chat:queuedMessages")}
          value={values.queued_messages}
          sub={t("office:overviewUndeliverableCount", { count: values.undeliverable_messages })}
          expanded={open === "queue"}
          onToggle={() => toggle("queue")}
        />
        <StatCard
          testId="overview-card-needs-human"
          loading={loading}
          title={t("needsYouInbox:tabLabel")}
          value={values.needs_human}
          href={`#${NEEDS_HUMAN_ANCHOR}`}
        />
        <StatCard
          testId="overview-card-blocked-accounts"
          loading={loading}
          title={t("office:overviewBlockedAccounts")}
          value={blocked}
          sub={blockedSub(t, values, loading)}
          href={`#${MODELS_ANCHOR}`}
        />
      </div>
      {open && (
        <OverviewRunningPanel kind={open} onKind={setOpen} refreshSeconds={refreshSeconds} />
      )}
    </section>
  );
}

function OverviewRunningPanel({
  kind,
  onKind,
  refreshSeconds,
}: {
  kind: OverviewRunningKind;
  onKind: (kind: OverviewRunningKind | null) => void;
  refreshSeconds: number;
}) {
  const { t } = useTranslation();
  const list = useOverviewList(
    `running:${kind}`,
    () => getWorkspaceAggregateRunning(kind, RUNNING_LIST_LIMIT, { cache: "no-store" }),
    refreshSeconds,
  );
  const data = list.data;
  return (
    <Card className="p-0" data-testid="overview-running-panel">
      <div className="flex items-center justify-between gap-2 border-b border-border p-2">
        <Tabs value={kind} onValueChange={(value) => onKind(value as OverviewRunningKind)}>
          <TabsList>
            <TabsTrigger value="tasks">{t("office:overviewRunningTasks")}</TabsTrigger>
            <TabsTrigger value="sessions">{t("office:overviewRunningSessions")}</TabsTrigger>
            <TabsTrigger value="queue">{t("chat:queuedMessages")}</TabsTrigger>
          </TabsList>
        </Tabs>
        <Button variant="ghost" size="sm" className="cursor-pointer" onClick={() => onKind(null)}>
          {t("office:collapse")}
        </Button>
      </div>
      <OverviewListBody state={list.loadState} empty={(data?.total ?? 0) === 0}>
        {data?.tasks && <OverviewTaskTable rows={data.tasks} showProject />}
        {data?.sessions && <OverviewSessionTable rows={data.sessions} />}
        {data?.queue && <OverviewQueueTable rows={data.queue} />}
        {data && data.total > RUNNING_LIST_LIMIT && (
          <div className="px-4 py-2 text-xs text-muted-foreground">
            {t("office:overviewShowingOf", { shown: RUNNING_LIST_LIMIT, total: data.total })}
          </div>
        )}
      </OverviewListBody>
    </Card>
  );
}

/** Loading, error, and empty states around an expanded list. */
export function OverviewListBody({
  state,
  empty,
  children,
}: {
  state: "idle" | "loading" | "loaded" | "error";
  empty: boolean;
  children: ReactNode;
}) {
  const { t } = useTranslation();
  if (state === "loading" || state === "idle") {
    return (
      <div className="px-4 py-6 text-sm text-muted-foreground" role="status">
        {t("common:loading")}
      </div>
    );
  }
  if (state === "error") {
    return (
      <div className="px-4 py-6 text-sm text-destructive" role="alert">
        {t("office:failedToLoad")}
      </div>
    );
  }
  if (empty) {
    return <div className="px-4 py-6 text-sm text-muted-foreground">{t("office:allClear")}</div>;
  }
  return <div className="overflow-x-auto">{children}</div>;
}
