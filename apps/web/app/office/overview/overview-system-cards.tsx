"use client";

import { useState, type ReactNode } from "react";
import { Card } from "@kandev/ui/card";
import { Button } from "@kandev/ui/button";
import { Tabs, TabsList, TabsTrigger } from "@kandev/ui/tabs";
import { useTranslation } from "react-i18next";
import { getWorkspaceAggregateRunning } from "@/lib/api/domains/office-overview-api";
import { useOverviewList } from "@/hooks/domains/office/use-overview-list";
import type { OverviewRunningKind, OverviewSystem } from "@/lib/state/slices/office/overview-types";
import { durationSince, relativeTime } from "./overview-format";
import { OverviewQueueTable, OverviewSessionTable, OverviewTaskTable } from "./overview-tables";

const RUNNING_LIST_LIMIT = 100;
export const NEEDS_HUMAN_ANCHOR = "overview-needs-human";
export const MODELS_ANCHOR = "overview-models";

function StatCard({
  title,
  value,
  sub,
  expanded,
  onToggle,
  href,
  testId,
}: {
  title: string;
  value: ReactNode;
  sub?: ReactNode;
  expanded?: boolean;
  onToggle?: () => void;
  href?: string;
  testId: string;
}) {
  const body = (
    <>
      <div className="text-xs text-muted-foreground">{title}</div>
      <div className="mt-1 text-2xl font-semibold tabular-nums">{value}</div>
      {sub && <div className="mt-1 text-xs text-muted-foreground">{sub}</div>}
    </>
  );
  const className =
    "block h-full w-full rounded-lg p-4 text-left transition-colors hover:bg-muted/50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring cursor-pointer";
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
    <Card className="p-4" data-testid={testId}>
      {body}
    </Card>
  );
}

/** The six system cards; three of them open the cross-workspace lists. */
export function OverviewSystemCards({ system }: { system: OverviewSystem }) {
  const { t } = useTranslation();
  const [open, setOpen] = useState<OverviewRunningKind | null>(null);
  const toggle = (kind: OverviewRunningKind) =>
    setOpen((current) => (current === kind ? null : kind));
  const limit = system.session_limit > 0 ? ` / ${system.session_limit}` : "";
  return (
    <section className="space-y-3">
      <div className="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-6">
        <StatCard
          testId="overview-card-system"
          title={t("common:system")}
          value={system.started_at ? durationSince(system.started_at) : t("common:unknown")}
          sub={system.started_at ? t("settings:uptime") : undefined}
        />
        <StatCard
          testId="overview-card-running-tasks"
          title={t("office:overviewRunningTasks")}
          value={system.active_tasks}
          sub={
            system.problems > 0
              ? t("office:overviewProblemCount", { count: system.problems })
              : undefined
          }
          expanded={open === "tasks"}
          onToggle={() => toggle("tasks")}
        />
        <StatCard
          testId="overview-card-running-sessions"
          title={t("office:overviewRunningSessions")}
          value={`${system.running_sessions}${limit}`}
          sub={t("office:overviewWaitingInput", { count: system.waiting_input_sessions })}
          expanded={open === "sessions"}
          onToggle={() => toggle("sessions")}
        />
        <StatCard
          testId="overview-card-queue"
          title={t("chat:queuedMessages")}
          value={system.queued_messages}
          sub={t("office:overviewUndeliverableCount", { count: system.undeliverable_messages })}
          expanded={open === "queue"}
          onToggle={() => toggle("queue")}
        />
        <StatCard
          testId="overview-card-needs-human"
          title={t("needsYouInbox:tabLabel")}
          value={system.needs_human}
          href={`#${NEEDS_HUMAN_ANCHOR}`}
        />
        <StatCard
          testId="overview-card-blocked-accounts"
          title={t("office:overviewBlockedAccounts")}
          value={system.blocked_accounts}
          sub={
            system.earliest_unblock_at
              ? t("office:overviewClearsAt", { time: relativeTime(system.earliest_unblock_at) })
              : undefined
          }
          href={`#${MODELS_ANCHOR}`}
        />
      </div>
      {open && <OverviewRunningPanel kind={open} onKind={setOpen} />}
    </section>
  );
}

function OverviewRunningPanel({
  kind,
  onKind,
}: {
  kind: OverviewRunningKind;
  onKind: (kind: OverviewRunningKind | null) => void;
}) {
  const { t } = useTranslation();
  const list = useOverviewList(`running:${kind}`, () =>
    getWorkspaceAggregateRunning(kind, RUNNING_LIST_LIMIT, { cache: "no-store" }),
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
