import { fetchJson, type ApiRequestOptions } from "../client";
import type { WorkspaceAggregateWire } from "@/lib/api/domains/office-extended-api";
import type {
  OverviewListResponse,
  OverviewRunningKind,
  OverviewTaskFilter,
} from "@/lib/state/slices/office/overview-types";

const BASE = "/api/v1/office";

/**
 * Asks a workspace list for every match instead of a page. The backend reads it
 * as an intent, so the problems list and "show all" are not cut at a fixed
 * ceiling; any finite limit stays clamped server-side.
 */
export const OVERVIEW_LIST_ALL = -1;

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

/**
 * One workspace's overview, computed and cached on its own. Each workspace
 * answers independently, so a slow workspace cannot hold back the ones that are
 * ready; the caller renders a card as soon as its own read lands.
 */
export function getWorkspaceOverview(workspaceId: string, options?: ApiRequestOptions) {
  const params = new URLSearchParams({ workspace_id: workspaceId });
  return fetchJson<WorkspaceAggregateWire>(
    `${BASE}/workspaces/aggregate/workspace?${params.toString()}`,
    options,
  );
}
