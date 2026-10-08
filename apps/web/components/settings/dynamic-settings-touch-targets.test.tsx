import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { TooltipProvider } from "@kandev/ui/tooltip";
import type { DynamicAgentCandidate, DynamicErrorPolicy } from "@/lib/types/agent-profile";
import { agentProfileId } from "@/lib/types/ids";
import { newCandidateRow } from "./dynamic-agent-tiers";
import { DynamicAgentCandidateRow } from "./dynamic-agent-candidate-row";
import { DynamicPolicyEditor } from "./dynamic-agent-policy-editor";

vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

/**
 * `@kandev/ui/switch` renders a 16.6px control, and the mobile E2E asserts the
 * rendered switch itself meets 44px. A surrounding row's `min-h-11` does not
 * enlarge it, so every switch this feature renders must carry the touch size.
 * Asserting the class here catches a regression without an E2E run.
 */
const TOUCH_SWITCH_CLASSES = ["data-[size=default]:h-11", "data-[size=default]:w-11"];

function expectTouchSized(switcher: HTMLElement | null, label: string) {
  expect(switcher, `${label} switch should exist`).not.toBeNull();
  const className = switcher?.getAttribute("class") ?? "";
  for (const required of TOUCH_SWITCH_CLASSES) {
    expect(className, `${label} switch is missing ${required}`).toContain(required);
  }
}

const policy: DynamicErrorPolicy = {
  retry: { enabled: false, maxRetries: 0, initialIntervalSeconds: 0 },
  waitForReset: { enabled: false, maxWaitSeconds: 0 },
  onExhausted: "skip",
};

describe("dynamic settings touch targets", () => {
  afterEach(cleanup);

  it("gives the candidate enabled switch a 44px hit area", () => {
    const candidate = newCandidateRow(
      agentProfileId("concrete-a"),
    ) as unknown as DynamicAgentCandidate;
    render(
      <TooltipProvider>
        <DynamicAgentCandidateRow
          candidate={candidate}
          label="A"
          isFirst
          isLast
          enabledLabel="Enabled"
          onOpenModelDetail={vi.fn()}
          onMove={vi.fn()}
          onToggleJoin={vi.fn()}
          onRemove={vi.fn()}
          onToggleEnabled={vi.fn()}
          onUpdateModel={vi.fn()}
          onUpdateWindows={vi.fn()}
          onUpdatePolicy={vi.fn()}
          onUpdateUnclassified={vi.fn()}
        />
      </TooltipProvider>,
    );
    expectTouchSized(screen.getByRole("switch", { name: "Enabled: A" }), "candidate enabled");
  });

  it("gives the retry and reset-wait switches a 44px hit area", () => {
    render(
      <TooltipProvider>
        <DynamicPolicyEditor errorClass="transient" policy={policy} onChange={vi.fn()} />
      </TooltipProvider>,
    );
    expectTouchSized(
      screen.getByRole("switch", { name: "agents:dynamicPolicyRetry" }),
      "policy retry",
    );
    expectTouchSized(
      screen.getByRole("switch", { name: "agents:dynamicPolicyWaitForReset" }),
      "policy wait-for-reset",
    );
  });
});
