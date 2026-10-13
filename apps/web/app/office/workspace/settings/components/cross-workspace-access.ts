import type { CrossWorkspaceAccessData } from "@/lib/api/domains/office-api";

/** The disabled default every Office workspace starts with. */
export function emptyCrossWorkspaceAccess(): CrossWorkspaceAccessData {
  return { enabled: false, allowed_workspace_ids: [], coordinator_task_ids: {} };
}

/** Normalizes a server value (which may be absent) into an editable draft. */
export function toCrossWorkspaceDraft(
  value: CrossWorkspaceAccessData | undefined | null,
): CrossWorkspaceAccessData {
  if (!value) return emptyCrossWorkspaceAccess();
  return {
    enabled: Boolean(value.enabled),
    allowed_workspace_ids: [...(value.allowed_workspace_ids ?? [])].sort(),
    coordinator_task_ids: { ...(value.coordinator_task_ids ?? {}) },
  };
}

/** Adds or removes one workspace from the allow-list. */
export function toggleAllowedWorkspace(
  draft: CrossWorkspaceAccessData,
  workspaceId: string,
  allowed: boolean,
): CrossWorkspaceAccessData {
  const set = new Set(draft.allowed_workspace_ids);
  if (allowed) set.add(workspaceId);
  else set.delete(workspaceId);
  return { ...draft, allowed_workspace_ids: [...set].sort() };
}

/** Sets (or clears, with an empty value) a workspace's coordinator task. */
export function setCoordinatorTask(
  draft: CrossWorkspaceAccessData,
  workspaceId: string,
  taskId: string,
): CrossWorkspaceAccessData {
  return {
    ...draft,
    coordinator_task_ids: { ...draft.coordinator_task_ids, [workspaceId]: taskId },
  };
}

/** The PATCH payload: trimmed ids, blank coordinator entries dropped. */
export function toCrossWorkspacePayload(draft: CrossWorkspaceAccessData): CrossWorkspaceAccessData {
  const coordinators: Record<string, string> = {};
  for (const [workspaceId, taskId] of Object.entries(draft.coordinator_task_ids)) {
    const trimmed = taskId.trim();
    if (trimmed) coordinators[workspaceId] = trimmed;
  }
  return {
    enabled: draft.enabled,
    allowed_workspace_ids: [...draft.allowed_workspace_ids].sort(),
    coordinator_task_ids: coordinators,
  };
}

/** Whether the draft differs from the saved value once both are normalized. */
export function isCrossWorkspaceDirty(
  saved: CrossWorkspaceAccessData,
  draft: CrossWorkspaceAccessData,
): boolean {
  return (
    JSON.stringify(canonical(toCrossWorkspacePayload(saved))) !==
    JSON.stringify(canonical(toCrossWorkspacePayload(draft)))
  );
}

function canonical(value: CrossWorkspaceAccessData) {
  return {
    enabled: value.enabled,
    allowed: value.allowed_workspace_ids,
    coordinators: Object.keys(value.coordinator_task_ids)
      .sort()
      .map((key) => [key, value.coordinator_task_ids[key]]),
  };
}
