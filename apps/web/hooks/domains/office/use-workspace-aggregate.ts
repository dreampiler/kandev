"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { useAppStore } from "@/components/state-provider";
import {
  getWorkspaceAggregate,
  type WorkspaceAggregateWire,
} from "@/lib/api/domains/office-extended-api";
import { normalizeActivityEntry } from "@/lib/api/domains/office-activity-normalize";
import type { OverviewSections } from "@/lib/state/slices/office/overview-types";

export const OVERVIEW_REFRESH_INTERVAL_MS = 30_000;

export type WorkspaceAggregateLoadState = "loading" | "loaded" | "error";
type InFlightRequest = { promise: Promise<void>; generation: number };

/** True while the document is shown; refreshes pause in hidden tabs. */
export function isPageVisible(): boolean {
  return typeof document === "undefined" || document.visibilityState !== "hidden";
}

function sectionsFrom(data: WorkspaceAggregateWire): OverviewSections {
  return {
    scope: data.scope === "reachable" ? "reachable" : "office",
    generated_at: data.generated_at,
    compute_ms: data.compute_ms,
    system: data.system,
    models: data.models ?? [],
    blocked_accounts: data.blocked_accounts ?? [],
    last_24h: data.last_24h ?? [],
    needs_human: data.needs_human ?? [],
  };
}

/**
 * Loads the shared workspace overview and refreshes it every 30 seconds while
 * a caller is mounted and the page is visible. A hidden tab makes no
 * requests; becoming visible again refreshes once immediately.
 */
export function useWorkspaceAggregate() {
  const setWorkspaceAggregate = useAppStore((state) => state.setWorkspaceAggregate);
  const [loadState, setLoadState] = useState<WorkspaceAggregateLoadState>("loading");
  const inFlightRef = useRef<InFlightRequest | null>(null);
  const requestGenerationRef = useRef(0);

  const refresh = useCallback((): Promise<void> => {
    if (inFlightRef.current?.generation === requestGenerationRef.current) {
      return inFlightRef.current.promise;
    }

    const requestGeneration = ++requestGenerationRef.current;
    const request = getWorkspaceAggregate({ cache: "no-store" })
      .then((data) => {
        if (requestGeneration !== requestGenerationRef.current) return;
        setWorkspaceAggregate({
          workspaces: data.workspaces ?? [],
          recentActivity: (data.recent_activity ?? []).map(normalizeActivityEntry),
          sections: sectionsFrom(data),
        });
        setLoadState("loaded");
      })
      .catch(() => {
        if (requestGeneration !== requestGenerationRef.current) return;
        setLoadState((current) => (current === "loading" ? "error" : current));
      })
      .finally(() => {
        if (inFlightRef.current?.promise === request) inFlightRef.current = null;
      });

    inFlightRef.current = { promise: request, generation: requestGeneration };
    return request;
  }, [setWorkspaceAggregate]);

  useEffect(() => {
    void refresh();
    const interval = window.setInterval(() => {
      if (isPageVisible()) void refresh();
    }, OVERVIEW_REFRESH_INTERVAL_MS);
    const onVisibilityChange = () => {
      if (isPageVisible()) void refresh();
    };
    document.addEventListener("visibilitychange", onVisibilityChange);
    return () => {
      window.clearInterval(interval);
      document.removeEventListener("visibilitychange", onVisibilityChange);
      requestGenerationRef.current++;
    };
  }, [refresh]);

  return { loadState, refresh };
}
