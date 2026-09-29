import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AgentProfileId } from "@/lib/types/ids";
import { listAgentProfiles, updateAgentProfile } from "./office-api";

const originalFetch = global.fetch;
const AGENT_ID = "agent-1";
const DYNAMIC_PROFILE_ID = "profile-dynamic-1";

function mockResponse(data: unknown, status = 200) {
  return new Response(JSON.stringify(data), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

describe("office-api execution profile binding", () => {
  let fetchSpy: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    fetchSpy = vi.fn();
    global.fetch = fetchSpy as unknown as typeof fetch;
  });

  afterEach(() => {
    global.fetch = originalFetch;
    vi.restoreAllMocks();
  });

  it("normalizes execution_agent_profile_id from a list response", async () => {
    fetchSpy.mockResolvedValueOnce(
      mockResponse({
        agents: [
          {
            id: AGENT_ID,
            name: "Worker",
            role: "worker",
            execution_agent_profile_id: DYNAMIC_PROFILE_ID,
          },
        ],
      }),
    );

    const res = await listAgentProfiles("ws-1");
    expect(res.agents[0]!.executionAgentProfileId).toBe(DYNAMIC_PROFILE_ID);
  });

  it("maps an empty execution_agent_profile_id to undefined", async () => {
    fetchSpy.mockResolvedValueOnce(
      mockResponse({
        agents: [{ id: AGENT_ID, name: "Worker", role: "worker", execution_agent_profile_id: "" }],
      }),
    );

    const res = await listAgentProfiles("ws-1");
    expect(res.agents[0]!.executionAgentProfileId).toBeUndefined();
  });

  it("PATCHes execution_agent_profile_id when a binding is selected", async () => {
    fetchSpy.mockResolvedValueOnce(
      mockResponse({
        agent: {
          id: AGENT_ID,
          name: "Worker",
          role: "worker",
          execution_agent_profile_id: DYNAMIC_PROFILE_ID,
        },
      }),
    );

    await updateAgentProfile(AGENT_ID, {
      executionAgentProfileId: DYNAMIC_PROFILE_ID as AgentProfileId,
    });
    const init = fetchSpy.mock.calls[0]![1] as RequestInit;
    expect(init.method).toBe("PATCH");
    expect(JSON.parse(init.body as string)).toMatchObject({
      execution_agent_profile_id: DYNAMIC_PROFILE_ID,
    });
  });

  it("PATCHes an empty string to clear the binding", async () => {
    fetchSpy.mockResolvedValueOnce(
      mockResponse({ agent: { id: AGENT_ID, name: "Worker", role: "worker" } }),
    );

    await updateAgentProfile(AGENT_ID, { executionAgentProfileId: "" as AgentProfileId });
    const init = fetchSpy.mock.calls[0]![1] as RequestInit;
    expect(JSON.parse(init.body as string)).toMatchObject({ execution_agent_profile_id: "" });
  });
});
