"use client";

import { useState, type ReactNode } from "react";
import Link from "@/components/routing/app-link";
import { Card } from "@kandev/ui/card";
import { Button } from "@kandev/ui/button";
import { useTranslation } from "react-i18next";
import { useAppStore } from "@/components/state-provider";
import { getWorkspaceAggregateTasks } from "@/lib/api/domains/office-overview-api";
import { useOverviewList } from "@/hooks/domains/office/use-overview-list";
import { linkToTask } from "@/lib/links";
import { resolveHomeHref } from "@/lib/navigation/workspace-home";
import type { WorkspaceAggregateEntry } from "@/lib/state/slices/office/types";
import type {
  OverviewTaskFilter,
  OverviewWorkspaceMetrics,
} from "@/lib/state/slices/office/overview-types";
import { reasonText, relativeTime, statusLabel } from "./overview-format";
import { OverviewListBody } from "./overview-system-cards";
import { OverviewStatusBadge, OverviewTaskTable } from "./overview-tables";

const TASK_LIST_LIMIT = 50;
const TASK_LIST_ALL_LIMIT = 500;

// Catalog keys for the filter chips, not copy.
const FILTER_CHIPS: { filter: OverviewTaskFilter; labelKey: string }[] = [
  { filter: "problems", labelKey: "office:overviewFilterProblems" },
  { filter: "active", labelKey: "common:taskStateInProgress" },
  { filter: "hold", labelKey: "office:projectStatusOnHold" },
  { filter: "all", labelKey: "office:all" },
];

function Metric({
  label,
  value,
  sub,
  onClick,
  href,
  active,
}: {
  label: string;
  value: ReactNode;
  sub?: ReactNode;
  onClick?: () => void;
  href?: string;
  active?: boolean;
}) {
  const body = (
    <>
      <div className="text-[11px] text-muted-foreground">{label}</div>
      <div className="text-lg font-semibold tabular-nums">{value}</div>
      {sub && <div className="text-[11px] text-muted-foreground">{sub}</div>}
    </>
  );
  const className = `block rounded-md p-2 text-left transition-colors hover:bg-muted/60 cursor-pointer ${
    active ? "bg-muted" : ""
  }`;
  if (href) {
    return (
      <Link href={href} className={className}>
        {body}
      </Link>
    );
  }
  if (onClick) {
    return (
      <button type="button" className={className} onClick={onClick}>
        {body}
      </button>
    );
  }
  return <div className="p-2">{body}</div>;
}

export function OverviewWorkspaceCard({ workspace }: { workspace: WorkspaceAggregateEntry }) {
  const { t } = useTranslation();
  const startupPage = useAppStore((state) => state.userSettings.startupPage);
  const [filter, setFilter] = useState<OverviewTaskFilter | null>(null);
  const [limit, setLimit] = useState(TASK_LIST_LIMIT);
  const metrics = workspace.metrics;
  const homeHref = resolveHomeHref({
    workspaceId: workspace.workspace_id,
    inOffice: Boolean(workspace.is_office),
    startupPage,
  });
  const show = (next: OverviewTaskFilter) => {
    setFilter(next);
    setLimit(TASK_LIST_LIMIT);
  };
  const quiet = !metrics || (metrics.open_tasks === 0 && metrics.running_sessions === 0);
  const listId = `overview-tasks-${workspace.workspace_id}`;

  return (
    <Card className="p-0" data-testid="overview-workspace-card">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 px-4 pt-3">
        <Link
          href={homeHref}
          className="font-medium hover:underline"
          data-testid="overview-workspace-link"
        >
          {workspace.name}
        </Link>
        <span className="text-[11px] text-muted-foreground">
          {t(workspace.is_office ? "sidebar:office" : "sidebar:kanban")}
        </span>
        {metrics && !quiet && <OverviewStatusBadge status={metrics.status} />}
        {metrics && <ProblemSummary metrics={metrics} />}
        {quiet && <span className="text-xs text-muted-foreground">{t("office:allClear")}</span>}
        {!quiet && (
          <Button
            variant="outline"
            size="sm"
            className="ml-auto cursor-pointer"
            aria-expanded={filter !== null}
            aria-controls={listId}
            onClick={() => (filter === null ? show("problems") : setFilter(null))}
          >
            {filter === null ? t("office:expand") : t("office:collapse")}
          </Button>
        )}
      </div>
      {metrics && !quiet && <WorkspaceMetrics metrics={metrics} filter={filter} onFilter={show} />}
      {!quiet && <ParentsAndWarning workspace={workspace} />}
      {filter !== null && (
        <WorkspaceTaskList
          id={listId}
          workspaceId={workspace.workspace_id}
          filter={filter}
          limit={limit}
          onFilter={show}
          onShowAll={() => setLimit(TASK_LIST_ALL_LIMIT)}
        />
      )}
      {quiet && <div className="pb-3" />}
    </Card>
  );
}

function ProblemSummary({ metrics }: { metrics: OverviewWorkspaceMetrics }) {
  const { t } = useTranslation();
  const { error, stalled, delayed } = metrics.problems;
  const total = error + stalled + delayed;
  if (total === 0) return null;
  const parts = [
    error > 0 ? `${statusLabel(t, "error")} ${error}` : "",
    stalled > 0 ? `${statusLabel(t, "stalled")} ${stalled}` : "",
    delayed > 0 ? `${statusLabel(t, "delayed")} ${delayed}` : "",
  ].filter(Boolean);
  return (
    <span className="text-xs text-destructive">
      {t("office:overviewProblemCount", { count: total })} ({parts.join(" · ")})
    </span>
  );
}

