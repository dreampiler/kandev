import type {
  DynamicAgentCandidate,
  DynamicModelPolicy,
  DynamicTierMode,
  DynamicTierFailureDirection,
  DynamicTierPolicy,
  DynamicUsageWindow,
} from "@/lib/types/agent-profile";

/**
 * One tier is a maximal contiguous run of candidate rows joined together. The
 * badge number is derived adjacency and is never stored: a row's stable identity
 * is always its concrete profile ID, so a badge survives no reorder.
 */
export type DynamicCandidateTier = {
  index: number;
  headPosition: number;
  policy: DynamicTierPolicy;
  rows: DynamicAgentCandidate[];
};

export const defaultTierPolicy = (): DynamicTierPolicy => ({
  mode: "order",
  onFailure: "same_tier_next",
});

export const defaultModelPolicy = (): DynamicModelPolicy => ({
  cost: "",
  usageSource: "none",
  reservedUserSharePct: 0,
});

/**
 * Groups an ordered candidate list into contiguous tiers. A row whose join flag
 * is set belongs to the tier above it. The first row is always a tier head.
 */
export function deriveTiers(candidates: DynamicAgentCandidate[]): DynamicCandidateTier[] {
  const tiers: DynamicCandidateTier[] = [];
  candidates.forEach((candidate, position) => {
    const joined = position > 0 && candidate.policies.selection?.joinPrevious === true;
    const previous = tiers[tiers.length - 1];
    if (joined && previous) {
      previous.rows.push(candidate);
      return;
    }
    tiers.push({
      index: tiers.length + 1,
      headPosition: position,
      policy: candidate.policies.selection?.tier ?? defaultTierPolicy(),
      rows: [candidate],
    });
  });
  return tiers;
}

/** The tier containing a position, or undefined when the position is invalid. */
export function tierAt(
  candidates: DynamicAgentCandidate[],
  position: number,
): DynamicCandidateTier | undefined {
  if (position < 0 || position >= candidates.length) return undefined;
  let seen = 0;
  for (const tier of deriveTiers(candidates)) {
    if (position < seen + tier.rows.length) return tier;
    seen += tier.rows.length;
  }
  return undefined;
}

function withPositions(candidates: DynamicAgentCandidate[]): DynamicAgentCandidate[] {
  return candidates.map((candidate, position) =>
    candidate.position === position ? candidate : { ...candidate, position },
  );
}

/** Rewrites tier ownership for a rebuilt order, keeping each fragment's policy. */
function applyTierOwnership(
  candidates: DynamicAgentCandidate[],
  blockPolicyByProfile: Map<string, DynamicTierPolicy>,
  joinedByProfile: Map<string, boolean>,
): DynamicAgentCandidate[] {
  return withPositions(
    candidates.map((candidate) => {
      const profileId = candidate.executionProfileId;
      const joined = joinedByProfile.get(profileId) === true;
      const policy = blockPolicyByProfile.get(profileId) ?? defaultTierPolicy();
      return {
        ...candidate,
        policies: {
          ...candidate.policies,
          selection: {
            joinPrevious: joined,
            ...(joined ? {} : { tier: policy }),
            model: candidate.policies.selection?.model ?? defaultModelPolicy(),
          },
        },
      };
    }),
  );
}

/**
 * Records, for every candidate, the tier block it belonged to before a mutation
 * and the policy that block owned. Identity is the concrete profile ID.
 */
function captureBlocks(candidates: DynamicAgentCandidate[]) {
  const blockOf = new Map<string, string>();
  const policyOf = new Map<string, DynamicTierPolicy>();
  for (const tier of deriveTiers(candidates)) {
    for (const row of tier.rows) {
      blockOf.set(row.executionProfileId, tier.headPosition === -1 ? "" : String(tier.index));
      policyOf.set(row.executionProfileId, tier.policy);
    }
  }
  return { blockOf, policyOf };
}

/**
 * Joins a row to the row above it, or splits it back out.
 *
 * Joining merges the row into the upper block and adopts that block's policy.
 * Unjoining splits the block and copies the policy to both new heads, so neither
 * fragment silently loses its tier settings. The first row can never join.
 */
export function toggleJoin(
  candidates: DynamicAgentCandidate[],
  position: number,
): DynamicAgentCandidate[] {
  if (position <= 0 || position >= candidates.length) return candidates;
  const tier = tierAt(candidates, position);
  if (!tier) return candidates;
  const joining = candidates[position].policies.selection?.joinPrevious !== true;
  const sharedPolicy = tier.policy;

  const joinedByProfile = new Map<string, boolean>();
  const policyByProfile = new Map<string, DynamicTierPolicy>();
  candidates.forEach((candidate, index) => {
    const profileId = candidate.executionProfileId;
    if (index === position) {
      joinedByProfile.set(profileId, joining);
      policyByProfile.set(profileId, sharedPolicy);
      return;
    }
    const rowTier = tierAt(candidates, index);
    joinedByProfile.set(profileId, candidate.policies.selection?.joinPrevious === true);
    policyByProfile.set(profileId, rowTier?.policy ?? sharedPolicy);
  });
  return applyTierOwnership(candidates, policyByProfile, joinedByProfile);
}

/**
 * Moves a row one position. An adjacent swap inside one tier keeps that tier
 * intact and only rebuilds which row owns its policy. A swap across a tier
 * boundary breaks the join edges whose endpoints are no longer adjacent, forces
 * the moved row unjoined, and never creates a new edge merely because two rows
 * became neighbours. Each resulting fragment inherits its originating policy.
 */
