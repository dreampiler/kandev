import { describe, expect, it } from "vitest";
import type { DynamicAgentCandidate, DynamicTierPolicy } from "@/lib/types/agent-profile";
import {
  deriveTiers,
  moveCandidate,
  newCandidateRow,
  normalizeReservedShare,
  removeCandidate,
  tierAt,
  toggleJoin,
  updateCandidateModel,
  updateManualWindow,
  updateTierPolicy,
} from "@/components/settings/dynamic-agent-tiers";

function row(id: string): DynamicAgentCandidate {
  return newCandidateRow(
    id as DynamicAgentCandidate["executionProfileId"],
  ) as DynamicAgentCandidate;
}

/** Builds a row list from the compact notation "A" (head) or "A=" (joined). */
function build(
  spec: string[],
  policy: DynamicTierPolicy = { mode: "pace", onFailure: "same_tier_next" },
) {
  return spec.map((entry, position) => {
    const joined = entry.endsWith("=");
    const id = joined ? entry.slice(0, -1) : entry;
    const base = row(id);
    // A joined row must not carry a tier: only a tier's first row owns one, and
    // the server rejects a row that claims both.
    const { tier: _dropped, ...rest } = base.policies.selection ?? {
      model: { cost: "" as const, usageSource: "none" as const, reservedUserSharePct: 0 },
    };
    return {
      ...base,
      position,
      policies: {
        ...base.policies,
        selection: { ...rest, joinPrevious: joined, ...(joined ? {} : { tier: policy }) },
      },
    } as DynamicAgentCandidate;
  });
}

/** Compact rendering: "A[pace] B[=] C[pace]". */
function summarize(candidates: DynamicAgentCandidate[]) {
  return candidates
    .map((candidate) => {
      const selection = candidate.policies.selection;
      if (!selection) return `${candidate.executionProfileId}[none]`;
      if (selection.joinPrevious) return `${candidate.executionProfileId}[=]`;
      return `${candidate.executionProfileId}[${selection.tier?.mode ?? "?"}]`;
    })
    .join(" ");
}

describe("dynamic tier grouping", () => {
  it("numbers contiguous joined runs from one in list order", () => {
    const tiers = deriveTiers(build(["A", "B=", "C=", "D"]));
    expect(tiers).toHaveLength(2);
    expect(tiers[0].index).toBe(1);
    expect(tiers[0].headPosition).toBe(0);
    expect(tiers[0].rows.map((entry) => entry.executionProfileId)).toEqual(["A", "B", "C"]);
    expect(tiers[1].index).toBe(2);
    expect(tiers[1].headPosition).toBe(3);
  });

  it("gives every legacy row its own tier with the documented defaults", () => {
    const tiers = deriveTiers([
      { ...row("A"), policies: { ...row("A").policies, selection: undefined } },
      { ...row("B"), policies: { ...row("B").policies, selection: undefined } },
    ]);
    expect(tiers).toHaveLength(2);
    for (const tier of tiers) {
      expect(tier.policy).toEqual({ mode: "order", onFailure: "same_tier_next" });
    }
  });

  it("locates the tier owning a position", () => {
    const candidates = build(["A", "B=", "C", "D"]);
    expect(tierAt(candidates, 1)?.index).toBe(1);
    expect(tierAt(candidates, 2)?.index).toBe(2);
    expect(tierAt(candidates, -1)).toBeUndefined();
    expect(tierAt(candidates, 99)).toBeUndefined();
  });
});

describe("join toggle", () => {
  it("merges a row into the upper tier using that tier's policy", () => {
    const joined = toggleJoin(build(["A", "B"]), 1);
    expect(summarize(joined)).toBe("A[pace] B[=]");
    expect(deriveTiers(joined)).toHaveLength(1);
  });

  it("splits a joined row back out and copies the policy to both heads", () => {
    const split = toggleJoin(build(["A", "B="]), 1);
    expect(summarize(split)).toBe("A[pace] B[pace]");
    expect(deriveTiers(split)).toHaveLength(2);
  });

  it("never lets the first row join the row above it", () => {
    const candidates = build(["A", "B"]);
    expect(toggleJoin(candidates, 0)).toBe(candidates);
  });

  it("keeps a joined row's own model options when merging", () => {
    const withModel = updateCandidateModel(build(["A", "B"]), 1, {
      cost: "metered",
      usageSource: "manual",
    });
    const joined = toggleJoin(withModel, 1);
    expect(joined[1].policies.selection?.model).toEqual({
      cost: "metered",
      usageSource: "manual",
      reservedUserSharePct: 0,
    });
  });
});

