import { fetchJson, type ApiRequestOptions } from "../client";
import type {
  OverviewListResponse,
  OverviewRunningKind,
  OverviewTaskFilter,
} from "@/lib/state/slices/office/overview-types";

const BASE = "/api/v1/office";

/** One workspace's overview task list for a filter (fetched only when expanded). */
export function getWorkspaceAggregateTasks(
  workspaceId: string,
  filter: OverviewTaskFilter,
  limit: number,
  options?: ApiRequestOptions,
) {
  const params = new URLSearchParams({ workspace_id: workspaceId, filter, limit: String(limit) });
  return fetchJson<OverviewListResponse>(
    `${BASE}/workspaces/aggregate/tasks?${params.toString()}`,
    options,
  );
}

/** Cross-workspace running tasks, running sessions, or queued messages. */
export function getWorkspaceAggregateRunning(
  kind: OverviewRunningKind,
  limit: number,
  options?: ApiRequestOptions,
) {
  const params = new URLSearchParams({ kind, limit: String(limit) });
  return fetchJson<OverviewListResponse>(
    `${BASE}/workspaces/aggregate/running?${params.toString()}`,
    options,
  );
}
