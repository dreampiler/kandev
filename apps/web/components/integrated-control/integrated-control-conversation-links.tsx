import { useTranslation } from "react-i18next";
import { IconMessage } from "@tabler/icons-react";
import Link from "@/components/routing/app-link";
import { controlHref, type ControlView } from "@/lib/navigation/integrated-control";
import { useSearchParams } from "@/lib/routing/client-router";
import type { Coordinator } from "@/lib/api/domains/coordinator-api";
import type { ControlCollection } from "@/hooks/domains/coordinator/control-conversation-lists";
import { useIntegratedControl } from "./integrated-control-provider";
import { ControlReadState } from "./control-read-state";

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
  collection?: Pick<ControlCollection<unknown>, "error" | "denied" | "stale" | "receivedAt">;
}) {
  if (!collection?.error && !collection?.denied) return null;
  return (
    <ControlReadState
      status={collection.denied ? "denied" : "error"}
      stale={collection.stale}
      receivedAt={collection.receivedAt}
    />
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
  grouped = true,
}: {
  workspaceId: string;
  view: ControlView;
  onNavigate?: () => void;
  grouped?: boolean;
}) {
  const { t } = useTranslation();
  const control = useIntegratedControl();
  const collection = control?.data?.conversationsByWorkspace[workspaceId]?.independent;
  const content = (
    <>
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
    </>
  );
  return grouped ? (
    <details open className="min-w-0">
      <summary className="cursor-pointer truncate px-2 py-1.5 text-sm [@media(pointer:coarse)]:min-h-11">
        {t("sidebar:integratedControl.independentConversations")}
      </summary>
      {content}
    </details>
  ) : (
    <div className="min-w-0" data-testid="control-direct-conversations">
      {content}
    </div>
  );
}
