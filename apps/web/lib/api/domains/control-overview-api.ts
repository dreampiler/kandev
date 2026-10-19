import { fetchJson, type ApiRequestOptions } from "../client";
import type {
  OverviewSections,
  OverviewWorkspaceMetrics,
  OverviewRunningTask,
  OverviewListResponse,
  OverviewTaskFilter,
} from "@/lib/types/control-overview";

export type ControlWorkspaceOverview = {
  workspace_id: string;
  name: string;
  is_office?: boolean;
  metrics?: OverviewWorkspaceMetrics;
  running?: OverviewRunningTask[];
  running_truncated?: number;
};
export type ControlOverviewWire = OverviewSections & { workspaces: ControlWorkspaceOverview[] };

/** Preserves the frozen fork's read-only aggregate contract. Missing routes stay unavailable. */
export function getControlOverview(options?: ApiRequestOptions) {
  return fetchJson<ControlOverviewWire>(
    "/api/v1/office/workspaces/aggregate?window_hours=24&scope=reachable",
    options,
  );
}

export function getControlWorkspaceOverview(workspaceId: string, options?: ApiRequestOptions) {
  const params = new URLSearchParams({
    workspace_id: workspaceId,
    window_hours: "24",
    scope: "reachable",
  });
  return fetchJson<ControlOverviewWire>(
    `/api/v1/office/workspaces/aggregate/workspace?${params}`,
    options,
  );
}

export function getControlWorkspaceTasks(
  workspaceId: string,
  filter: OverviewTaskFilter,
  limit = 50,
  options?: ApiRequestOptions,
) {
  const params = new URLSearchParams({
    workspace_id: workspaceId,
    filter,
    limit: String(limit),
    scope: "reachable",
  });
  return fetchJson<OverviewListResponse>(
    `/api/v1/office/workspaces/aggregate/tasks?${params}`,
    options,
  );
}
