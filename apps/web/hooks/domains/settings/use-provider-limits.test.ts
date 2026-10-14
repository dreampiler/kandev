import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ProviderAccountLimit, ProviderLimit } from "@/lib/api/domains/provider-limits-api";
import { useProviderLimits } from "./use-provider-limits";

const api = vi.hoisted(() => ({ listProviderLimits: vi.fn(), updateProviderLimit: vi.fn() }));
vi.mock("@/lib/api/domains/provider-limits-api", () => api);

const account: ProviderAccountLimit = {
  account_id: "opencode-go",
  account_kind: "opencode_go",
  profiles: [],
  overrides: { tiers: {} },
  tiers: {
    free: { defaults: {}, learned: {}, override: {}, effective: {}, usage: {} },
    subscription: { defaults: {}, learned: {}, override: {}, effective: {}, usage: {} },
    paid: { defaults: {}, learned: {}, override: {}, effective: {}, usage: {} },
  },
};
const current: ProviderLimit = {
  provider: "opencode-go",
  profile_count: 1,
  model_scoped: true,
  monthly_reset_at: "2026-10-30T05:00:00Z",
  accounts: [account],
};
const legacyRequest = {
  monthly_reset_at: current.monthly_reset_at!,
  monthly_reset_timezone: "UTC",
  block_until: "2026-10-31T00:00:00Z",
};

afterEach(() => vi.clearAllMocks());

describe("provider limit saves", () => {
  it("preserves account editors when the monthly reset/block response omits accounts", async () => {
    api.listProviderLimits.mockResolvedValue({ providers: [current] });
    api.updateProviderLimit.mockResolvedValue({
      provider: current.provider,
      profile_count: 1,
      model_scoped: true,
      ...legacyRequest,
    });
    const { result } = renderHook(() => useProviderLimits());
    await waitFor(() => expect(result.current.state.status).toBe("ready"));
    await act(() => result.current.save(current.provider, legacyRequest));
    expect(result.current.state).toMatchObject({
      status: "ready",
      providers: [{ ...legacyRequest, accounts: [account] }],
    });
  });

  it("updates an account without replacing monthly reset or paid block controls", async () => {
    const updated = { ...account, overrides: { tiers: { free: { requests_per_day: 42 } } } };
    const original = { ...current, block_until: legacyRequest.block_until };
    api.listProviderLimits.mockResolvedValue({ providers: [original] });
    api.updateProviderLimit.mockResolvedValue({ ...current, accounts: [updated] });
    const { result } = renderHook(() => useProviderLimits());
    await waitFor(() => expect(result.current.state.status).toBe("ready"));
    await act(() =>
      result.current.save(current.provider, {
        account_id: account.account_id,
        account_limits: updated.overrides,
        monthly_reset_at: null,
        monthly_reset_timezone: "",
        block_until: null,
      }),
    );
    expect(result.current.state).toEqual({
      status: "ready",
      providers: [{ ...original, accounts: [updated] }],
    });
  });
});
