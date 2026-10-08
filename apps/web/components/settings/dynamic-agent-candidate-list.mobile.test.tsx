import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { createContext, useContext } from "react";
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

// Radix keeps a closed drawer's portal unmounted, so the mock gates its content
// on `open` too; otherwise a closed surface's form would appear in the tree.
vi.mock("@kandev/ui/drawer", () => {
  const OpenContext = createContext(false);
  return {
    Drawer: ({ open, children }: { open: boolean; children: React.ReactNode }) => (
      <OpenContext.Provider value={open}>
        <div>{children}</div>
      </OpenContext.Provider>
    ),
    DrawerContent: ({ children, ...props }: { children: React.ReactNode }) => {
      const open = useContext(OpenContext);
      return open ? <div {...props}>{children}</div> : null;
    },
    DrawerHeader: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
    DrawerTitle: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  };
});

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

function candidates(count: number): DynamicAgentCandidate[] {
  return Array.from({ length: count }, (_, position) => {
    const base = newCandidateRow(
      agentProfileId(`concrete-${String.fromCharCode(65 + position)}`),
    ) as DynamicAgentCandidate;
    return { ...base, position };
  });
}

function renderPhone(count: number) {
  breakpoint.current = { isMobile: true, isTablet: false, isFinePointer: false };
  return render(
    <TooltipProvider>
      <DynamicAgentCandidateList
        candidates={candidates(count)}
        concreteProfiles={candidates(count).map((entry) =>
          concreteProfile(entry.executionProfileId.replace("concrete-", "")),
        )}
        availableProfileOptions={[]}
        enabledLabel="Enabled"
        addCandidate={vi.fn()}
        moveCandidate={vi.fn()}
        removeCandidate={vi.fn()}
        toggleJoin={vi.fn()}
        updateTierPolicy={vi.fn()}
        updateCandidateModel={vi.fn()}
        updateManualWindow={vi.fn()}
        updateCandidate={vi.fn()}
        updateCandidatePolicy={vi.fn()}
        updateCandidateUnclassified={vi.fn()}
        applyUnclassifiedToAll={vi.fn()}
      />
    </TooltipProvider>,
  );
}

describe("DynamicAgentCandidateList on a phone", () => {
  afterEach(() => {
    cleanup();
    breakpoint.current = { isMobile: false, isTablet: false, isFinePointer: true };
  });

  it("moves tier options behind an explicit entry point instead of inline", () => {
    renderPhone(2);
    expect(screen.getByTestId("dynamic-tier-settings-open-1")).toBeTruthy();
    expect(screen.queryByTestId("dynamic-tier-settings-1")).toBeNull();
    expect(screen.queryByTestId("dynamic-tier-drawer")).toBeNull();
  });

  it("keeps model options out of the list until a detail surface is opened", () => {
    renderPhone(2);
    expect(screen.queryAllByTestId("dynamic-model-settings")).toHaveLength(0);
    expect(screen.getByTestId("dynamic-model-settings-open-0")).toBeTruthy();
  });
});
