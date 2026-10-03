import { act, cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const listAgentProfileUsage = vi.fn();
vi.mock("@/lib/api/domains/agent-profile-usage-api", () => ({
  listAgentProfileUsage: (...args: unknown[]) => listAgentProfileUsage(...args),
}));

import {
  refreshAgentProfileUsage,
  resetAgentProfileUsageForTests,
  useAgentProfileUsage,
} from "./use-agent-profile-usage";

function UsageState({ profileId }: { profileId: string }) {
  const usage = useAgentProfileUsage(profileId);
  return <span data-testid={`usage-${profileId}`}>{usage ? usage.state : "none"}</span>;
}

describe("useAgentProfileUsage", () => {
  beforeEach(() => {
    resetAgentProfileUsageForTests();
    listAgentProfileUsage.mockReset();
  });
  afterEach(() => cleanup());

  it("serves every profile row from one shared request", async () => {
    listAgentProfileUsage.mockResolvedValue({
      profiles: [
        { profile_id: "a", state: "ok", windows: [] },
        { profile_id: "b", state: "unavailable", reason: "unauthorized", windows: [] },
      ],
    });
    render(
      <>
        <UsageState profileId="a" />
        <UsageState profileId="b" />
        <UsageState profileId="c" />
      </>,
    );
    await act(async () => {
      await refreshAgentProfileUsage();
    });
    expect(listAgentProfileUsage).toHaveBeenCalledTimes(1);
    expect(screen.getByTestId("usage-a").textContent).toBe("ok");
    expect(screen.getByTestId("usage-b").textContent).toBe("unavailable");
    expect(screen.getByTestId("usage-c").textContent).toBe("none");
  });

  it("shows nothing rather than a zero when the list cannot be read", async () => {
    listAgentProfileUsage.mockRejectedValue(new Error("offline"));
    render(<UsageState profileId="a" />);
    await act(async () => {
      await refreshAgentProfileUsage();
    });
    expect(screen.getByTestId("usage-a").textContent).toBe("none");
  });
});
