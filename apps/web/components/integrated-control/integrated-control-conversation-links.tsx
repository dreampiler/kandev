import { useTranslation } from "react-i18next";
import { IconMessage } from "@tabler/icons-react";
import Link from "@/components/routing/app-link";
import { controlHref, type ControlView } from "@/lib/navigation/integrated-control";
import { useSearchParams } from "@/lib/routing/client-router";
import type { Coordinator } from "@/lib/api/domains/coordinator-api";
import type { ControlCollection } from "@/hooks/domains/coordinator/control-conversation-lists";
import { useIntegratedControl } from "./integrated-control-provider";

function ConversationLink({
  workspaceId,
  coordinatorId,
  taskId,
  current,
  label,
  view,
  onNavigate,
}: {
  workspaceId: string;
  coordinatorId?: string;
  taskId: string;
  current?: boolean;
  label: string;
  view: ControlView;
  onNavigate?: () => void;
}) {
  const query = useSearchParams();
  const selected =
    query.get("workspace") === workspaceId &&
    (query.get("coordinator") ?? undefined) === coordinatorId &&
    (query.get("task") === taskId || (!query.has("task") && current));
  return (
    <Link
      href={controlHref(view, workspaceId, coordinatorId, current ? undefined : taskId)}
      onNavigated={onNavigate}
      aria-current={selected ? "page" : undefined}
      title={label}
      className="ml-3 flex min-h-7 cursor-pointer min-w-0 items-center gap-2 rounded-md px-2 py-1.5 text-xs hover:bg-muted aria-[current=page]:bg-accent [@media(pointer:coarse)]:min-h-11"
      data-testid={`control-conversation-${current ? coordinatorId : taskId}`}
    >
      <IconMessage className="size-3.5 shrink-0" aria-hidden />
      <span className="truncate">{label}</span>
    </Link>
  );
}

function CollectionNotice({
  collection,
}: {
  collection?: Pick<ControlCollection<unknown>, "error" | "denied">;
}) {
  const { t } = useTranslation();
  if (!collection?.error && !collection?.denied) return null;
  return (
    <p role="alert" className="px-2 py-1.5 text-xs text-destructive">
      {t(
        collection.denied
          ? "sidebar:integratedControl.readDenied"
          : "sidebar:integratedControl.loadError",
      )}
    </p>
  );
}

export function ControlCoordinatorConversations({
  workspaceId,
  coordinator,
  view,
  onNavigate,
}: {
  workspaceId: string;
  coordinator: Coordinator;
  view: ControlView;
  onNavigate?: () => void;
}) {
  const { t } = useTranslation();
  return (
    <>
      {coordinator.conversation_task_id ? (
        <ConversationLink
          workspaceId={workspaceId}
          coordinatorId={coordinator.id}
          taskId={coordinator.conversation_task_id}
          current
          view={view}
          onNavigate={onNavigate}
          label={t("sidebar:integratedControl.currentConversation")}
        />
      ) : (
        <p className="px-5 py-1.5 text-xs text-muted-foreground">
          {t("sidebar:integratedControl.noConversation")}
        </p>
      )}
    </>
  );
}

export function ControlIndependentConversations({
  workspaceId,
  view,
  onNavigate,
}: {
  workspaceId: string;
  view: ControlView;
  onNavigate?: () => void;
}) {
  const { t } = useTranslation();
  const control = useIntegratedControl();
  const collection = control?.data?.conversationsByWorkspace[workspaceId]?.independent;
  return (
    <details open className="min-w-0">
      <summary className="cursor-pointer truncate px-2 py-1.5 text-sm [@media(pointer:coarse)]:min-h-11">
        {t("sidebar:integratedControl.independentConversations")}
      </summary>
      <CollectionNotice collection={collection} />
      {collection?.items.map((item) => (
        <ConversationLink
          key={item.task_id}
          workspaceId={workspaceId}
          taskId={item.task_id}
          view={view}
          onNavigate={onNavigate}
          label={item.name || t("sidebar:integratedControl.unnamedConversation")}
        />
      ))}
      {collection && !collection.error && collection.items.length === 0 && (
        <p className="px-5 py-1.5 text-xs text-muted-foreground">
          {t("sidebar:integratedControl.noIndependentConversations")}
        </p>
      )}
    </details>
  );
}