export function moveCandidate(
  candidates: DynamicAgentCandidate[],
  position: number,
  direction: -1 | 1,
): DynamicAgentCandidate[] {
  const target = position + direction;
  if (position < 0 || target < 0 || target >= candidates.length) return candidates;

  const { blockOf, policyOf } = captureBlocks(candidates);
  const swapped = [...candidates];
  [swapped[position], swapped[target]] = [swapped[target], swapped[position]];

  const joinedByProfile = new Map<string, boolean>();
  const policyByProfile = new Map<string, DynamicTierPolicy>();
  swapped.forEach((candidate, index) => {
    const profileId = candidate.executionProfileId;
    policyByProfile.set(profileId, policyOf.get(profileId) ?? defaultTierPolicy());
    const above = index > 0 ? swapped[index - 1] : undefined;
    if (!above) {
      joinedByProfile.set(profileId, false);
      return;
    }
    // An edge survives only when both sides already shared a saved tier.
    joinedByProfile.set(
      profileId,
      blockOf.get(profileId) !== undefined &&
        blockOf.get(profileId) === blockOf.get(above.executionProfileId),
    );
  });
  return applyTierOwnership(swapped, policyByProfile, joinedByProfile);
}

/**
 * Removes a row. The remaining members of the same tier stay grouped and the
 * next member inherits the head's policy; the removed row never merges its block
 * into the following unrelated tier.
 */
export function removeCandidate(
  candidates: DynamicAgentCandidate[],
  position: number,
): DynamicAgentCandidate[] {
  if (position < 0 || position >= candidates.length) return candidates;
  const { blockOf, policyOf } = captureBlocks(candidates);
  const remaining = candidates.filter((_, index) => index !== position);

  const joinedByProfile = new Map<string, boolean>();
  const policyByProfile = new Map<string, DynamicTierPolicy>();
  remaining.forEach((candidate, index) => {
    const profileId = candidate.executionProfileId;
    policyByProfile.set(profileId, policyOf.get(profileId) ?? defaultTierPolicy());
    const above = index > 0 ? remaining[index - 1] : undefined;
    joinedByProfile.set(
      profileId,
      above !== undefined &&
        blockOf.get(profileId) !== undefined &&
        blockOf.get(profileId) === blockOf.get(above.executionProfileId),
    );
  });
  return applyTierOwnership(remaining, policyByProfile, joinedByProfile);
}

/** A new row is a singleton tier, so it starts unjoined with the default policy. */
export function newCandidateRow(executionProfileId: DynamicAgentCandidate["executionProfileId"]) {
  return {
    position: 0,
    executionProfileId,
    enabled: true,
    policies: {
      version: 1,
      transient: {
        retry: { enabled: false, maxRetries: 0, initialIntervalSeconds: 0 },
        waitForReset: { enabled: false, maxWaitSeconds: 0 },
        onExhausted: "skip" as const,
      },
      hard: {
        retry: { enabled: false, maxRetries: 0, initialIntervalSeconds: 0 },
        waitForReset: { enabled: false, maxWaitSeconds: 0 },
        onExhausted: "stop" as const,
      },
      unclassified: { enabled: false, consecutiveFailureThreshold: 0 },
      selection: { joinPrevious: false, tier: defaultTierPolicy(), model: defaultModelPolicy() },
    },
  };
}

/** Applies a tier policy to a tier's head only; joined rows never carry one. */
export function updateTierPolicy(
  candidates: DynamicAgentCandidate[],
  tierIndex: number,
  patch: Partial<DynamicTierPolicy>,
): DynamicAgentCandidate[] {
  const tier = deriveTiers(candidates).find((entry) => entry.index === tierIndex);
  if (!tier) return candidates;
  const nextPolicy = { ...tier.policy, ...patch };
  return candidates.map((candidate, position) => {
    if (position !== tier.headPosition) return candidate;
    return {
      ...candidate,
      policies: {
        ...candidate.policies,
        selection: {
          ...(candidate.policies.selection ?? { joinPrevious: false, model: defaultModelPolicy() }),
          tier: nextPolicy,
        },
      },
    };
  });
}

/** Applies per-model options to one row, which every row owns for itself. */
export function updateCandidateModel(
  candidates: DynamicAgentCandidate[],
  position: number,
  patch: Partial<DynamicModelPolicy>,
): DynamicAgentCandidate[] {
  return candidates.map((candidate, index) => {
    if (index !== position) return candidate;
    const selection = candidate.policies.selection ?? {
      joinPrevious: false,
      tier: defaultTierPolicy(),
      model: defaultModelPolicy(),
    };
    return {
      ...candidate,
      policies: {
        ...candidate.policies,
        selection: { ...selection, model: { ...selection.model, ...patch } },
      },
    };
  });
}

export function updateManualWindow(
  candidates: DynamicAgentCandidate[],
  position: number,
  windows: DynamicUsageWindow[],
): DynamicAgentCandidate[] {
  return updateCandidateModel(candidates, position, { windows });
}

/**
 * A positive reserved share is only meaningful against a usage source that can
 * be observed, so clearing the source must clear the share with it. This keeps
 * the editor from offering a reserve that the server would reject.
 */
export function normalizeReservedShare(model: DynamicModelPolicy): DynamicModelPolicy {
  if (model.usageSource === "none" && model.reservedUserSharePct !== 0) {
    return { ...model, reservedUserSharePct: 0 };
  }
  return model;
}

export const tierModeOptions: readonly DynamicTierMode[] = ["order", "pace", "cost"];
export const tierFailureOptions: readonly DynamicTierFailureDirection[] = [
  "same_tier_next",
  "next_tier",
];
