import { useCallback, useEffect, useRef, useState } from "react";
import { useAppStore, useAppStoreApi } from "@/components/state-provider";
import { stateReadScopeIdentity } from "@/lib/state/shared-resource-reads";
import {
  readProfileExecutionUsage,
  type ProfileExecutionUsage,
} from "@/lib/api/domains/provider-usage-api";

/** This GET reads cached execution observations, without refreshing a provider. */
export function useProfileExecutionUsage(profileId: string, autoRead: boolean) {
  const store = useAppStoreApi();
  const identity = useAppStore(stateReadScopeIdentity);
  const [snapshot, setSnapshot] = useState<{
    identity: string;
    profileId: string;
    entries: ProfileExecutionUsage[];
  }>();
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState(false);
  const request = useRef<AbortController | null>(null);
  const refresh = useCallback(async () => {
    request.current?.abort();
    const controller = new AbortController();
    request.current = controller;
    const current = () =>
      !controller.signal.aborted && stateReadScopeIdentity(store.getState()) === identity;
    setBusy(true);
    setFailed(false);
    setSnapshot(undefined);
    try {
      const result = await readProfileExecutionUsage(profileId, {
        cache: "no-store",
        init: { signal: controller.signal },
      });
      if (current()) setSnapshot({ identity, profileId, entries: result.executions });
    } catch {
      if (current()) setFailed(true);
    } finally {
      if (current()) setBusy(false);
    }
  }, [profileId, identity, store]);
  useEffect(() => {
    setFailed(false);
    setBusy(false);
    if (autoRead) void refresh();
    return () => request.current?.abort();
  }, [autoRead, refresh]);
  const entries =
    snapshot?.identity === identity && snapshot.profileId === profileId
      ? snapshot.entries
      : undefined;
  return { entries, busy, failed, refresh };
}