describe("move", () => {
  it("keeps a same-tier adjacent swap inside its tier with the same policy", () => {
    const moved = moveCandidate(build(["A", "B=", "C="]), 0, 1);
    expect(summarize(moved)).toBe("B[pace] A[=] C[=]");
    expect(deriveTiers(moved)).toHaveLength(1);
    expect(deriveTiers(moved)[0].policy.mode).toBe("pace");
  });

  it("breaks both joins on a cross-tier move without binding new neighbours", () => {
    const moved = moveCandidate(build(["A", "B=", "C", "D="]), 1, 1);
    expect(summarize(moved)).toBe("A[pace] C[pace] B[pace] D[pace]");
    expect(deriveTiers(moved)).toHaveLength(4);
  });

  it("gives each resulting fragment its originating block's policy", () => {
    const left: DynamicTierPolicy = { mode: "pace", onFailure: "same_tier_next" };
    const right: DynamicTierPolicy = { mode: "cost", onFailure: "next_tier" };
    const candidates = [
      ...build(["A", "B="], left),
      ...build(["C", "D="], right).map((entry, position) => ({ ...entry, position: position + 2 })),
    ];
    const moved = moveCandidate(candidates, 1, 1);
    const tiers = deriveTiers(moved);
    expect(tiers.map((tier) => tier.policy.mode)).toEqual(["pace", "cost", "pace", "cost"]);
  });

  it("ignores a move beyond either end of the list", () => {
    const candidates = build(["A", "B"]);
    expect(moveCandidate(candidates, 0, -1)).toBe(candidates);
    expect(moveCandidate(candidates, 1, 1)).toBe(candidates);
  });

  it("renumbers positions after a move", () => {
    const moved = moveCandidate(build(["A", "B", "C"]), 0, 1);
    expect(moved.map((entry) => entry.position)).toEqual([0, 1, 2]);
  });
});

describe("remove", () => {
  it("keeps the remaining members of a removed head's tier grouped", () => {
    const next = removeCandidate(build(["A", "B=", "C="]), 0);
    expect(summarize(next)).toBe("B[pace] C[=]");
  });

  it("does not merge two tiers across a removal", () => {
    const next = removeCandidate(build(["A", "B=", "C", "D="]), 1);
    expect(summarize(next)).toBe("A[pace] C[pace] D[=]");
  });

  it("renumbers positions after a removal", () => {
    const next = removeCandidate(build(["A", "B", "C"]), 0);
    expect(next.map((entry) => entry.position)).toEqual([0, 1]);
  });
});

describe("tier policy", () => {
  it("applies a policy to the tier head only", () => {
    const next = updateTierPolicy(build(["A", "B="]), 1, { mode: "cost", onFailure: "next_tier" });
    expect(next[0].policies.selection?.tier).toEqual({ mode: "cost", onFailure: "next_tier" });
    expect(next[1].policies.selection?.tier).toBeUndefined();
    expect(next[1].policies.selection?.joinPrevious).toBe(true);
  });

  it("leaves other tiers untouched", () => {
    const candidates = build(["A", "B", "C"], {
      mode: "order",
      onFailure: "same_tier_next",
    });
    const next = updateTierPolicy(candidates, 2, { mode: "pace" });
    expect(next[0].policies.selection?.tier?.mode).toBe("order");
    expect(next[1].policies.selection?.tier?.mode).toBe("pace");
  });
});

describe("reserved share", () => {
  it("clears a reserve that has no usage source to observe", () => {
    expect(
      normalizeReservedShare({ cost: "free", usageSource: "none", reservedUserSharePct: 30 }),
    ).toEqual({ cost: "free", usageSource: "none", reservedUserSharePct: 0 });
  });

  it("keeps a reserve while a usage source is selected", () => {
    expect(
      normalizeReservedShare({ cost: "free", usageSource: "automatic", reservedUserSharePct: 30 }),
    ).toEqual({ cost: "free", usageSource: "automatic", reservedUserSharePct: 30 });
  });

  it("keeps a zero reserve untouched", () => {
    expect(
      normalizeReservedShare({ cost: "free", usageSource: "none", reservedUserSharePct: 0 }),
    ).toEqual({ cost: "free", usageSource: "none", reservedUserSharePct: 0 });
  });
});

describe("manual windows", () => {
  it("replaces the window list for one row only", () => {
    const candidates = build(["A", "B"]);
    const windows = [
      {
        period: "month" as const,
        unit: "money" as const,
        limit: "20",
        reset: { anchor: "00:00", timezone: "UTC" },
      },
    ];
    const next = updateManualWindow(candidates, 1, windows);
    expect(next[0].policies.selection?.model?.windows).toBeUndefined();
    expect(next[1].policies.selection?.model?.windows).toEqual(windows);
  });

  it("leaves a joined row's model options addressable by position", () => {
    const candidates = toggleJoin(build(["A", "B"]), 1);
    const next = updateCandidateModel(candidates, 1, { cost: "metered" });
    expect(deriveTiers(next)).toHaveLength(1);
    expect(next[1].policies.selection?.model.cost).toBe("metered");
  });
});
