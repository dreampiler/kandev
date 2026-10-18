import { ControlTaskActivity } from "./control-task-activity";
import { ControlProviderUsage } from "./control-provider-usage";
import { linkToTasks } from "@/lib/links";
import { ControlCapacitySnapshot } from "./control-capacity-snapshot";
import { useTranslation } from "react-i18next";
import { formatDateTime } from "@/lib/i18n/formats";
import Link from "@/components/routing/app-link";
import { controlHref, type ControlView } from "@/lib/navigation/integrated-control";
import { hasScope, SCOPE } from "@/lib/types/team-access";
import { useIntegratedControl } from "./integrated-control-provider";
import {
  controlTaskTotal,
  type ControlTaskCount,
} from "@/hooks/domains/coordinator/control-conversation-lists";

export function IntegratedControlDashboard({ view }: { view: ControlView }) {
  const { t } = useTranslation();
  const control = useIntegratedControl();
  if (!control?.data) return null;
  const { workspaces, byWorkspace, conversationsByWorkspace } = control.data;
  const rows = workspaces.flatMap((workspace) =>
    (byWorkspace[workspace.id]?.items ?? []).map((coordinator) => ({ workspace, coordinator })),
  );
  return (
    <div
      className="min-h-0 flex-1 space-y-5 overflow-y-auto p-4 md:p-6"
      data-testid={`control-dashboard-${view}`}
    >
      {view === "overview" && (
        <ControlOverview
          data={control.data}
          rows={rows}
          coordinatorEnabled={control.coordinatorEnabled}
        />
      )}
      {view === "history" && <UnavailableHistory />}
      {(view === "workspaces" || view === "overview") && (
        <div className="grid gap-3">
          {workspaces.map((workspace) => (
            <section key={workspace.id} className="rounded-lg border p-4">
              <h2 className="font-medium">
                <Link
                  className="inline-flex min-h-7 items-center underline [@media(pointer:coarse)]:min-h-11"
                  href={controlHref("workspaces", workspace.id)}
                >
                  {workspace.name}
                </Link>
              </h2>
              {workspace.description && (
                <p className="mt-1 whitespace-pre-wrap text-sm text-muted-foreground">
                  {workspace.description}
                </p>
              )}
              <p className="mt-2 text-xs text-muted-foreground">
                {t(
                  hasScope(workspace.scopes, SCOPE.workspaceManage)
                    ? "sidebar:integratedControl.manageAccess"
                    : "sidebar:integratedControl.readAccess",
                )}
              </p>
              <WorkspaceTaskCount count={conversationsByWorkspace[workspace.id]?.tasks} />
              {byWorkspace[workspace.id]?.error && (
                <p role="alert" className="mt-2 text-sm text-destructive">
                  {t("sidebar:integratedControl.loadError")}
                </p>
              )}
            </section>
          ))}
        </div>
      )}
      {view === "activity" && (
        <>
          <ControlCapacitySnapshot observation={control.data.capacity} />
          <ControlTaskActivity />
          <CoordinatorFacts view={view} rows={rows} />
        </>
      )}
      {view === "providers" && <ControlProviderUsage />}
      {!control.coordinatorEnabled && (
        <p role="status" className="text-sm text-muted-foreground">
          {t("sidebar:integratedControl.coordinatorsOff")}
        </p>
      )}
      {hasPartialControlData(control.data) && (
        <p role="alert" className="text-sm text-destructive">
          {t("sidebar:integratedControl.partialData")}
        </p>
      )}
      {workspaces.length === 0 && (
        <p className="text-sm text-muted-foreground">
          {t("sidebar:integratedControl.noWorkspaces")}
        </p>
      )}
    </div>
  );
}

type ControlData = NonNullable<NonNullable<ReturnType<typeof useIntegratedControl>>["data"]>;

