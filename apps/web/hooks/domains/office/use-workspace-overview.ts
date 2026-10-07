"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { getWorkspaceOverview } from "@/lib/api/domains/office-overview-api";
import { isPageVisible } from "@/hooks/domains/office/use-workspace-aggregate";
import type { WorkspaceAggregateWire } from "@/lib/api/domains/office-extended-api";
import type { WorkspaceAggregateEntry } from "@/lib/state/slices/office/types";

export type WorkspaceOverviewState =
  | { status: "loading" }
  | { status: "error" }
  | { status: "loaded"; entry: WorkspaceAggregateEntry };

/**
 * Loads one workspace's overview on its own schedule. Each workspace is an
 * independent read, so one slow workspace delays only its own card and the
 * rest of the overview is shown as soon as it is ready.
 */
export function useWorkspaceOverview(
  workspaceId: string,
  refreshSeconds: number,
  windowHours: number,
) {
  const [state, setState] = useState<WorkspaceOverviewState>({ status: "loading" });
  const generationRef = useRef(0);
  const inFlightRef = useRef(false);

  const load = useCallback(async () => {
    if (inFlightRef.current) return;
    inFlightRef.current = true;
    const generation = ++generationRef.current;
    try {
      const data: WorkspaceAggregateWire = await getWorkspaceOverview(workspaceId, windowHours, {
        cache: "no-store",
      });
      if (generation !== generationRef.current) return;
      const entry = data.workspaces?.[0];
      setState(entry ? { status: "loaded", entry } : { status: "error" });
    } catch {
      if (generation !== generationRef.current) return;
      setState({ status: "error" });
    } finally {
      inFlightRef.current = false;
    }
  }, [workspaceId, windowHours]);

  useEffect(() => {
    void load();
    const interval = window.setInterval(() => {
      if (isPageVisible()) void load();
    }, refreshSeconds * 1000);
    return () => {
      window.clearInterval(interval);
      generationRef.current++;
    };
  }, [load, refreshSeconds]);

  return state;
}
