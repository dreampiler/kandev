"use client";

import { useCallback, useMemo, useState } from "react";
import { useAppStore } from "@/components/state-provider";
import { selectWorkspaceAggregate } from "@/lib/state/slices/office/selectors";
import type { WorkspaceAggregate } from "@/lib/state/slices/office/types";
import type { OverviewSections, OverviewSort } from "@/lib/state/slices/office/overview-types";
import { OVERVIEW_REFRESH_SECONDS_DEFAULT } from "@/lib/settings/overview-refresh";
import { useWorkspaceAggregate } from "@/hooks/domains/office/use-workspace-aggregate";
import { useTranslation } from "react-i18next";
import { OverviewHeader } from "./overview-header";
import { OverviewSystemCards } from "./overview-system-cards";
import { OverviewLast24h, OverviewModels, OverviewNeedsHuman } from "./overview-sections";
import { OverviewWorkspaceCard } from "./overview-workspace-card";

/**
 * Read-only multi-workspace overview: system cards with expandable running
 * lists, items waiting on a person, one card per workspace in the caller's
 * scope, then models and the last 24 hours.
 *
 * Each workspace card runs its own overview read, so a workspace that is slow
 * to compute shows what it is waiting for while the others are already on
 * screen. Every refresh pauses while the page is hidden.
 */
export function WorkspaceAggregatePageClient() {
  const { t } = useTranslation();
  const aggregate = useAppStore(selectWorkspaceAggregate);
  const savedRefreshSeconds = useAppStore(
    (state) => state.userSettings.officeOverviewRefreshSeconds,
  );
  const sort = useAppStore((state) => state.userSettings.officeOverviewSort);
  const [refreshSeconds, setRefreshSeconds] = useState(
    savedRefreshSeconds || OVERVIEW_REFRESH_SECONDS_DEFAULT,
  );
  const { loadState, refresh, refreshing, receivedAt } = useWorkspaceAggregate(refreshSeconds);
  const onRefresh = useCallback(() => void refresh(), [refresh]);

  return (
    <div className="space-y-4 p-6">
      <OverviewHeader
        onScopeChanged={onRefresh}
        onRefresh={onRefresh}
        refreshing={refreshing}
        receivedAt={receivedAt}
        refreshSeconds={refreshSeconds}
        onRefreshSecondsChanged={setRefreshSeconds}
      />
      {loadState === "error" && (
        <div className="text-sm text-destructive" role="alert">
          {t("office:failedToLoad")}
        </div>
      )}
      <OverviewBody
        aggregate={aggregate}
        loading={loadState === "loading"}
        refreshSeconds={refreshSeconds}
        sort={sort}
      />
    </div>
  );
}

/**
 * The chosen order is applied to whatever cards are in hand, so a workspace
 * whose card arrives late takes its place in the order instead of waiting for
 * the whole list.
 */
function sortWorkspaces(
  workspaces: WorkspaceAggregate["workspaces"],
  sort: OverviewSort,
): WorkspaceAggregate["workspaces"] {
  if (sort === "name") return workspaces;
  const ordered = [...workspaces];
  if (sort === "recent") {
    ordered.sort((a, b) => {
      const left = a.metrics?.last_output_at ?? "";
      const right = b.metrics?.last_output_at ?? "";
      if (left !== right) return left < right ? 1 : -1;
      return a.name.localeCompare(b.name);
    });
    return ordered;
  }
  ordered.sort((a, b) => {
    const diff = problemTotal(b) - problemTotal(a);
    if (diff !== 0) return diff;
    return a.name.localeCompare(b.name);
  });
  return ordered;
}

function problemTotal(workspace: WorkspaceAggregate["workspaces"][number]): number {
  const problems = workspace.metrics?.problems;
  if (!problems) return 0;
  return (problems.error ?? 0) + (problems.stalled ?? 0) + (problems.delayed ?? 0);
}

function OverviewBody({
  aggregate,
  loading,
  refreshSeconds,
  sort,
}: {
  aggregate: WorkspaceAggregate | null;
  loading: boolean;
  refreshSeconds: number;
  sort: OverviewSort;
}) {
  const workspaces = useMemo(() => aggregate?.workspaces ?? [], [aggregate]);
  const ordered = useMemo(() => sortWorkspaces(workspaces, sort), [workspaces, sort]);
  const sections = aggregate?.sections;
  const workspaceNames = useMemo(
    () => Object.fromEntries(workspaces.map((w) => [w.workspace_id, w.name])),
    [workspaces],
  );
  const circuitsKnown = sections?.system?.blocked_accounts_total !== undefined;
  return (
    <>
      <OverviewSystemCards
        system={sections?.system}
        loading={loading}
        refreshSeconds={refreshSeconds}
      />
      {sections && !loading && <OverviewNeedsHuman items={sections.needs_human ?? []} />}
      <WorkspaceCards workspaces={ordered} refreshSeconds={refreshSeconds} />
      {sections && !loading && (
        <LowerSections
          sections={sections}
          workspaceNames={workspaceNames}
          circuitsKnown={circuitsKnown}
        />
      )}
    </>
  );
}

function WorkspaceCards({
  workspaces,
  refreshSeconds,
}: {
  workspaces: WorkspaceAggregate["workspaces"];
  refreshSeconds: number;
}) {
  const { t } = useTranslation();
  if (workspaces.length === 0) {
    return <div className="text-sm text-muted-foreground">{t("office:noWorkspaces")}</div>;
  }
  return (
    <div className="grid gap-3">
      {workspaces.map((workspace) => (
        <OverviewWorkspaceCard
          key={workspace.workspace_id}
          workspace={workspace}
          refreshSeconds={refreshSeconds}
        />
      ))}
    </div>
  );
}

function LowerSections({
  sections,
  workspaceNames,
  circuitsKnown,
}: {
  sections: OverviewSections;
  workspaceNames: Record<string, string>;
  circuitsKnown: boolean;
}) {
  return (
    <>
      <OverviewModels
        models={sections.models ?? []}
        blocked={sections.blocked_accounts ?? []}
        circuits={sections.blocked_circuits ?? []}
        circuitsKnown={circuitsKnown}
      />
      <OverviewLast24h events={sections.last_24h ?? []} workspaceNames={workspaceNames} />
    </>
  );
}