function ControlOverview({
  data,
  rows,
  coordinatorEnabled,
}: {
  data: ControlData;
  rows: FactRows;
  coordinatorEnabled: boolean;
}) {
  const { t } = useTranslation();
  const unknown = t("common:unknown");
  const unavailable = t("sidebar:integratedControl.unavailable");
  const { workspaces, byWorkspace, conversationsByWorkspace } = data;
  const taskTotal = controlTaskTotal(workspaces, conversationsByWorkspace);
  const independentCount = workspaces.reduce<number | null>((count, workspace) => {
    const list = conversationsByWorkspace[workspace.id]?.independent;
    if (count === null || !list || list.error || list.denied) return null;
    return count + new Set(list.items.map((item) => item.task_id)).size;
  }, 0);
  const coordinatorsKnown = workspaces.every(
    (workspace) => byWorkspace[workspace.id] && !byWorkspace[workspace.id].error,
  );
  let coordinatorCount: number | string = unavailable;
  let currentCount: number | string = unavailable;
  if (coordinatorEnabled) {
    coordinatorCount = coordinatorsKnown ? rows.length : unknown;
    currentCount = coordinatorsKnown
      ? rows.filter((row) => row.coordinator.conversation_task_id).length
      : unknown;
  }
  return (
    <>
      <ControlCapacitySnapshot observation={data.capacity} />
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <Fact label={t("sidebar:integratedControl.workspaces")} value={workspaces.length} />
        <Fact label={t("sidebar:integratedControl.unarchivedTasks")} value={taskTotal ?? unknown} />
        <Fact
          label={t("sidebar:integratedControl.independentConversations")}
          value={independentCount ?? unknown}
        />
        <Fact label={t("sidebar:integratedControl.coordinators")} value={coordinatorCount} />
        <Fact label={t("sidebar:controlInspection.nativeConversations")} value={currentCount} />
      </div>
    </>
  );
}

function UnavailableHistory() {
  const { t } = useTranslation();
  return (
    <section className="space-y-3">
      <p role="status" className="text-sm text-muted-foreground">
        {t("sidebar:controlInspection.historyHelp")}
      </p>
      <Link
        className="inline-flex min-h-7 items-center underline [@media(pointer:coarse)]:min-h-11"
        href={linkToTasks()}
      >
        {t("sidebar:controlInspection.openTasks")}
      </Link>
      <ControlTaskActivity />
    </section>
  );
}

function hasPartialControlData(
  data: NonNullable<NonNullable<ReturnType<typeof useIntegratedControl>>["data"]>,
) {
  return (
    Object.values(data.byWorkspace).some((list) => list.error) ||
    Object.values(data.conversationsByWorkspace).some(
      (list) => list.independent.error || list.tasks?.error,
    )
  );
}

function WorkspaceTaskCount({ count }: { count?: ControlTaskCount }) {
  const { t } = useTranslation();
  return (
    <>
      <p className="mt-2 text-sm">
        {t("sidebar:integratedControl.unarchivedTasks")}: {count?.total ?? t("common:unknown")}
      </p>
      {count?.error && (
        <p role="alert" className="mt-2 text-sm text-destructive">
          {t(
            count.denied
              ? "sidebar:integratedControl.readDenied"
              : "sidebar:integratedControl.loadError",
          )}
        </p>
      )}
    </>
  );
}

function Fact({ label, value }: { label: string; value: number | string }) {
  return (
    <section className="rounded-lg border p-4">
      <h2 className="text-sm text-muted-foreground">{label}</h2>
      <p className="mt-2 text-2xl font-semibold">{value}</p>
    </section>
  );
}

type FactRows = Array<{
  workspace: NonNullable<
    NonNullable<ReturnType<typeof useIntegratedControl>>["data"]
  >["workspaces"][number];
  coordinator: NonNullable<
    NonNullable<ReturnType<typeof useIntegratedControl>>["data"]
  >["byWorkspace"][string]["items"][number];
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
          {view === "activity" && (
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
          )}
          {view === "providers" && (
            <dl className="space-y-1 break-all text-sm">
              <div>
                <dt className="inline text-muted-foreground">
                  {t("sidebar:integratedControl.agentProfile")}:{" "}
                </dt>
                <dd className="inline">{coordinator.agent_profile_id}</dd>
              </div>
              <div>
                <dt className="inline text-muted-foreground">
                  {t("sidebar:integratedControl.executorProfile")}:{" "}
                </dt>
                <dd className="inline">{coordinator.executor_profile_id}</dd>
              </div>
              <div>
                <dt className="inline text-muted-foreground">
                  {t("sidebar:integratedControl.views.providers")}:{" "}
                </dt>
                <dd className="inline">{t("sidebar:integratedControl.unknown")}</dd>
              </div>
            </dl>
          )}
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
      {rows.length === 0 && (
        <p className="text-sm text-muted-foreground">
          {t("sidebar:integratedControl.noCoordinators")}
        </p>
      )}
    </div>
  );
}
