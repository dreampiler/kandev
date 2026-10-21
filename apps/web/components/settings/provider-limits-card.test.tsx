import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ProviderLimit } from "@/lib/api/domains/provider-limits-api";
import { ProviderLimitsCard } from "./provider-limits-card";

vi.mock("react-i18next", () => ({
  useTranslation: () => ({
    t: (key: string, options?: Record<string, unknown>) =>
      options ? `${key}:${JSON.stringify(options)}` : key,
  }),
}));

vi.mock("@/hooks/domains/features/use-feature", () => ({
  useFeature: () => true,
}));

const listProviderLimits = vi.fn();
const updateProviderLimit = vi.fn();
vi.mock("@/lib/api/domains/provider-limits-api", () => ({
  listProviderLimits: (...args: unknown[]) => listProviderLimits(...args),
  updateProviderLimit: (...args: unknown[]) => updateProviderLimit(...args),
}));

const goLimit: ProviderLimit = {
  provider: "opencode-go",
  profile_count: 8,
  model_scoped: true,
  next_monthly_reset: "2026-10-30T05:00:00Z",
  next_monthly_reset_source: "usage",
};

describe("ProviderLimitsCard", () => {
  beforeEach(() => {
    listProviderLimits.mockResolvedValue({
      providers: [goLimit, { provider: "llmgateway", profile_count: 6, model_scoped: false }],
    });
    updateProviderLimit.mockImplementation(async (provider: string, request: object) => ({
      ...goLimit,
      provider,
      ...request,
    }));
  });
  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it("shows every provider with the source of its monthly reset", async () => {
    render(<ProviderLimitsCard />);
    const go = await screen.findByTestId("provider-limit-opencode-go");
    expect(go.textContent).toContain("agents:providerLimitsNextResetUsage");
    expect(go.textContent).toContain("agents:providerLimitsModelScoped");
    expect(screen.getByTestId("provider-limit-llmgateway")).toBeTruthy();
  });

  it("limits the card to one provider on a profile page", async () => {
    render(<ProviderLimitsCard provider="llmgateway" />);
    await screen.findByTestId("provider-limit-llmgateway");
    expect(screen.queryByTestId("provider-limit-opencode-go")).toBeNull();
  });

  it("saves a manual block as an instant", async () => {
    render(<ProviderLimitsCard provider="llmgateway" />);
    await screen.findByTestId("provider-limit-llmgateway");
    fireEvent.change(screen.getByLabelText("agents:providerLimitsBlockUntil"), {
      target: { value: "2026-10-07T23:20" },
    });
    fireEvent.click(screen.getByText("agents:providerLimitsSave"));
    await waitFor(() => expect(updateProviderLimit).toHaveBeenCalledTimes(1));
    const [provider, request] = updateProviderLimit.mock.calls[0];
    expect(provider).toBe("llmgateway");
    expect(request.block_until).toBe(new Date("2026-10-07T23:20").toISOString());
    expect(request.monthly_reset_at).toBeNull();
    expect(request.monthly_reset_timezone).toBe("");
    expect(await screen.findByText("agents:providerLimitsSaved")).toBeTruthy();
  });
});
