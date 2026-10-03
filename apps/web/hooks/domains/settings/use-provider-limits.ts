"use client";

import { useCallback, useEffect, useState } from "react";
import {
  listProviderLimits,
  updateProviderLimit,
  type ProviderLimit,
  type UpdateProviderLimitRequest,
} from "@/lib/api/domains/provider-limits-api";

export type ProviderLimitsState =
  | { status: "loading" }
  | { status: "failed" }
  | { status: "ready"; providers: ProviderLimit[] };

/** Loads provider limit settings and saves one provider at a time. */
export function useProviderLimits() {
  const [state, setState] = useState<ProviderLimitsState>({ status: "loading" });

  const reload = useCallback(async () => {
    setState({ status: "loading" });
    try {
      const response = await listProviderLimits({ cache: "no-store" });
      setState({ status: "ready", providers: response.providers });
    } catch {
      setState({ status: "failed" });
    }
  }, []);

  useEffect(() => {
    void reload();
  }, [reload]);

  const save = useCallback(async (provider: string, request: UpdateProviderLimitRequest) => {
    const saved = await updateProviderLimit(provider, request);
    setState((current) =>
      current.status === "ready"
        ? {
            status: "ready",
            providers: current.providers.map((entry) =>
              entry.provider === saved.provider ? saved : entry,
            ),
          }
        : current,
    );
    return saved;
  }, []);

  return { state, reload, save };
}
