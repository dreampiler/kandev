"use client";

import { useEffect, useRef, useState } from "react";
import type { OverviewListResponse } from "@/lib/state/slices/office/overview-types";
import { isPageVisible } from "./use-workspace-aggregate";
import { OVERVIEW_REFRESH_SECONDS_DEFAULT } from "@/lib/settings/overview-refresh";

export type OverviewListState = {
  data: OverviewListResponse | null;
  loadState: "idle" | "loading" | "loaded" | "error";
};

const IDLE: OverviewListState = { data: null, loadState: "idle" };

/**
 * Loads an overview list only while it is expanded (`load` non-null) and
 * refreshes it on the overview's auto-refresh period while the page is visible.
 * `key` identifies the request; a new key reloads at once. The previous rows
 * stay on screen while a refresh is in flight, and nothing is kept once the
 * list closes.
 */
export function useOverviewList(
  key: string,
  load: (() => Promise<OverviewListResponse>) | null,
  refreshSeconds: number = OVERVIEW_REFRESH_SECONDS_DEFAULT,
): OverviewListState {
  const [state, setState] = useState<OverviewListState>(IDLE);
  const loadRef = useRef(load);
  loadRef.current = load;
  const enabled = load !== null;

  useEffect(() => {
    if (!enabled) {
      setState(IDLE);
      return;
    }
    let active = true;
    let inFlight = false;
    const run = () => {
      const current = loadRef.current;
      if (!current || inFlight) return;
      inFlight = true;
      setState((prev) => (prev.data ? prev : { data: null, loadState: "loading" }));
      current()
        .then((data) => {
          if (active) setState({ data, loadState: "loaded" });
        })
        .catch(() => {
          if (active) setState((prev) => (prev.data ? prev : { data: null, loadState: "error" }));
        })
        .finally(() => {
          inFlight = false;
        });
    };
    setState(IDLE);
    run();
    const interval = window.setInterval(() => {
      if (isPageVisible()) run();
    }, refreshSeconds * 1000);
    const onVisibilityChange = () => {
      if (isPageVisible()) run();
    };
    document.addEventListener("visibilitychange", onVisibilityChange);
    return () => {
      active = false;
      window.clearInterval(interval);
      document.removeEventListener("visibilitychange", onVisibilityChange);
    };
  }, [key, enabled, refreshSeconds]);

  return state;
}
