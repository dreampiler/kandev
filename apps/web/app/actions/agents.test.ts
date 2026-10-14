import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/lib/config", () => ({
  getBackendConfig: () => ({ apiBaseUrl: "http://backend.test" }),
}));

const reloadMocks = vi.hoisted(() => ({
  signalBackendReloadRequired: vi.fn(),
}));

vi.mock("@/lib/platform/backend-reload-coordinator", () => ({
  signalBackendReloadRequired: (...args: unknown[]) =>
    reloadMocks.signalBackendReloadRequired(...args),
}));

import {
  deleteAgentProfileAction,
  dynamicProfilesFromApiError,
  reorderAgentProfilesAction,
  resolveDynamicReferencesAction,
} from "./agents";

const bootWindow = window as unknown as { __KANDEV_BOOT_PAYLOAD__?: unknown };

beforeEach(() => {
  bootWindow.__KANDEV_BOOT_PAYLOAD__ = {
    interimSettingsInterlockToken: "boot-token",
  };
  reloadMocks.signalBackendReloadRequired.mockReset();
});

afterEach(() => {
  vi.unstubAllGlobals();
  delete bootWindow.__KANDEV_BOOT_PAYLOAD__;
});

describe("deleteAgentProfileAction", () => {
  it("returns a handled result for the stale interlock response", async () => {
    const fetcher = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          error: "interim settings interlock required",
          error_code: "interim_settings_interlock_required",
        }),
        { status: 403, headers: { "Content-Type": "application/json" } },
      ),
    );
    vi.stubGlobal("fetch", fetcher);

    await expect(deleteAgentProfileAction("profile-1")).resolves.toEqual({
      status: "error",
      message: "interim settings interlock required",
      handled: true,
    });
    expect(reloadMocks.signalBackendReloadRequired).toHaveBeenCalledWith(
      "settings_interlock_rejected",
    );
    expect(
      new Headers(fetcher.mock.calls[0]?.[1]?.headers).get("X-Kandev-Interim-Settings-Interlock"),
    ).toBe("boot-token");
  });

  it("keeps the typed conflict result for profile deletion", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            active_sessions: [{ id: "session-1" }],
            watchers: [],
            routing_tiers: [],
            automations: [],
            utility_agents: [],
          }),
          { status: 409, headers: { "Content-Type": "application/json" } },
        ),
      ),
    );

    await expect(deleteAgentProfileAction("profile-1")).resolves.toMatchObject({
      status: "conflict",
      activeSessions: [{ id: "session-1" }],
    });
    expect(reloadMocks.signalBackendReloadRequired).not.toHaveBeenCalled();
  });

  it("carries the typed dynamic profile references in the conflict result", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            dynamic_profiles: [
              {
                id: "dynamic-1",
                name: "Frontier",
                deleted: false,
                version: 7,
                routes: [{ position: 0, tier: 1, enabled: true }],
              },
            ],
          }),
          { status: 409, headers: { "Content-Type": "application/json" } },
        ),
      ),
    );

    await expect(deleteAgentProfileAction("profile-1")).resolves.toMatchObject({
      status: "conflict",
      dynamicProfiles: [{ id: "dynamic-1", version: 7 }],
    });
  });
});

describe("dynamicProfilesFromApiError", () => {
  it("returns [] for a non-ApiError value", () => {
    expect(dynamicProfilesFromApiError(new Error("boom"))).toEqual([]);
  });
});

describe("resolveDynamicReferencesAction", () => {
  it("posts the echoed versions and returns the disabled profile", async () => {
    const fetcher = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ id: "profile-1", name: "P", kind: "concrete" }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );
    vi.stubGlobal("fetch", fetcher);

    const result = await resolveDynamicReferencesAction("profile-1", {
      operation: "disable",
      action: "disable_candidate",
      versions: { "dynamic-1": 7 },
    });

    expect(result.status).toBe("ok");
    expect(fetcher.mock.calls[0][0]).toBe(
      "http://backend.test/api/v1/agent-profiles/profile-1/dynamic-reference-resolution",
    );
    expect(JSON.parse(fetcher.mock.calls[0][1].body)).toEqual({
      operation: "disable",
      action: "disable_candidate",
      versions: { "dynamic-1": 7 },
    });
  });

  it("returns ok with a null profile for a delete resolution", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(new Response(JSON.stringify({ success: true }), { status: 200 })),
    );
    await expect(
      resolveDynamicReferencesAction("profile-1", {
        operation: "delete",
        action: "remove_candidate",
        versions: { "dynamic-1": 7 },
      }),
    ).resolves.toEqual({ status: "ok", profile: null });
  });

  it("surfaces a non-dynamic blocker as a conflict", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            routing_tiers: [{ workspace_id: "ws-1", provider_id: "p-1", tier: "balanced" }],
          }),
          { status: 409, headers: { "Content-Type": "application/json" } },
        ),
      ),
    );
    await expect(
      resolveDynamicReferencesAction("profile-1", {
        operation: "delete",
        action: "remove_candidate",
        versions: { "dynamic-1": 7 },
      }),
    ).resolves.toMatchObject({ status: "conflict", routingTiers: [{ workspace_id: "ws-1" }] });
  });

  it("reports a stale-version 409 with no lists as an error", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ error: "dynamic profile version conflict" }), {
          status: 409,
          headers: { "Content-Type": "application/json" },
        }),
      ),
    );
    await expect(
      resolveDynamicReferencesAction("profile-1", {
        operation: "disable",
        action: "disable_candidate",
        versions: { "dynamic-1": 7 },
      }),
    ).resolves.toMatchObject({ status: "error", message: "dynamic profile version conflict" });
  });
});

describe("reorderAgentProfilesAction", () => {
  it("bounds the real fetch request with a fifteen-second abort signal", async () => {
    const controller = new AbortController();
    const timeout = vi.spyOn(AbortSignal, "timeout").mockReturnValue(controller.signal);
    const fetcher = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ agent_id: "agent", profile_ids: ["b", "a"], revision: 2 }), {
        status: 200,
      }),
    );
    vi.stubGlobal("fetch", fetcher);
    try {
      await reorderAgentProfilesAction("agent", ["b", "a"]);
      expect(timeout).toHaveBeenCalledWith(15_000);
      expect(fetcher.mock.calls[0][1].signal).toBe(controller.signal);
    } finally {
      timeout.mockRestore();
    }
  });
});
