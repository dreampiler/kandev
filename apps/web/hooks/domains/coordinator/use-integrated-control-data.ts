import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useAppStore, useAppStoreApi } from "@/components/state-provider";
import { readJourneyWorkspaces } from "@/hooks/journey-metadata-resources";
import { useForegroundRefresh } from "@/hooks/use-foreground-refresh";
import { listCoordinators, type Coordinator } from "@/lib/api/domains/coordinator-api";
import { mapWorkspaceItem } from "@/lib/routing/route-bootstrap";
import { stateReadScopeIdentity } from "@/lib/state/shared-resource-reads";
import type { WorkspaceState } from "@/lib/state/slices/workspace/types";
import { hasScope, SCOPE } from "@/lib/types/team-access";
import { useWebSocketClient } from "@/lib/ws/connection";
import {
  readControlConversationLists,
  type ControlConversationLists,
} from "./control-conversation-lists";

export type ControlWorkspace = WorkspaceState["items"][number];
export type ControlCoordinatorList = { items: Coordinator[]; error: boolean };
type Snapshot = {
  identity: string;
  owner: object;
  workspaces: ControlWorkspace[];
  byWorkspace: Record<string, ControlCoordinatorList>;
  conversationsByWorkspace: Record<string, ControlConversationLists>;
};

async function readCoordinatorLists(workspaces: ControlWorkspace[], signal: AbortSignal) {
  const rows = await Promise.all(
    workspaces.map(async (workspace): Promise<[string, ControlCoordinatorList]> => {
      try {
        const result = await listCoordinators(workspace.id, {
          cache: "no-store",
          init: { signal },
        });
        return [workspace.id, { items: result.coordinators, error: false }];
      } catch {
        return [workspace.id, { items: [], error: true }];
      }
    }),
  );
  return Object.fromEntries(rows);
}

/** One authorized read lifetime shared by the route and both navigation surfaces. */
export function useIntegratedControlData(enabled: boolean, coordinatorEnabled: boolean) {
  const store = useAppStoreApi();
  const identity = useAppStore(stateReadScopeIdentity);
  const owner = useMemo(() => ({}), [enabled, coordinatorEnabled, identity]);
  const ws = useWebSocketClient();
  const [snapshot, setSnapshot] = useState<Snapshot>();
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState(false);
  const request = useRef<AbortController | null>(null);
  const refresh = useCallback(async () => {
    if (!enabled) return;
    request.current?.abort();
    const controller = new AbortController();
    request.current = controller;
    const current = () =>
      !controller.signal.aborted && stateReadScopeIdentity(store.getState()) === identity;
    setLoading(true);
    setError(false);
    try {
      const result = await readJourneyWorkspaces(store, {
        signal: controller.signal,
        refresh: true,
      });
      if (!current()) return;
      const workspaces = result.workspaces
        .map(mapWorkspaceItem)
        .filter((item) => hasScope(item.scopes, SCOPE.workspaceRead));
      store.getState().setWorkspaces(result.workspaces.map(mapWorkspaceItem));
      const [byWorkspace, conversationsByWorkspace] = await Promise.all([
        coordinatorEnabled
          ? readCoordinatorLists(workspaces, controller.signal)
          : Promise.resolve({}),
        readControlConversationLists(workspaces, controller.signal),
      ]);
      if (current())
        setSnapshot({ identity, owner, workspaces, byWorkspace, conversationsByWorkspace });
    } catch {
      if (current()) {
        setSnapshot(undefined);
        setError(true);
      }
    } finally {
      if (current()) setLoading(false);
    }
  }, [enabled, coordinatorEnabled, store, identity, owner]);

  useEffect(() => {
    if (!enabled) return;
    void refresh();
    return () => request.current?.abort();
  }, [enabled, refresh]);
  useForegroundRefresh(refresh, enabled, identity);
  useEffect(() => {
    if (!enabled || !ws) return;
    const removers = [
      ws.on("coordinator.updated", () => void refresh()),
      ws.on("workspace.updated", () => void refresh()),
      ws.on("workspace.deleted", () => void refresh()),
      ws.on("task.created", () => void refresh()),
      ws.on("task.updated", () => void refresh()),
      ws.on("task.deleted", () => void refresh()),
      ws.on("session.state_changed", ({ payload }) => {
        if (payload.new_state === "CREATED") void refresh();
      }),
    ];
    return () => removers.forEach((remove) => remove());
  }, [enabled, ws, refresh]);

  const data =
    enabled && snapshot?.identity === identity && snapshot.owner === owner ? snapshot : undefined;
  return { data, loading, error, refresh };
}