function WorkspaceMetrics({
  metrics,
  filter,
  onFilter,
}: {
  metrics: OverviewWorkspaceMetrics;
  filter: OverviewTaskFilter | null;
  onFilter: (filter: OverviewTaskFilter) => void;
}) {
  const { t } = useTranslation();
  return (
    <div className="grid grid-cols-2 gap-1 px-2 pt-2 sm:grid-cols-3 lg:grid-cols-6">
      <Metric
        label={t("office:overviewActiveTasks")}
        value={metrics.active_tasks}
        onClick={() => onFilter("active")}
        active={filter === "active"}
      />
      <Metric
        label={t("office:overviewRunningSessions")}
        value={metrics.running_sessions}
        sub={t("office:overviewWaitingInput", { count: metrics.waiting_input_sessions })}
        onClick={() => onFilter("sessions")}
        active={filter === "sessions"}
      />
      <Metric
        label={t("office:overviewLastOutput")}
        value={metrics.last_output_at ? relativeTime(metrics.last_output_at) : "-"}
        href={metrics.last_output_task_id ? linkToTask(metrics.last_output_task_id) : undefined}
      />
      <Metric
        label={t("chat:queuedMessages")}
        value={metrics.queued_messages}
        onClick={() => onFilter("queued")}
        active={filter === "queued"}
      />
      <Metric
        label={t("office:overviewCompleted24h")}
        value={metrics.completed_24h}
        onClick={() => onFilter("completed")}
        active={filter === "completed"}
      />
      <Metric
        label={t("office:overviewOpenTasks")}
        value={metrics.open_tasks}
        sub={t("office:overviewOpenBreakdown", {
          waiting: metrics.waiting_tasks,
          hold: metrics.blocked_tasks,
        })}
        onClick={() => onFilter("all")}
        active={filter === "all"}
      />
    </div>
  );
}

function ParentsAndWarning({ workspace }: { workspace: WorkspaceAggregateEntry }) {
  const { t } = useTranslation();
  const warning = workspace.metrics?.top_warning;
  const parents = workspace.parents ?? [];
  if (!warning && parents.length === 0) return null;
  return (
    <div className="space-y-1 px-4 pt-2">
      {parents.map((parent) => (
        <Link
          key={parent.task_id}
          href={linkToTask(parent.task_id)}
          className="flex items-center gap-2 text-xs hover:underline"
          data-testid="overview-parent-task"
        >
          <OverviewStatusBadge status={parent.status} />
          <span className="truncate font-medium">{parent.title}</span>
          <span className="text-muted-foreground">
            {t("office:overviewSubtasksOpen", {
              open: parent.open_children,
              total: parent.children,
            })}
          </span>
        </Link>
      ))}
      {warning && (
        <Link
          href={linkToTask(warning.task_id)}
          className="flex items-center gap-2 text-xs text-destructive hover:underline"
          data-testid="overview-warning"
        >
          <OverviewStatusBadge status={warning.status} />
          <span className="truncate">
            {warning.task_title} · {reasonText(t, warning.reason)}
          </span>
        </Link>
      )}
    </div>
  );
}

function WorkspaceTaskList({
  id,
  workspaceId,
  filter,
  limit,
  onFilter,
  onShowAll,
}: {
  id: string;
  workspaceId: string;
  filter: OverviewTaskFilter;
  limit: number;
  onFilter: (filter: OverviewTaskFilter) => void;
  onShowAll: () => void;
}) {
  const { t } = useTranslation();
  const list = useOverviewList(`${workspaceId}:${filter}:${limit}`, () =>
    getWorkspaceAggregateTasks(workspaceId, filter, limit, { cache: "no-store" }),
  );
  const total = list.data?.total ?? 0;
  const shown = list.data?.tasks?.length ?? 0;
  return (
    <div id={id} className="mt-2 border-t border-border">
      <div className="flex flex-wrap gap-1 px-4 py-2" role="group">
        {FILTER_CHIPS.map((chip) => (
          <Button
            key={chip.filter}
            size="sm"
            variant={filter === chip.filter ? "default" : "outline"}
            className="h-7 cursor-pointer"
            aria-pressed={filter === chip.filter}
            onClick={() => onFilter(chip.filter)}
          >
            {t(chip.labelKey)}
          </Button>
        ))}
      </div>
      <OverviewListBody state={list.loadState} empty={total === 0}>
        <OverviewTaskTable rows={list.data?.tasks ?? []} />
        {total > shown && (
          <div className="flex items-center gap-2 px-4 py-2 text-xs text-muted-foreground">
            {t("office:overviewShowingOf", { shown, total })}
            {limit < TASK_LIST_ALL_LIMIT && (
              <Button
                variant="link"
                size="sm"
                className="h-auto cursor-pointer p-0"
                onClick={onShowAll}
              >
                {t("office:overviewShowAll")}
              </Button>
            )}
          </div>
        )}
      </OverviewListBody>
    </div>
  );
}
