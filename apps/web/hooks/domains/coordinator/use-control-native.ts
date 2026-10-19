import { useCallback } from "react";
import type { ApiRequestOptions } from "@/lib/api/client";
import {
  readNativeDashboard,
  readNativeHistory,
  readNativeLedger,
  readNativeTasks,
} from "@/lib/api/domains/control-native-api";
import type { OverviewTaskFilter } from "@/lib/types/control-overview";
import type { NativeArchive } from "@/lib/types/control-native";
import { useControlResource, controlReadFailure, retainControlRows } from "./use-control-resource";

function useWorkspaceReads<T>(
  workspaceIds: string[],
  revision: object | undefined,
  reader: (id: string, options: ApiRequestOptions) => Promise<T>,
) {
  const ids = JSON.stringify([...new Set(workspaceIds)].sort());
  const read = useCallback(
    (signal: AbortSignal) =>
      Promise.all(
        (JSON.parse(ids) as string[]).map(async (id) => {
          try {
            return {
              id,
              status: "ready" as const,
              data: await reader(id, { cache: "no-store", init: { signal } }),
            };
          } catch (error) {
            return { id, status: controlReadFailure(error) };
          }
        }),
      ),
    [ids, reader],
  );
  return useControlResource(read, revision, retainControlRows);
}
export const useNativeHistory = (ids: string[], revision: object | undefined) =>
  useWorkspaceReads(ids, revision, readNativeHistory);
export const useNativeLedger = (ids: string[], revision: object | undefined) =>
  useWorkspaceReads(ids, revision, readNativeLedger);
export const useNativeDashboards = (ids: string[], revision: object | undefined) =>
  useWorkspaceReads(ids, revision, readNativeDashboard);
const readAllActiveTasks = (id: string, options: ApiRequestOptions) =>
  readNativeTasks(id, "all", "active", -1, options);
/** Every unarchived task per workspace, for views that rank tasks across workspaces. */
export const useNativeTaskRows = (ids: string[], revision: object | undefined) =>
  useWorkspaceReads(ids, revision, readAllActiveTasks);
export function useNativeDashboard(id: string, revision: object | undefined) {
  const read = useCallback(
    (signal: AbortSignal) => readNativeDashboard(id, { cache: "no-store", init: { signal } }),
    [id],
  );
  return useControlResource(read, revision);
}
export function useNativeTasks(
  id: string,
  filter: OverviewTaskFilter,
  archive: NativeArchive,
  limit: number,
  revision: object | undefined,
) {
  const read = useCallback(
    (signal: AbortSignal) =>
      readNativeTasks(id, filter, archive, limit, { cache: "no-store", init: { signal } }),
    [id, filter, archive, limit],
  );
  return useControlResource(read, revision);
}
