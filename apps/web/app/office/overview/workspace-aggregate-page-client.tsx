"use client";

import { useEffect, useRef } from "react";
import Link from "@/components/routing/app-link";
import { Card } from "@kandev/ui/card";
import { useAppStore } from "@/components/state-provider";
import { selectWorkspaceAggregate } from "@/lib/state/slices/office/selectors";
import { getWorkspaceAggregate } from "@/lib/api/domains/office-extended-api";
import { normalizeActivityEntry } from "@/lib/api/domains/office-activity-normalize";
import type { WorkspaceAggregateEntry } from "@/lib/state/slices/office/types";
import { ActivityRow } from "@/app/office/workspace/activity/activity-row";
import { useTranslation } from "react-i18next";

/**
 * Read-only multi-workspace overview: every workspace the owner can reach, with
 * compact task counts, plus a merged recent-activity feed. Selecting a row
 * switches the active workspace through the standard `?workspaceId=` bootstrap.
 */
export function WorkspaceAggregatePageClient() {
  const { t } = useTranslation();
  const aggregate = useAppStore(selectWorkspaceAggregate);
  const setWorkspaceAggregate = useAppStore((s) => s.setWorkspaceAggregate);
  const loadedRef = useRef(false);

  useEffect(() => {
    if (loadedRef.current) return;
    loadedRef.current = true;
    void getWorkspaceAggregate()
      .then((data) => {
        setWorkspaceAggregate({
          workspaces: data.workspaces ?? [],
          recentActivity: (data.recent_activity ?? []).map(normalizeActivityEntry),
        });
      })
      .catch(() => {
        setWorkspaceAggregate({ workspaces: [], recentActivity: [] });
      });
  }, [setWorkspaceAggregate]);

  const workspaces = aggregate?.workspaces ?? [];
  const activity = aggregate?.recentActivity ?? [];

  return (
    <div className="space-y-4 p-6">
      {workspaces.length === 0 ? (
        <div className="text-sm text-muted-foreground">{t("office:noWorkspaces")}</div>
      ) : (
        <div className="grid gap-3">
          {workspaces.map((workspace) => (
            <WorkspaceAggregateCard key={workspace.workspace_id} workspace={workspace} />
          ))}
        </div>
      )}

      <Card>
        <div className="p-4 border-b border-border">
          <h2 className="text-sm font-semibold">{t("office:recentActivity")}</h2>
        </div>
        <div className="divide-y divide-border">
          {activity.length === 0 ? (
            <div className="px-4 py-6 text-center text-sm text-muted-foreground">
              {t("office:noRecentActivityActionsByAgents")}
            </div>
          ) : (
            activity.map((entry) => <ActivityRow key={entry.id} entry={entry} />)
          )}
        </div>
      </Card>
    </div>
  );
}

function WorkspaceAggregateCard({ workspace }: { workspace: WorkspaceAggregateEntry }) {
  const { t } = useTranslation();
  return (
    <Link
      href={`/office?workspaceId=${encodeURIComponent(workspace.workspace_id)}`}
      className="cursor-pointer"
    >
      <Card className="block p-4 hover:bg-muted/50 transition-colors">
        <div className="flex flex-col gap-1 sm:flex-row sm:items-center sm:justify-between sm:gap-4">
          <span className="font-medium">{workspace.name}</span>
          <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground">
            <span>
              {t("office:tasks")}: {workspace.task_count}
            </span>
            <span>
              {t("office:inProgress")}: {workspace.in_progress_tasks}
            </span>
            <span>
              {t("office:openBlocked", {
                open: workspace.open_tasks,
                blocked: workspace.blocked_tasks,
              })}
            </span>
            <span>
              {t("office:pendingApprovals")}: {workspace.pending_approvals}
            </span>
          </div>
        </div>
      </Card>
    </Link>
  );
}
