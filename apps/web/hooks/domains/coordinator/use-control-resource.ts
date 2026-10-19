import { useEffect, useState } from "react";
import { useAppStore } from "@/components/state-provider";
import { ApiError } from "@/lib/api/client";
import { NativeControlContractError } from "@/lib/api/domains/control-native-api";
import { stateReadScopeIdentity } from "@/lib/state/shared-resource-reads";

export type ControlReadStatus = "loading" | "ready" | "unsupported" | "denied" | "error";
export type ControlRead<T> = { status: ControlReadStatus; data?: T; receivedAt?: string };

/** Route-owned reads follow the existing control snapshot. No timer or provider refresh. */
export function useControlResource<T>(
  read: (signal: AbortSignal) => Promise<T>,
  revision: object | undefined,
): ControlRead<T> {
  const identity = useAppStore(stateReadScopeIdentity);
  const [result, setResult] = useState<
    ControlRead<T> & { identity: string; read: typeof read; revision: typeof revision }
  >();
  useEffect(() => {
    if (!revision) return;
    const request = new AbortController();
    const publish = (value: ControlRead<T>) => {
      if (!request.signal.aborted) setResult({ ...value, identity, read, revision });
    };
    void read(request.signal).then(
      (data) => publish({ status: "ready", data, receivedAt: new Date().toISOString() }),
      (error: unknown) => publish({ status: controlReadFailure(error) }),
    );
    return () => request.abort();
  }, [identity, read, revision]);
  return result?.identity === identity && result.read === read && result.revision === revision
    ? result
    : { status: "loading" as const };
}

export function controlReadFailure(error: unknown): ControlReadStatus {
  if (error instanceof NativeControlContractError) return "unsupported";
  if (error instanceof ApiError) {
    if (error.status === 401 || error.status === 403) return "denied";
    if (error.status === 404 || error.status === 501) return "unsupported";
  }
  return "error";
}
