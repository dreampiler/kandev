"use client";

import { useState, type ReactNode } from "react";
import Link from "@/components/routing/app-link";
import { Card } from "@kandev/ui/card";
import { Button } from "@kandev/ui/button";
import { IconChevronDown, IconChevronUp } from "@tabler/icons-react";
import { useTranslation } from "react-i18next";
import { useAppStore } from "@/components/state-provider";
import { getWorkspaceAggregateTasks } from "@/lib/api/domains/office-overview-api";
import { useOverviewList } from "@/hooks/domains/office/use-overview-list";
import { useWorkspaceOverview } from "@/hooks/domains/office/use-workspace-overview";
import { linkToTask } from "@/lib/links";
import { resolveHomeHref } from "@/lib/navigation/workspace-home";
import type { WorkspaceAggregateEntry } from "@/lib/state/slices/office/types";
import type {
  OverviewParentTask,
  OverviewTaskFilter,
  OverviewThresholds,
  OverviewWorkspaceMetrics,
} from "@/lib/state/slices/office/overview-types";
import { PROBLEM_STATUSES, reasonText, relativeTime } from "./overview-format";
import { OverviewListBody } from "./overview-system-cards";
import { OverviewStatusBadge, OverviewTaskTable } from "./overview-tables";
import { OverviewProblemBreakdown, OverviewProblemTooltip } from "./overview-problems";

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
      <div className="min-h-3 text-[11px] text-muted-foreground">{sub}</div>
    </>
  );
  const className = `block rounded-md p-2 text-center transition-colors hover:bg-muted/60 cursor-pointer ${
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
  return <div className="p-2 text-center">{body}</div>;
}

/**
 * One workspace card. Its own overview read drives the metrics and the problem
 * counts, so a workspace that is still computing shows what it is waiting for
 * instead of the roster's empty counts, and one slow workspace never blocks
 * another card.
 */
export function OverviewWorkspaceCard({
  workspace,
  refreshSeconds,
  thresholds,
}: {
  workspace: WorkspaceAggregateEntry;
  refreshSeconds: number;
  thresholds?: OverviewThresholds;
}) {
  const state = useWorkspaceOverview(workspace.workspace_id, refreshSeconds);
  if (state.status === "loading") {
    return <WorkspaceCardSkeleton name={workspace.name} />;
  }
  if (state.status === "error") {
    return <WorkspaceCardError workspace={workspace} />;
  }
  return (
    <WorkspaceCardBody
      workspace={workspace}
      detail={state.entry}
      refreshSeconds={refreshSeconds}
      thresholds={thresholds}
    />
  );
}

/**
 * The loaded card: the header, the metric chips, the parent and warning lines,
 * and the single detail disclosure. The workspace's own identity and name come
 * from the roster; only the metrics come from its per-workspace read, so the
 * roster cannot disagree with itself about which workspaces are on screen.
 */
function WorkspaceCardBody({
  workspace,
  detail,
  refreshSeconds,
  thresholds,
}: {
  workspace: WorkspaceAggregateEntry;
  detail: WorkspaceAggregateEntry;
  refreshSeconds: number;
  thresholds?: OverviewThresholds;
}) {
  const { t } = useTranslation();
  const startupPage = useAppStore((state) => state.userSettings.startupPage);
  const [filter, setFilter] = useState<OverviewTaskFilter | null>(null);
  const [limit, setLimit] = useState(TASK_LIST_LIMIT);
  const metrics = detail.metrics;
  const parents = detail.parents ?? [];
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
        {quiet && <span className="text-xs text-muted-foreground">{t("office:allClear")}</span>}
      </div>
      {metrics && !quiet && <WorkspaceMetrics metrics={metrics} filter={filter} onFilter={show} />}
      {metrics && !quiet && <ParentsAndWarning metrics={metrics} parents={parents} />}
      {metrics && !quiet && (
        <WorkspaceDetail
          id={listId}
          workspaceId={workspace.workspace_id}
          metrics={metrics}
          filter={filter}
          limit={limit}
          onFilter={show}
          onShowAll={() => setLimit(TASK_LIST_ALL_LIMIT)}
          refreshSeconds={refreshSeconds}
          thresholds={thresholds}
        />
      )}
      {quiet && <div className="pb-3" />}
    </Card>
  );
}

/** A card whose own overview read failed; the rest of the page is unaffected. */
function WorkspaceCardError({ workspace }: { workspace: WorkspaceAggregateEntry }) {
  const { t } = useTranslation();
  const startupPage = useAppStore((state) => state.userSettings.startupPage);
  const homeHref = resolveHomeHref({
    workspaceId: workspace.workspace_id,
    inOffice: Boolean(workspace.is_office),
    startupPage,
  });
  return (
    <Card className="p-0" data-testid="overview-workspace-card">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 px-4 py-3">
        <Link
          href={homeHref}
          className="font-medium hover:underline"
          data-testid="overview-workspace-link"
        >
          {workspace.name}
        </Link>
        <span className="text-xs text-destructive" role="alert">
          {t("office:failedToLoad")}
        </span>
      </div>
    </Card>
  );
}

/** A card shown while its own workspace overview is still being read. */
function WorkspaceCardSkeleton({ name }: { name: string }) {
  const { t } = useTranslation();
  return (
    <Card className="p-0" data-testid="overview-workspace-card">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 px-4 py-3">
        <span className="font-medium">{name}</span>
        <span
          className="text-xs text-muted-foreground"
          role="status"
          data-testid="overview-workspace-analyzing"
        >
          {t("office:overviewAnalyzing")}
        </span>
      </div>
    </Card>
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

function ParentsAndWarning({
  metrics,
  parents,
}: {
  metrics: OverviewWorkspaceMetrics;
  parents: OverviewParentTask[];
}) {
  const { t } = useTranslation();
  const warning = metrics.top_warning;
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

/**
 * The card's detail area: the problem counts, then the full problem list, then
 * the task list the metric chips drive. One disclosure opens all of it, so the
 * control that expands the list sits directly above the list it expands.
 */
function WorkspaceDetail({
  id,
  workspaceId,
  metrics,
  filter,
  limit,
  onFilter,
  onShowAll,
  refreshSeconds,
  thresholds,
}: {
  id: string;
  workspaceId: string;
  metrics: OverviewWorkspaceMetrics;
  filter: OverviewTaskFilter | null;
  limit: number;
  onFilter: (filter: OverviewTaskFilter) => void;
  onShowAll: () => void;
  refreshSeconds: number;
  thresholds?: OverviewThresholds;
}) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const problems = metrics.problems;
  const total = problems.error + problems.stalled + problems.delayed;

  return (
    <div className="mt-2 border-t border-border px-4 pb-3">
      <div className="flex flex-wrap items-center gap-2 pt-3">
        <Button
          variant="outline"
          size="sm"
          className="min-h-11 cursor-pointer gap-1.5 sm:min-h-0"
          aria-expanded={open}
          aria-controls={id}
          data-testid="overview-workspace-expand"
          onClick={() => setOpen((current) => !current)}
        >
          {open ? (
            <IconChevronUp className="h-3.5 w-3.5" aria-hidden="true" />
          ) : (
            <IconChevronDown className="h-3.5 w-3.5" aria-hidden="true" />
          )}
          {open ? t("office:overviewCollapseDetails") : t("office:overviewExpandDetails")}
        </Button>
        <OverviewProblemBreakdown problems={problems} />
        <OverviewProblemTooltip thresholds={thresholds} />
      </div>
      {open && (
        <div id={id} className="mt-2">
          <WorkspaceProblemList
            workspaceId={workspaceId}
            total={total}
            refreshSeconds={refreshSeconds}
          />
          <div className="mt-3 flex flex-wrap gap-1" role="group">
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
          <WorkspaceTaskList
            workspaceId={workspaceId}
            filter={filter ?? "problems"}
            limit={limit}
            onShowAll={onShowAll}
            refreshSeconds={refreshSeconds}
          />
        </div>
      )}
    </div>
  );
}

/**
 * Every problem of one workspace, not a preview. The counts come from the same
 * classification the badges use, so the list and the numbers agree.
 */
function WorkspaceProblemList({
  workspaceId,
  total,
  refreshSeconds,
}: {
  workspaceId: string;
  total: number;
  refreshSeconds: number;
}) {
  const list = useOverviewList(
    `problems:${workspaceId}`,
    () =>
      getWorkspaceAggregateTasks(workspaceId, "problems", TASK_LIST_ALL_LIMIT, {
        cache: "no-store",
      }),
    refreshSeconds,
  );
  if (total === 0) {
    return (
      <div className="rounded-md border border-border px-3 py-2 text-xs text-muted-foreground">
        <ProblemListBody state={list.loadState} />
      </div>
    );
  }
  return (
    <div className="rounded-md border border-border" data-testid="overview-workspace-problems">
      <OverviewListBody state={list.loadState} empty={(list.data?.total ?? 0) === 0}>
        <OverviewTaskTable rows={(list.data?.tasks ?? []).filter(isProblemRow)} />
      </OverviewListBody>
    </div>
  );
}

function ProblemListBody({ state }: { state: "idle" | "loading" | "loaded" | "error" }) {
  const { t } = useTranslation();
  if (state === "loading" || state === "idle") {
    return (
      <span role="status" className="text-muted-foreground">
        {t("office:overviewAnalyzing")}
      </span>
    );
  }
  if (state === "error") {
    return (
      <span role="alert" className="text-destructive">
        {t("office:failedToLoad")}
      </span>
    );
  }
  return t("office:overviewNoProblems");
}

function isProblemRow(row: { status?: string }): boolean {
  return row.status !== undefined && PROBLEM_STATUSES.has(row.status as never);
}

function WorkspaceTaskList({
  workspaceId,
  filter,
  limit,
  onShowAll,
  refreshSeconds,
}: {
  workspaceId: string;
  filter: OverviewTaskFilter;
  limit: number;
  onShowAll: () => void;
  refreshSeconds: number;
}) {
  const { t } = useTranslation();
  const list = useOverviewList(
    `${workspaceId}:${filter}:${limit}`,
    () => getWorkspaceAggregateTasks(workspaceId, filter, limit, { cache: "no-store" }),
    refreshSeconds,
  );
  const total = list.data?.total ?? 0;
  const shown = list.data?.tasks?.length ?? 0;
  return (
    <div className="mt-2" data-testid="overview-workspace-task-list">
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
