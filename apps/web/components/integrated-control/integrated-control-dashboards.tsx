import { ControlProviderUsage } from "./control-provider-usage";
import { ControlHistory } from "./control-history";
import { ControlWorkspaces } from "./control-workspaces";
import { ControlCapacityLine } from "./control-capacity-bars";
import { ControlBlockerChips } from "./control-blocker-chips";
import { ControlTaskBoard } from "./control-task-board";
import { ControlOverview } from "./control-overview";
import { ControlInbox } from "./control-inbox";
import { ControlWorkspaceCompare } from "./control-workspace-compare";
import { useControlSignals } from "./use-control-signals";
import { useTranslation } from "react-i18next";
import { formatDateTime } from "@/lib/i18n/formats";
import Link from "@/components/routing/app-link";
import { controlHref, type ControlView } from "@/lib/navigation/integrated-control";
import { useIntegratedControl } from "./integrated-control-provider";

export function IntegratedControlDashboard({
  view,
  workspaceId,
}: {
  view: ControlView;
  workspaceId?: string;
}) {
  const { t } = useTranslation();
  const control = useIntegratedControl();
  if (!control?.data) return null;
  const { workspaces, byWorkspace } = control.data;
  const rows = workspaces.flatMap((workspace) =>
    (byWorkspace[workspace.id]?.items ?? []).map((coordinator) => ({ workspace, coordinator })),
  );
  return (
    <div
      className="min-h-0 w-full min-w-0 flex-1 space-y-5 overflow-y-auto p-4 md:p-6 [scrollbar-gutter:stable]"
      data-testid={`control-dashboard-${view}`}
    >
      {view === "overview" && <ControlOverview />}
      {view === "inbox" && <ControlInbox />}
      {view === "history" && <ControlHistory workspaceId={workspaceId} />}
      {view === "workspaces" && <WorkspacesView workspaceId={workspaceId} />}
      {view === "activity" && (
        <>
          <ControlCapacityLine
            observation={control.data.capacity}
            lastConfirmed={control.data.lastConfirmedCapacity}
          />
          <ControlBlockerChips />
          <ControlTaskBoard />
          {rows.length > 0 && <CoordinatorFacts view={view} rows={rows} />}
        </>
      )}
      {view === "providers" && <ControlProviderUsage workspaceId={workspaceId} />}
      {workspaces.length === 0 && (
        <p className="text-sm text-muted-foreground">
          {t("sidebar:integratedControl.noWorkspaces")}
        </p>
      )}
    </div>
  );
}

/** The comparison table first; a selected workspace adds its detail card under it. */
function WorkspacesView({ workspaceId }: { workspaceId?: string }) {
  const signals = useControlSignals({ tasks: false, history: false });
  return (
    <>
      <ControlWorkspaceCompare signals={signals} workspaceId={workspaceId} />
      {workspaceId && <ControlWorkspaces workspaceId={workspaceId} />}
    </>
  );
}

type ControlData = NonNullable<NonNullable<ReturnType<typeof useIntegratedControl>>["data"]>;

type FactRows = Array<{
  workspace: ControlData["workspaces"][number];
  coordinator: ControlData["byWorkspace"][string]["items"][number];
}>;

function CoordinatorFacts({ view, rows }: { view: ControlView; rows: FactRows }) {
  const { t } = useTranslation();
  return (
    <div className="space-y-3">
      {rows.map(({ workspace, coordinator }) => (
        <section key={coordinator.id} className="space-y-2 rounded-lg border p-4">
          <h2 className="font-medium">
            {workspace.name} / {coordinator.name}
          </h2>
          <dl className="grid gap-1 text-sm">
            <div>
              <dt className="inline text-muted-foreground">
                {t("sidebar:integratedControl.pendingProposals")}:{" "}
              </dt>
              <dd className="inline">
                {coordinator.open_proposals ?? t("sidebar:integratedControl.unknown")}
              </dd>
            </div>
            <div>
              <dt className="inline text-muted-foreground">
                {t("sidebar:integratedControl.updated")}:{" "}
              </dt>
              <dd className="inline">
                <time dateTime={coordinator.updated_at}>
                  {formatDateTime(coordinator.updated_at)}
                </time>
              </dd>
            </div>
          </dl>
          {coordinator.conversation_task_id ? (
            <Link
              className="inline-flex min-h-7 cursor-pointer items-center gap-2 rounded text-sm underline underline-offset-4 [@media(pointer:coarse)]:min-h-11"
              href={controlHref(view, workspace.id, coordinator.id)}
            >
              {t("sidebar:integratedControl.currentConversation")}
              <span className="break-all text-xs text-muted-foreground">
                {coordinator.conversation_task_id}
              </span>
            </Link>
          ) : (
            <p className="text-sm text-muted-foreground">
              {t("sidebar:integratedControl.noConversation")}
            </p>
          )}
        </section>
      ))}
    </div>
  );
}
