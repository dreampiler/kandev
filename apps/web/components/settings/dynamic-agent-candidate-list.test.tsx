import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { TooltipProvider } from "@kandev/ui/tooltip";
import type { AgentProfile } from "@/lib/types/http";
import { agentProfileId } from "@/lib/types/ids";
import type { DynamicAgentCandidate } from "@/lib/types/agent-profile";
import { newCandidateRow } from "./dynamic-agent-tiers";
import { DynamicAgentCandidateList } from "./dynamic-agent-candidate-list";

const { breakpoint } = vi.hoisted(() => ({
  breakpoint: { current: { isMobile: false, isTablet: false, isFinePointer: true } },
}));

vi.mock("@/hooks/use-responsive-breakpoint", () => ({
  useResponsiveBreakpoint: () => breakpoint.current,
}));

vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

vi.mock("@/components/settings/agent-profile-picker", () => ({
  AgentProfilePicker: () => <div data-testid="add-dynamic-candidate" />,
}));

function concreteProfile(name: string): AgentProfile {
  return {
    id: agentProfileId(`concrete-${name}`),
    kind: "concrete",
    name,
    agentId: "claude",
    agentDisplayName: "Claude",
    model: "",
    allowIndexing: false,
    autoApprove: false,
    cliFlags: [],
    cliPassthrough: false,
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
  };
}

/** Builds a candidate list from "A" (tier head) or "A=" (joined to the row above). */
function candidates(spec: string[]): DynamicAgentCandidate[] {
  return spec.map((entry, position) => {
    const joined = entry.endsWith("=");
    const id = agentProfileId(`concrete-${joined ? entry.slice(0, -1) : entry}`);
    const base = newCandidateRow(id) as DynamicAgentCandidate;
    const selection = base.policies.selection;
    return {
      ...base,
      position,
      policies: {
        ...base.policies,
        selection: {
          ...selection,
          model: selection?.model ?? { cost: "", usageSource: "none", reservedUserSharePct: 0 },
          joinPrevious: joined,
          tier: { mode: "pace" as const, onFailure: "next_tier" as const },
        },
      },
    };
  });
}

function renderList(
  spec: string[],
  overrides: Partial<Parameters<typeof DynamicAgentCandidateList>[0]> = {},
) {
  const props = {
    candidates: candidates(spec),
    concreteProfiles: spec.map((entry) => concreteProfile(entry.replace("=", ""))),
    availableProfileOptions: [],
    enabledLabel: "Enabled",
    addCandidate: vi.fn(),
    moveCandidate: vi.fn(),
    removeCandidate: vi.fn(),
    toggleJoin: vi.fn(),
    updateTierPolicy: vi.fn(),
    updateCandidateModel: vi.fn(),
    updateManualWindow: vi.fn(),
    updateCandidate: vi.fn(),
    updateCandidatePolicy: vi.fn(),
    ...overrides,
  };
  return {
    ...render(
      <TooltipProvider>
        <DynamicAgentCandidateList {...props} />
      </TooltipProvider>,
    ),
    props,
  };
}

function childrenOf(element: Element | null): string[] {
  return element
    ? Array.from(element.children).map((child) => child.getAttribute("aria-label") ?? "")
    : [];
}

describe("DynamicAgentCandidateList", () => {
  afterEach(() => {
    cleanup();
    breakpoint.current = { isMobile: false, isTablet: false, isFinePointer: true };
  });

  it("places the join control between the up and down controls", () => {
    renderList(["A", "B", "C"]);
    const actions = screen.getByTestId("dynamic-candidate-1").querySelector("div:last-child");
    expect(childrenOf(actions)).toEqual([
      "agents:moveDynamicCandidateUp",
      "agents:dynamicJoinToggle: B",
      "agents:moveDynamicCandidateDown",
      "agents:removeDynamicCandidate",
    ]);
  });

  it("disables the first row's join control, which has no row above it", () => {
    renderList(["A", "B"]);
    expect((screen.getByTestId("dynamic-join-toggle-0") as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByTestId("dynamic-join-toggle-1") as HTMLButtonElement).disabled).toBe(false);
  });

  it("numbers contiguous joined rows as one tier and renumbers after a split", () => {
    renderList(["A", "B=", "C="]);
    expect(screen.getByTestId("dynamic-tier-1")).toBeTruthy();
    expect(screen.queryByTestId("dynamic-tier-2")).toBeNull();

    cleanup();
    renderList(["A", "B=", "C"]);
    expect(screen.getByTestId("dynamic-tier-1")).toBeTruthy();
    expect(screen.getByTestId("dynamic-tier-2")).toBeTruthy();
  });

  it("renders one selection policy per tier, not per row", () => {
    renderList(["A", "B=", "C"]);
    expect(screen.getByTestId("dynamic-tier-settings-1")).toBeTruthy();
    expect(screen.getByTestId("dynamic-tier-settings-2")).toBeTruthy();
    expect(screen.queryByTestId("dynamic-tier-settings-3")).toBeNull();
  });

  it("keeps the model options inline on desktop", () => {
    renderList(["A", "B"]);
    expect(screen.getByTestId("dynamic-model-settings-open-0")).toBeTruthy();
    expect(screen.queryByTestId("dynamic-model-detail")).toBeNull();
  });

  it("keeps each row's own failure policies reachable", () => {
    renderList(["A", "B"]);
    expect(screen.getAllByTestId("dynamic-policy-transient")).toHaveLength(2);
    expect(screen.getAllByTestId("dynamic-policy-hard")).toHaveLength(2);
  });

  it("reports an empty candidate list rather than rendering an empty tier", () => {
    renderList([]);
    expect(screen.getByText("agents:noDynamicCandidates")).toBeTruthy();
    expect(screen.queryByTestId("dynamic-tier-1")).toBeNull();
  });

  it("labels rows by concrete profile name rather than raw identity", () => {
    renderList(["A"]);
    expect(screen.getByTestId("dynamic-candidate-0").textContent).toContain("A");
  });
});
