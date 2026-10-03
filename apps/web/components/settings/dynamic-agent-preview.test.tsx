import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type {
  DynamicPreviewCandidate,
  DynamicPreviewResponse,
} from "@/lib/api/domains/dynamic-preview-api";
import { DynamicAgentPreview } from "./dynamic-agent-preview";

vi.mock("react-i18next", () => ({
  useTranslation: () => ({
    t: (key: string, options?: Record<string, unknown>) =>
      options ? `${key}:${JSON.stringify(options)}` : key,
  }),
}));

function preview(overrides: Partial<DynamicPreviewResponse>): DynamicPreviewResponse {
  return {
    state: "ready",
    observed_at: "2026-10-03T00:00:00Z",
    considered: [],
    usage_complete: true,
    ...overrides,
  };
}

const labelFor = (id: string) => `Profile ${id}`;

function considered(overrides: Partial<DynamicPreviewCandidate>): DynamicPreviewCandidate {
  return {
    position: 0,
    execution_profile_id: "a",
    tier_index: 1,
    selected: true,
    eligible: true,
    usage_percent: 0,
    elapsed_percent: 0,
    pace: 0,
    usage_known: true,
    usage_complete: true,
    elapsed_floor_used: false,
    reserved_share_pct: 0,
    ...overrides,
  };
}

describe("DynamicAgentPreview", () => {
  afterEach(cleanup);

  it("renders nothing while the preview is not in play", () => {
    const { container } = render(
      <DynamicAgentPreview state={{ status: "idle" }} labelFor={labelFor} onRetry={vi.fn()} />,
    );
    expect(container.textContent).toBe("");
  });

  it("announces loading through a status region", () => {
    render(
      <DynamicAgentPreview state={{ status: "loading" }} labelFor={labelFor} onRetry={vi.fn()} />,
    );
    const status = screen.getByTestId("dynamic-preview-loading");
    expect(status.getAttribute("role")).toBe("status");
  });

  it("distinguishes no candidates from no eligible candidates", () => {
    const { unmount } = render(
      <DynamicAgentPreview
        state={{ status: "ready", preview: preview({ state: "no_candidates" }) }}
        labelFor={labelFor}
        onRetry={vi.fn()}
      />,
    );
    expect(screen.getByTestId("dynamic-preview-no-candidates")).toBeTruthy();
    unmount();

    render(
      <DynamicAgentPreview
        state={{ status: "ready", preview: preview({ state: "no_eligible_candidate" }) }}
        labelFor={labelFor}
        onRetry={vi.fn()}
      />,
    );
    expect(screen.getByTestId("dynamic-preview-no-eligible")).toBeTruthy();
  });

  it("offers a retry after a read failure", () => {
    const onRetry = vi.fn();
    render(
      <DynamicAgentPreview state={{ status: "failed" }} labelFor={labelFor} onRetry={onRetry} />,
    );
    screen.getByTestId("dynamic-preview-failed").querySelector("button")?.click();
    expect(onRetry).toHaveBeenCalled();
  });
});

describe("DynamicAgentPreview with a prediction", () => {
  afterEach(cleanup);

  it("names the selected candidate, its tier and its reason", () => {
    render(
      <DynamicAgentPreview
        state={{
          status: "ready",
          preview: preview({ candidate_id: "a", tier_index: 2, reason: "tier_pace_same_tier" }),
        }}
        labelFor={labelFor}
        onRetry={vi.fn()}
      />,
    );
    const ready = screen.getByTestId("dynamic-preview-ready");
    expect(ready.textContent).toContain("Profile a");
    expect(ready.textContent).toContain("agents:dynamicPreviewTier");
    expect(ready.textContent).toContain("tier_pace_same_tier");
  });

  it("reports unknown usage as unknown rather than as zero", () => {
    render(
      <DynamicAgentPreview
        state={{
          status: "ready",
          preview: preview({
            candidate_id: "a",
            considered: [considered({ usage_known: false, usage_percent: 0, elapsed_percent: 0 })],
          }),
        }}
        labelFor={labelFor}
        onRetry={vi.fn()}
      />,
    );
    const ready = screen.getByTestId("dynamic-preview-ready");
    expect(ready.textContent).toContain("agents:dynamicPreviewUnknownUsage");
    expect(ready.textContent).not.toContain("agents:dynamicPreviewWindow");
  });

  it("marks a partial recorded total as a lower bound and shows the reserved share", () => {
    render(
      <DynamicAgentPreview
        state={{
          status: "ready",
          preview: preview({
            candidate_id: "a",
            usage_complete: false,
            considered: [
              considered({
                usage_known: true,
                usage_percent: 40,
                elapsed_percent: 50,
                pace: 0.8,
                reserved_share_pct: 10,
              }),
            ],
          }),
        }}
        labelFor={labelFor}
        onRetry={vi.fn()}
      />,
    );
    const ready = screen.getByTestId("dynamic-preview-ready");
    expect(ready.textContent).toContain("agents:dynamicPreviewUsageIncomplete");
    expect(ready.textContent).toContain("agents:dynamicModelReservedShare");
  });
});
