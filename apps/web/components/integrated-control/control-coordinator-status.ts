import type { Coordinator } from "@/lib/api/domains/coordinator-api";

export type WorkspaceCoordinatorState = "responding" | "talking" | "waiting" | "none";

const LIVE_SESSION_STATES = new Set(["STARTING", "RUNNING"]);

/** The already-read workspace task page, keyed by task id. */
export function conversationSessionStates(
  tasks: ReadonlyArray<{ id: string; primary_session_state?: string | null }> | undefined,
): ReadonlyMap<string, string | null> {
  return new Map((tasks ?? []).map((task) => [task.id, task.primary_session_state ?? null]));
}

/**
 * One workspace's coordinator state from the recorded coordinator list: an
 * open proposal needs an answer first, a conversation whose session is
 * observed starting or running reads as talking, an existing conversation
 * that is not observed running reads as waiting, and no conversation reads as
 * none. The task page is a bounded sample, so a conversation outside it is
 * not confirmed running and reads as waiting rather than as talking.
 */
export function workspaceCoordinatorState(
  coordinators: ReadonlyArray<Pick<Coordinator, "open_proposals" | "conversation_task_id">>,
  sessionStates: ReadonlyMap<string, string | null>,
): WorkspaceCoordinatorState {
  if (coordinators.length === 0) return "none";
  if (coordinators.some((coordinator) => (coordinator.open_proposals ?? 0) > 0))
    return "responding";
  const conversationIds = coordinators
    .map((coordinator) => coordinator.conversation_task_id)
    .filter((id): id is string => Boolean(id));
  if (conversationIds.length === 0) return "none";
  if (conversationIds.some((id) => LIVE_SESSION_STATES.has(sessionStates.get(id) ?? "")))
    return "talking";
  return "waiting";
}
