"use client";

import { useMemo } from "react";
import { Card } from "@kandev/ui/card";
import { useAppStore } from "@/components/state-provider";
import { selectWorkspaceAggregate } from "@/lib/state/slices/office/selectors";
import type { WorkspaceAggregate } from "@/lib/state/slices/office/types";
import type { OverviewSections } from "@/lib/state/slices/office/overview-types";
import { ActivityRow } from "@/app/office/workspace/activity/activity-row";
import { useWorkspaceAggregate } from "@/hooks/domains/office/use-workspace-aggregate";
import { useTranslation } from "react-i18next";
import { OverviewHeader } from "./overview-header";
import { OverviewSystemCards } from "./overview-system-cards";
import { OverviewLast24h, OverviewModels, OverviewNeedsHuman } from "./overview-sections";
import { OverviewWorkspaceCard } from "./overview-workspace-card";

/**
 * Read-only multi-workspace overview: system cards with expandable running
 * lists, items waiting on a person, one card per workspace in the caller's
 * scope (with an expandable task list), the last 24 hours, models, and the
 * merged recent-activity feed. Every list loads only while expanded and every
 * refresh pauses while the page is hidden.
 */
export function WorkspaceAggregatePageClient() {
  const { t } = useTranslation();
  const aggregate = useAppStore(selectWorkspaceAggregate);
  const { loadState, refresh } = useWorkspaceAggregate();

  return (
    <div className="space-y-4 p-6">
      <OverviewHeader onScopeChanged={() => void refresh()} />
      {loadState === "loading" && (
        <div className="text-sm text-muted-foreground" role="status">
          {t("common:loading")}
        </div>
      )}
      {loadState === "error" && (
        <div className="text-sm text-destructive" role="alert">
          {t("office:failedToLoad")}
        </div>
      )}
      {loadState === "loaded" && <OverviewBody aggregate={aggregate} />}
    </div>
  );
}

function OverviewBody({ aggregate }: { aggregate: WorkspaceAggregate | null }) {
  const workspaces = useMemo(() => aggregate?.workspaces ?? [], [aggregate]);
  const sections = aggregate?.sections;
  const workspaceNames = useMemo(
    () => Object.fromEntries(workspaces.map((w) => [w.workspace_id, w.name])),
    [workspaces],
  );
  return (
    <>
      {sections?.system && <OverviewSystemCards system={sections.system} />}
      {sections?.system && <OverviewNeedsHuman items={sections.needs_human ?? []} />}
      <WorkspaceCards workspaces={workspaces} />
      {sections?.system && <LowerSections sections={sections} workspaceNames={workspaceNames} />}
      <RecentActivity entries={aggregate?.recentActivity ?? []} />
    </>
  );
}

function WorkspaceCards({ workspaces }: { workspaces: WorkspaceAggregate["workspaces"] }) {
  const { t } = useTranslation();
  if (workspaces.length === 0) {
    return <div className="text-sm text-muted-foreground">{t("office:noWorkspaces")}</div>;
  }
  return (
    <div className="grid gap-3">
      {workspaces.map((workspace) => (
        <OverviewWorkspaceCard key={workspace.workspace_id} workspace={workspace} />
      ))}
    </div>
  );
}

function LowerSections({
  sections,
  workspaceNames,
}: {
  sections: OverviewSections;
  workspaceNames: Record<string, string>;
}) {
  return (
    <>
      <OverviewLast24h events={sections.last_24h ?? []} workspaceNames={workspaceNames} />
      <OverviewModels models={sections.models ?? []} blocked={sections.blocked_accounts ?? []} />
    </>
  );
}

function RecentActivity({ entries }: { entries: WorkspaceAggregate["recentActivity"] }) {
  const { t } = useTranslation();
  return (
    <Card>
      <div className="p-4 border-b border-border">
        <h2 className="text-sm font-semibold">{t("office:recentActivity")}</h2>
      </div>
      <div className="divide-y divide-border">
        {entries.length === 0 ? (
          <div className="px-4 py-6 text-center text-sm text-muted-foreground">
            {t("office:noRecentActivityActionsByAgents")}
          </div>
        ) : (
          entries.map((entry) => <ActivityRow key={entry.id} entry={entry} />)
        )}
      </div>
    </Card>
  );
}
