"use client";

import { useEffect, useSyncExternalStore } from "react";
import {
  listAgentProfileUsage,
  type AgentProfileUsage,
} from "@/lib/api/domains/agent-profile-usage-api";

/**
 * Every profile row reads one shared list, so a page with many profiles makes
 * one request. The backend caches provider reads for five minutes, so the list
 * is refreshed at most once a minute while rows are mounted.
 */
const REFRESH_INTERVAL_MS = 60_000;

type UsageSnapshot = {
  byProfile: ReadonlyMap<string, AgentProfileUsage>;
  loaded: boolean;
};

const EMPTY_SNAPSHOT: UsageSnapshot = { byProfile: new Map(), loaded: false };

let snapshot: UsageSnapshot = EMPTY_SNAPSHOT;
let fetchedAt = 0;
let inFlight: Promise<void> | null = null;
const listeners = new Set<() => void>();

function publish(next: UsageSnapshot) {
  snapshot = next;
  for (const listener of listeners) listener();
}

/** Loads the list unless a fresh one is cached or a request is already running. */
export function refreshAgentProfileUsage(now: number = Date.now()): Promise<void> {
  if (inFlight) return inFlight;
  if (snapshot.loaded && now - fetchedAt < REFRESH_INTERVAL_MS) return Promise.resolve();
  inFlight = listAgentProfileUsage()
    .then((response) => {
      fetchedAt = Date.now();
      publish({
        byProfile: new Map(response.profiles.map((usage) => [usage.profile_id, usage])),
        loaded: true,
      });
    })
    .catch(() => {
      // A failed read leaves rows without a usage line rather than showing a
      // stale or zero value; the next mount or interval tries again.
      fetchedAt = Date.now();
    })
    .finally(() => {
      inFlight = null;
    });
  return inFlight;
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

/** Test seam: forget the shared list. */
export function resetAgentProfileUsageForTests() {
  snapshot = EMPTY_SNAPSHOT;
  fetchedAt = 0;
  inFlight = null;
  listeners.clear();
}

/** Returns one profile's usage, or undefined until the shared list has it. */
export function useAgentProfileUsage(profileId: string): AgentProfileUsage | undefined {
  const current = useSyncExternalStore(
    subscribe,
    () => snapshot,
    () => EMPTY_SNAPSHOT,
  );
  useEffect(() => {
    void refreshAgentProfileUsage();
    const timer = window.setInterval(() => void refreshAgentProfileUsage(), REFRESH_INTERVAL_MS);
    return () => window.clearInterval(timer);
  }, []);
  return current.byProfile.get(profileId);
}
