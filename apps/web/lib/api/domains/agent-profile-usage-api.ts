import { fetchJson, type ApiRequestOptions } from "@/lib/api/client";

/**
 * A profile's usage state. `unavailable` carries a bounded reason, and
 * `no_usage_api` means the provider publishes no usage, in which case the
 * account's recorded usage for the current UTC day may be present.
 */
export type AgentProfileUsageState = "ok" | "unavailable" | "unsupported" | "no_usage_api";

export type AgentProfileUsageWindow = {
  label: string;
  utilization_pct: number;
  reset_at?: string;
  start_at?: string;
  duration_seconds?: number;
  scope?: string;
  limit_reached?: boolean;
};

export type AgentProfileRecordedUsage = {
  window_start: string;
  window_end: string;
  turns: number;
  tokens_total: number;
  profile_count: number;
};

export type AgentProfileInternalUsage = {
  profile_count: number;
  windows: { label: string; turns: number; tokens_total: number; cost_subcents: number }[];
};

export type AgentProfileLimitHits = {
  count: number;
  last_at: string;
  median_turns_5h: number;
  median_turns_day: number;
  median_tokens_day: number;
  median_turns_week: number;
  median_tokens_week: number;
};

export type AgentProfileUsage = {
  profile_id: string;
  state: AgentProfileUsageState;
  reason?: string;
  status?: number;
  source?: string;
  provider?: string;
  plan?: string;
  model_class?: string;
  fetched_at?: string;
  stale?: boolean;
  /**
   * The value came from what a running agent reported about its own account,
   * because the provider's usage API did not answer. It travels with
   * `source: "agent_stream"`, so the reading's origin and age are stated rather
   * than presented as a live provider reading.
   */
  observed?: boolean;
  windows: AgentProfileUsageWindow[];
  recorded?: AgentProfileRecordedUsage;
  internal?: AgentProfileInternalUsage;
  limit_hits?: AgentProfileLimitHits;
};

export type ListAgentProfileUsageResponse = {
  profiles: AgentProfileUsage[];
};

/** Reads every concrete profile's provider usage. The call is read-only. */
export async function listAgentProfileUsage(
  options?: ApiRequestOptions,
): Promise<ListAgentProfileUsageResponse> {
  return fetchJson<ListAgentProfileUsageResponse>("/api/v1/agent-profiles/usage", options);
}
