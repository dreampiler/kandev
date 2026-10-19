import { useCallback } from "react";
import {
  getControlOverview,
  getControlWorkspaceOverview,
  getControlWorkspaceTasks,
} from "@/lib/api/domains/control-overview-api";
import type { OverviewTaskFilter } from "@/lib/types/control-overview";
import { useControlResource } from "./use-control-resource";

export function useControlOverview(revision: object | undefined, workspaceId?: string) {
  const read = useCallback(
    (signal: AbortSignal) => {
      const options = { cache: "no-store" as const, init: { signal } };
      return workspaceId
        ? getControlWorkspaceOverview(workspaceId, options)
        : getControlOverview(options);
    },
    [workspaceId],
  );
  return useControlResource(read, revision);
}

export function useControlWorkspaceOverview(workspaceId: string, revision: object | undefined) {
  const read = useCallback(
    (signal: AbortSignal) =>
      getControlWorkspaceOverview(workspaceId, { cache: "no-store", init: { signal } }),
    [workspaceId],
  );
  return useControlResource(read, revision);
}

export function useControlWorkspaceTasks(
  workspaceId: string,
  filter: OverviewTaskFilter | null,
  revision: object | undefined,
  limit = 50,
) {
  const read = useCallback(
    (signal: AbortSignal) =>
      getControlWorkspaceTasks(workspaceId, filter ?? "all", limit, {
        cache: "no-store",
        init: { signal },
      }),
    [workspaceId, filter, limit],
  );
  return useControlResource(read, filter ? revision : undefined);
}
