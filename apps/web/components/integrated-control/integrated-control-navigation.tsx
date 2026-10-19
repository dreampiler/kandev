import { useTranslation } from "react-i18next";
import {
  IconActivity,
  IconBuilding,
  IconHistory,
  IconLayoutDashboard,
  IconTopologyStar,
} from "@tabler/icons-react";
import Link from "@/components/routing/app-link";
import { Button } from "@kandev/ui/button";
import { usePathname } from "@/lib/routing/client-router";
import {
  CONTROL_VIEWS,
  controlHref,
  controlView,
  type ControlView,
} from "@/lib/navigation/integrated-control";
import { cn } from "@/lib/utils";
import { useIntegratedControl } from "./integrated-control-provider";
import {
  ControlCoordinatorConversations,
  ControlIndependentConversations,
  independentVisible,
} from "./integrated-control-conversation-links";
import { ControlStatusPill } from "./control-status-pill";
import { useControlSignals } from "./use-control-signals";
import type { WorkspaceStatus } from "./control-signals";
import type {
  ControlCoordinatorList,
  ControlWorkspace,
} from "@/hooks/domains/coordinator/use-integrated-control-data";
import { ControlReadState, ControlSnapshotReadState } from "./control-read-state";

const icons = [IconLayoutDashboard, IconActivity, IconBuilding, IconTopologyStar, IconHistory];

export function IntegratedControlNavigation({
  collapsed = false,
  onNavigate,
}: {
  collapsed?: boolean;
  onNavigate?: () => void;
}) {
  const { t } = useTranslation();
  const control = useIntegratedControl();
  const view = controlView(usePathname());
  if (!control?.active) return null;
  return (
    <div
      className="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto p-2"
      data-testid="integrated-control-navigation"
    >
      <div className="flex flex-col gap-1">
        {CONTROL_VIEWS.map((item, index) => {
          const Icon = icons[index];
          const label = t(`sidebar:integratedControl.views.${item}`);
          return (
            <Link
              key={item}
              href={controlHref(item)}
              title={label}
              aria-label={collapsed ? label : undefined}
              aria-current={view === item ? "page" : undefined}
              onNavigated={onNavigate}
              className={cn(
                "flex min-h-7 cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 text-sm [@media(pointer:coarse)]:min-h-11",
                view === item ? "bg-accent font-medium" : "hover:bg-muted",
              )}
              data-testid={`control-nav-${item}`}
            >
              <Icon className="size-4 shrink-0" aria-hidden />
              {!collapsed && <span>{label}</span>}
            </Link>
          );
        })}
      </div>
      {!collapsed && <ControlWorkspaceTree onNavigate={onNavigate} />}
    </div>
  );
}

function ControlWorkspaceTree({ onNavigate }: { onNavigate?: () => void }) {
  const { t } = useTranslation();
  const control = useIntegratedControl();
  const view = controlView(usePathname());
  const signals = useControlSignals({ tasks: false, history: false });
  if (!control) return null;
  const data = control.data;
  const conversations = data?.workspaces.map(
    (workspace) => data.conversationsByWorkspace[workspace.id]?.independent,
  );
  const noIndependent =
    Boolean(conversations?.length) &&
    conversations!.every((list) => list && !list.error && !list.denied && list.items.length === 0);
  return (
    <section
      className="space-y-1 border-t pt-3"
      aria-label={t("sidebar:integratedControl.workspaces")}
    >
      <div className="flex items-center justify-between gap-2 px-2">
        <h2 className="text-xs font-medium text-muted-foreground">
          {t("sidebar:integratedControl.workspaces")}
        </h2>
        <Button
          variant="ghost"
          size="sm"
          disabled={control.loading}
          onClick={() => void control.refresh()}
        >
          {t("sidebar:integratedControl.refresh")}
        </Button>
      </div>
      {control.error && <ControlSnapshotReadState />}
      {!control.coordinatorEnabled && (
        <p className="px-2 text-xs text-muted-foreground">
          {t("sidebar:integratedControl.coordinatorsOff")}
        </p>
      )}
      {data?.workspaces.map((workspace) => (
        <ControlWorkspaceEntry
          key={workspace.id}
          workspace={workspace}
          status={signals.statusById[workspace.id]}
          view={view}
          onNavigate={onNavigate}
        />
      ))}
      {noIndependent && (
        <p className="px-2 pt-1 text-xs text-muted-foreground" data-testid="control-no-independent">
          {t("sidebar:controlSummary.nav.noIndependent")}
        </p>
      )}
      {data?.workspaces.length === 0 && (
        <p className="px-2 text-sm text-muted-foreground">
          {t("sidebar:integratedControl.noWorkspaces")}
        </p>
      )}
    </section>
  );
}

function ControlWorkspaceEntry({
  workspace,
  status,
  view,
  onNavigate,
}: {
  workspace: ControlWorkspace;
  status?: WorkspaceStatus;
  view: ControlView;
  onNavigate?: () => void;
}) {
  const data = useIntegratedControl()?.data;
  const coordinators = data?.byWorkspace[workspace.id];
  const chip = status && status !== "active" ? <ControlStatusPill state={status} /> : null;
  const nested =
    Boolean(coordinators?.error || coordinators?.items.length) ||
    independentVisible(data?.conversationsByWorkspace[workspace.id]?.independent);
  if (!nested)
    return (
      <Link
        href={controlHref("workspaces", workspace.id)}
        onNavigated={onNavigate}
        className="flex min-h-7 cursor-pointer items-center justify-between gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-muted [@media(pointer:coarse)]:min-h-11"
        data-testid={`control-workspace-${workspace.id}`}
      >
        <span className="truncate">{workspace.name}</span>
        {chip}
      </Link>
    );
  return (
    <details open className="min-w-0 rounded-md" data-testid={`control-workspace-${workspace.id}`}>
      <summary className="flex cursor-pointer items-center justify-between gap-2 px-2 py-1.5 text-sm font-medium [@media(pointer:coarse)]:min-h-11">
        <span className="truncate">{workspace.name}</span>
        {chip}
      </summary>
      <WorkspaceChildren
        workspaceId={workspace.id}
        coordinators={coordinators}
        view={view}
        onNavigate={onNavigate}
      />
    </details>
  );
}

function WorkspaceChildren({
  workspaceId,
  coordinators,
  view,
  onNavigate,
}: {
  workspaceId: string;
  coordinators?: ControlCoordinatorList;
  view: ControlView;
  onNavigate?: () => void;
}) {
  return (
    <div className="ml-3 border-l pl-2">
      {coordinators?.error && <ControlReadState status="error" {...coordinators} />}
      {coordinators?.items.map((coordinator) => (
        <details key={coordinator.id} open className="min-w-0">
          <summary className="cursor-pointer truncate px-2 py-1.5 text-sm [@media(pointer:coarse)]:min-h-11">
            {coordinator.name}
          </summary>
          <ControlCoordinatorConversations
            workspaceId={workspaceId}
            coordinator={coordinator}
            view={view}
            onNavigate={onNavigate}
          />
        </details>
      ))}
      <ControlIndependentConversations
        workspaceId={workspaceId}
        grouped={Boolean(coordinators?.items.length)}
        view={view}
        onNavigate={onNavigate}
      />
    </div>
  );
}
