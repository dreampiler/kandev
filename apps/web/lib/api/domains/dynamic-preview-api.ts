import { fetchJson, type ApiRequestOptions } from "@/lib/api/client";

/**
 * Preview states are distinct rather than a single boolean so the editor can tell
 * "nothing to compare" apart from "nothing is currently eligible", which are
 * different problems with the same empty-looking response.
 */
export type DynamicPreviewState =
  | "ready"
  | "no_candidates"
  | "no_eligible_candidate"
  | "unavailable";

export type DynamicPreviewCandidate = {
  position: number;
  execution_profile_id: string;
  tier_index: number;
  selected: boolean;
  eligible: boolean;
  ineligible_reason?: string;
  controlling_window?: string;
  usage_percent: number;
  elapsed_percent: number;
  pace: number;
  usage_known: boolean;
  usage_complete: boolean;
  elapsed_floor_used: boolean;
  cost_class?: string;
  reserved_share_pct: number;
};

export type DynamicPreviewResponse = {
  state: DynamicPreviewState;
  candidate_id?: string;
  tier_index?: number;
  tier_head_id?: string;
  mode?: string;
  reason?: string;
  observed_at: string;
  considered: DynamicPreviewCandidate[];
  usage_complete: boolean;
};

export type DynamicPreviewRequest = {
  dynamic: unknown;
};

/**
 * The preview is read-only, so it posts to the un-suffixed endpoint for an
 * unsaved draft and the profile-scoped one for a saved profile. Neither path
 * saves settings, claims a candidate, or touches route health.
 */
export async function previewDynamicProfile(
  dynamic: unknown,
  options?: ApiRequestOptions & { profileId?: string },
): Promise<DynamicPreviewResponse> {
  const path = options?.profileId
    ? `/api/v1/agent-profiles/${options.profileId}/dynamic-preview`
    : "/api/v1/agent-profiles/dynamic-preview";
  return fetchJson<DynamicPreviewResponse>(path, {
    ...options,
    init: {
      method: "POST",
      body: JSON.stringify({ dynamic } satisfies DynamicPreviewRequest),
      ...(options?.init ?? {}),
    },
  });
}
