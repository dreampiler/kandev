export type ActiveSessionInfo = {
  task_id: string;
  task_title: string;
  is_ephemeral: boolean;
};

// WatcherReference points at one issue/PR watcher row that uses the agent
// profile being deleted. Mirrors the Go shape returned from
// /api/v1/agent-profiles/:id?force=false on a 409 conflict.
export type WatcherReference = {
  id: string;
  kind: "linear" | "jira" | "github_issue" | "github_review";
  label: string;
};

export type RoutingTierReference = {
  workspace_id: string;
  provider_id: string;
  tier: "frontier" | "balanced" | "economy";
};

// AutomationReference points at one enabled automation bound to the agent
// profile being deleted. Mirrors the Go shape on the same 409. An automation is
// configuration rather than a session — nothing is running, so it never appears
// in activeSessions, but its next firing would launch against a profile that no
// longer exists.
export type AutomationReference = {
  id: string;
  name: string;
  workspace_id: string;
};

export type UtilityAgentReference = { id: string; name: string };

// DynamicProfileRouteRef names one route row of a dynamic profile that
// references the concrete profile: its zero-based position and 1-based tier.
export type DynamicProfileRouteRef = {
  position: number;
  tier: number;
  enabled: boolean;
};

// DynamicProfileReference is one dynamic profile that contains the concrete
// profile as a candidate. `version` is echoed back on a resolution request so
// the server can fence a stale confirmation.
export type DynamicProfileReference = {
  id: string;
  name: string;
  deleted: boolean;
  version: number;
  routes: DynamicProfileRouteRef[];
};

// DynamicReferenceConflictLists carries the non-dynamic blockers returned when a
// resolution delete re-check finds routing tiers, sessions, watchers, or
// automations that still reference the profile.
export type DynamicReferenceConflictLists = {
  activeSessions: ActiveSessionInfo[];
  watchers: WatcherReference[];
  routingTiers: RoutingTierReference[];
  automations: AutomationReference[];
  utilityAgents: UtilityAgentReference[];
};
