import { useCallback, useRef, useState } from "react";
import { runOpenSequence } from "./use-copilot-open-sequence";

/**
 * Opens a coordinator's conversation from the control tree when it has none
 * yet: the existing coordinator open sequence (profile check, then the
 * conversation route), then one control refresh so the snapshot carries the
 * new conversation before the caller navigates to it. A click while an open
 * is in flight is ignored.
 */
export function useOpenControlConversation(
  workspaceId: string,
  coordinatorId: string,
  refresh: (() => Promise<void>) | undefined,
) {
  const inFlight = useRef(false);
  const [pending, setPending] = useState(false);
  const [failed, setFailed] = useState(false);
  const open = useCallback(async (): Promise<boolean> => {
    if (inFlight.current) return false;
    inFlight.current = true;
    setPending(true);
    setFailed(false);
    try {
      const result = await runOpenSequence(workspaceId, coordinatorId);
      if (result.kind !== "ready") {
        setFailed(true);
        return false;
      }
      await refresh?.();
      return true;
    } finally {
      inFlight.current = false;
      setPending(false);
    }
  }, [workspaceId, coordinatorId, refresh]);
  return { open, pending, failed };
}
