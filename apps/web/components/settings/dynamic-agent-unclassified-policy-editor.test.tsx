import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useState } from "react";
import { TooltipProvider } from "@kandev/ui/tooltip";
import type { DynamicUnclassifiedPolicy } from "@/lib/types/agent-profile";
import {
  DYNAMIC_UNCLASSIFIED_MAX_THRESHOLD,
  DYNAMIC_UNCLASSIFIED_MIN_THRESHOLD,
  DynamicUnclassifiedPolicyEditor,
  DynamicUnclassifiedPolicySummary,
  isDynamicUnclassifiedPolicyValid,
} from "./dynamic-agent-unclassified-policy-editor";

vi.mock("react-i18next", () => ({
  useTranslation: () => ({
    t: (key: string, options?: Record<string, unknown>) => {
      if (key === "agents:dynamicUnclassifiedSummaryOn") {
        return `On: ${options?.threshold}`;
      }
      return key;
    },
  }),
}));

const disabled = (): DynamicUnclassifiedPolicy => ({
  enabled: false,
  consecutiveFailureThreshold: 0,
});

function StatefulEditor({ initial }: { initial?: DynamicUnclassifiedPolicy }) {
  const [policy, setPolicy] = useState(initial ?? disabled());
  return (
    <DynamicUnclassifiedPolicyEditor
      policy={policy}
      onChange={(patch) => setPolicy((previous) => ({ ...previous, ...patch }))}
    />
  );
}

describe("isDynamicUnclassifiedPolicyValid", () => {
  it("rejects a disabled policy that still carries a threshold", () => {
    expect(isDynamicUnclassifiedPolicyValid(disabled())).toBe(true);
    expect(
      isDynamicUnclassifiedPolicyValid({ enabled: false, consecutiveFailureThreshold: 3 }),
    ).toBe(false);
  });

  it("bounds an enabled threshold between the shared constants", () => {
    expect(
      isDynamicUnclassifiedPolicyValid({
        enabled: true,
        consecutiveFailureThreshold: DYNAMIC_UNCLASSIFIED_MIN_THRESHOLD,
      }),
    ).toBe(true);
    expect(
      isDynamicUnclassifiedPolicyValid({
        enabled: true,
        consecutiveFailureThreshold: DYNAMIC_UNCLASSIFIED_MAX_THRESHOLD,
      }),
    ).toBe(true);
    expect(
      isDynamicUnclassifiedPolicyValid({
        enabled: true,
        consecutiveFailureThreshold: DYNAMIC_UNCLASSIFIED_MAX_THRESHOLD + 1,
      }),
    ).toBe(false);
    expect(
      isDynamicUnclassifiedPolicyValid({ enabled: true, consecutiveFailureThreshold: 1 }),
    ).toBe(false);
  });
});

describe("DynamicUnclassifiedPolicyEditor", () => {
  afterEach(cleanup);

  it("renders the section with an off switch and no threshold field", () => {
    render(
      <TooltipProvider>
        <StatefulEditor />
      </TooltipProvider>,
    );

    expect(screen.getByTestId("dynamic-policy-unclassified")).toBeTruthy();
    expect(screen.getByRole("switch", { name: "agents:dynamicPolicyUnclassified" })).toBeTruthy();
    expect(screen.queryByRole("spinbutton")).toBeNull();
  });

  it("reveals a bounded threshold field and its help when enabled", () => {
    render(
      <TooltipProvider>
        <StatefulEditor />
      </TooltipProvider>,
    );

    fireEvent.click(screen.getByRole("switch", { name: "agents:dynamicPolicyUnclassified" }));

    const input = screen.getByRole("spinbutton", { name: "agents:dynamicUnclassifiedThreshold" });
    expect(input.getAttribute("min")).toBe(String(DYNAMIC_UNCLASSIFIED_MIN_THRESHOLD));
    expect(input.getAttribute("max")).toBe(String(DYNAMIC_UNCLASSIFIED_MAX_THRESHOLD));
    expect(screen.getByTestId("dynamic-policy-option-help-unclassified")).toBeTruthy();
  });

  it("flags an out-of-range threshold as invalid", () => {
    render(
      <TooltipProvider>
        <StatefulEditor initial={{ enabled: true, consecutiveFailureThreshold: 1 }} />
      </TooltipProvider>,
    );

    const input = screen.getByRole("spinbutton", { name: "agents:dynamicUnclassifiedThreshold" });
    expect(input.getAttribute("aria-invalid")).toBe("true");
    expect(screen.getByText("agents:dynamicUnclassifiedThresholdValidation")).toBeTruthy();
  });
});

describe("DynamicUnclassifiedPolicySummary", () => {
  afterEach(cleanup);

  it("interpolates the threshold when enabled and hides it when off", () => {
    const { rerender } = render(
      <DynamicUnclassifiedPolicySummary
        policy={{ enabled: true, consecutiveFailureThreshold: 4 }}
      />,
    );
    expect(screen.getByText("On: 4")).toBeTruthy();

    rerender(<DynamicUnclassifiedPolicySummary policy={disabled()} />);
    expect(screen.getByText("agents:dynamicUnclassifiedSummaryOff")).toBeTruthy();
  });
});
