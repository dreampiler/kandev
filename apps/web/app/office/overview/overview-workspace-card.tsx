"use client";

import { useState } from "react";
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
import { reasonText } from "./overview-format";
import { WorkspaceMetrics } from "./overview-metrics";
import { OverviewListBody } from "./overview-system-cards";
import { OverviewTaskTable } from "./overview-tables";
import { OverviewProblemBreakdown, OverviewProblemTooltip } from "./overview-problems";
import { OverviewStatusName } from "./overview-status-legend";
import { statusTextClass, type OverviewStatusTone } from "./overview-status-colors";

const TASK_LIST_LIMIT = 50;
const TASK_LIST_ALL_LIMIT = 500;

// Catalog keys for the filter chips, not copy. The completed chip is here
// because the 24-hour tile opens the completed list: without a chip the reader
// lands in that list with no way back and no sign of which filter is selected.
const FILTER_CHIPS: { filter: OverviewTaskFilter; labelKey: string }[] = [
  { filter: "problems", labelKey: "office:overviewFilterProblems" },
  { filter: "active", labelKey: "common:taskStateInProgress" },
  { filter: "completed", labelKey: "office:overviewCompleted24h" },
  { filter: "hold", labelKey: "office:projectStatusOnHold" },
  { filter: "all", labelKey: "office:all" },
];

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
  const active = !quiet && Boolean(metrics);
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
        {active ? (
          <>
            <OverviewStatusName status={metrics.status} className="text-xs" />
            <span className="text-[11px]">
              <ProblemToneLine problems={metrics.problems} />
            </span>
          </>
        ) : (
          <span className="text-xs text-muted-foreground">{t("office:allClear")}</span>
        )}
      </div>
      {active && metrics && (
        <WorkspaceActiveBody
          workspace={workspace}
          metrics={metrics}
          parents={parents}
          listId={listId}
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

/**
 * Everything a project card shows once it is known to have work: the metric
 * tiles, the parent and warning lines, and the one disclosure that opens the
 * lists. A card with nothing open stops at its name.
 */
function WorkspaceActiveBody({
  workspace,
  metrics,
  parents,
  listId,
  filter,
  limit,
  onFilter,
  onShowAll,
  refreshSeconds,
  thresholds,
}: {
  workspace: WorkspaceAggregateEntry;
  metrics: OverviewWorkspaceMetrics;
  parents: OverviewParentTask[];
  listId: string;
  filter: OverviewTaskFilter | null;
  limit: number;
  onFilter: (filter: OverviewTaskFilter) => void;
  onShowAll: () => void;
  refreshSeconds: number;
  thresholds?: OverviewThresholds;
}) {
  return (
    <>
      <WorkspaceMetrics metrics={metrics} filter={filter} onFilter={onFilter} />
      <ParentsAndWarning metrics={metrics} parents={parents} />
      <WorkspaceDetail
        id={listId}
        workspaceId={workspace.workspace_id}
        metrics={metrics}
        filter={filter}
        limit={limit}
        onFilter={onFilter}
        onShowAll={onShowAll}
        refreshSeconds={refreshSeconds}
        thresholds={thresholds}
      />
    </>
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

/** The worst state actually present, which is the one the count wears. */
function worstProblemTone(
  problems: OverviewWorkspaceMetrics["problems"],
): OverviewStatusTone | null {
  if (problems.error > 0) return "error";
  if (problems.stalled > 0) return "stalled";
  if (problems.delayed > 0) return "delayed";
  return null;
}

/**
 * The project name line's problem count, wearing the color of the worst state
 * actually present. A project with no problems shows nothing here rather than
 * a zero, because the expanded breakdown below already says so.
 */
function ProblemToneLine({ problems }: { problems: OverviewWorkspaceMetrics["problems"] }) {
  const { t } = useTranslation();
  const tone = worstProblemTone(problems);
  if (!tone) return null;
  const total = problems.error + problems.stalled + problems.delayed;
  return (
    <span className={statusTextClass(tone)}>
      {t("office:overviewProblemCount", { count: total })}
    </span>
  );
}

/**
 * The parent tasks and the one warning worth surfacing. Each line wears the
 * color of its own state, so a parent task that is on hold does not read like
 * one that is running.
 */

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
    <div className="px-4 pt-2">
      <div className="space-y-1 rounded-md border border-tile-border bg-tile p-2">
        {parents.map((parent) => (
          <Link
            key={parent.task_id}
            href={linkToTask(parent.task_id)}
            className="flex items-center gap-2 rounded-md px-1 py-0.5 text-xs hover:underline"
            data-testid="overview-parent-task"
          >
            <OverviewStatusName status={parent.status} />
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
            className="flex items-center gap-2 rounded-md border border-status-error/30 bg-status-error/5 px-1 py-0.5 text-xs hover:underline"
            data-testid="overview-warning"
          >
            <OverviewStatusName status={warning.status} />
            <span className={statusTextClass("error")}>
              {warning.task_title} · {reasonText(t, warning.reason)}
            </span>
          </Link>
        )}
      </div>
    </div>
  );
}

/**
 * The card's detail area: the problem counts, then the filter chips, then the
 * one list those chips select. One disclosure opens all of it, so the control
 * that expands the list sits directly above the list it expands, and a task
 * appears once no matter which chip is pressed. The problems filter reads every
 * problem, so the expanded list is the whole problem set rather than a preview
 * of it.
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
  /** The limits the backend applied on this read, shown by the criteria tooltip. */
  thresholds?: OverviewThresholds;
}) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const problems = metrics.problems;

  return (
    <div className="mt-2 border-t border-border bg-tile px-4 pb-3">
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
          <div className="flex flex-wrap gap-1" role="group">
            {FILTER_CHIPS.map((chip) => (
              <Button
                key={chip.filter}
                size="sm"
                variant={(filter ?? "problems") === chip.filter ? "default" : "outline"}
                className="h-7 cursor-pointer"
                aria-pressed={(filter ?? "problems") === chip.filter}
                onClick={() => onFilter(chip.filter)}
              >
                {t(chip.labelKey)}
              </Button>
            ))}
          </div>
          <WorkspaceTaskList
            workspaceId={workspaceId}
            filter={filter ?? "problems"}
            limit={(filter ?? "problems") === "problems" ? TASK_LIST_ALL_LIMIT : limit}
            onShowAll={onShowAll}
            refreshSeconds={refreshSeconds}
          />
        </div>
      )}
    </div>
  );
}

/**
 * The one list the filter chips select. The problems filter is read in full, so
 * the "n of m" line and its show-all control stay off it; every other filter is
 * read at the card's limit until the operator asks for the rest.
 */
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
      <OverviewListBody
        state={list.loadState}
        empty={total === 0}
        emptyLabel={filter === "problems" ? t("office:overviewNoProblems") : undefined}
      >
        <OverviewTaskTable
          rows={list.data?.tasks ?? []}
          timeColumnLabelKey={
            filter === "completed" ? "office:overviewCompletedAt" : "office:overviewTimeInStep"
          }
        />
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
