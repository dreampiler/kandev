"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { useAppStore } from "@/components/state-provider";
import {
  getWorkspaceAggregate,
  type WorkspaceAggregateWire,
} from "@/lib/api/domains/office-extended-api";
import type { OverviewSections } from "@/lib/state/slices/office/overview-types";
import {
  OVERVIEW_STATS_WINDOW_DEFAULT,
  type OverviewStatsWindowHours,
} from "@/lib/state/slices/office/overview-types";
import { OVERVIEW_REFRESH_SECONDS_DEFAULT } from "@/lib/settings/overview-refresh";

export type WorkspaceAggregateLoadState = "loading" | "loaded" | "error";
type InFlightRequest = { promise: Promise<void>; generation: number };

/** True while the document is shown; refreshes pause in hidden tabs. */
export function isPageVisible(): boolean {
  return typeof document === "undefined" || document.visibilityState !== "hidden";
}

export function sectionsFrom(data: WorkspaceAggregateWire): OverviewSections {
  return {
    scope: data.scope === "reachable" ? "reachable" : "office",
    generated_at: data.generated_at,
    compute_ms: data.compute_ms,
    system: data.system,
    models: data.models ?? [],
    blocked_accounts: data.blocked_accounts ?? [],
    blocked_circuits: data.blocked_circuits,
    last_24h: data.last_24h ?? [],
    needs_human: data.needs_human ?? [],
  };
}

/**
 * Loads the shared workspace overview and refreshes it on the caller's chosen
 * period while mounted and the page is visible. A hidden tab makes no requests;
 * becoming visible again refreshes once immediately.
 *
 * The returned `receivedAt` and `refreshing` describe the last accepted read,
 * so a widget can say how fresh it is and a control can show that a refresh is
 * running instead of guessing from the interval.
 */
export function useWorkspaceAggregate(
  refreshSeconds: number = OVERVIEW_REFRESH_SECONDS_DEFAULT,
  windowHours: OverviewStatsWindowHours = OVERVIEW_STATS_WINDOW_DEFAULT,
) {
  const setWorkspaceAggregate = useAppStore((state) => state.setWorkspaceAggregate);
  const [loadState, setLoadState] = useState<WorkspaceAggregateLoadState>("loading");
  const [refreshing, setRefreshing] = useState(true);
  const [receivedAt, setReceivedAt] = useState<string | null>(null);
  const inFlightRef = useRef<InFlightRequest | null>(null);
  const requestGenerationRef = useRef(0);

  const refresh = useCallback((): Promise<void> => {
    if (inFlightRef.current?.generation === requestGenerationRef.current) {
      return inFlightRef.current.promise;
    }

    const requestGeneration = ++requestGenerationRef.current;
    setRefreshing(true);
    const request = getWorkspaceAggregate(windowHours, { cache: "no-store" })
      .then((data) => {
        if (requestGeneration !== requestGenerationRef.current) return;
        setWorkspaceAggregate({
          workspaces: data.workspaces ?? [],
          sections: sectionsFrom(data),
        });
        setReceivedAt(data.generated_at ?? new Date().toISOString());
        setLoadState("loaded");
      })
      .catch(() => {
        if (requestGeneration !== requestGenerationRef.current) return;
        setLoadState((current) => (current === "loading" ? "error" : current));
      })
      .finally(() => {
        if (inFlightRef.current?.promise === request) inFlightRef.current = null;
        if (requestGeneration === requestGenerationRef.current) setRefreshing(false);
      });

    inFlightRef.current = { promise: request, generation: requestGeneration };
    return request;
  }, [setWorkspaceAggregate, windowHours]);

  useEffect(() => {
    void refresh();
    const interval = window.setInterval(() => {
      if (isPageVisible()) void refresh();
    }, refreshSeconds * 1000);
    const onVisibilityChange = () => {
      if (isPageVisible()) void refresh();
    };
    document.addEventListener("visibilitychange", onVisibilityChange);
    return () => {
      window.clearInterval(interval);
      document.removeEventListener("visibilitychange", onVisibilityChange);
      requestGenerationRef.current++;
    };
  }, [refresh, refreshSeconds]);

  return { loadState, refresh, refreshing, receivedAt };
}
