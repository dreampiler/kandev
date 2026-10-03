import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { AgentProfileUsage } from "@/lib/api/domains/agent-profile-usage-api";
import { AgentProfileUsageView } from "./agent-profile-usage";

vi.mock("react-i18next", () => ({
  useTranslation: () => ({
    t: (key: string, options?: Record<string, unknown>) =>
      options ? `${key}:${JSON.stringify(options)}` : key,
  }),
}));

function usage(overrides: Partial<AgentProfileUsage>): AgentProfileUsage {
  return { profile_id: "p", state: "ok", windows: [], ...overrides };
}

describe("AgentProfileUsageView", () => {
  afterEach(() => cleanup());

  it("lists each window with its scope, exhaustion and reset", () => {
    const now = Date.parse("2026-10-03T12:00:00Z");
    render(
      <AgentProfileUsageView
        now={now}
        usage={usage({
          windows: [
            {
              label: "7-day",
              utilization_pct: 100,
              limit_reached: true,
              reset_at: "2026-10-05T00:00:00Z",
            },
            { label: "1-day", utilization_pct: 1.6, scope: "free_models" },
          ],
        })}
      />,
    );
    const windows = screen.getAllByTestId("agent-profile-usage-window");
    expect(windows).toHaveLength(2);
    expect(windows[0].textContent).toContain("agents:profileUsageLimitReached");
    expect(windows[0].textContent).toContain("agents:profileUsageResets");
    expect(windows[1].textContent).toContain("agents:profileUsageScopeFree");
    expect(windows[1].textContent).toContain('"percent":"2%"');
  });

  it("names the reason an account cannot be read", () => {
    render(
      <AgentProfileUsageView
        usage={usage({ state: "unavailable", reason: "credential_missing" })}
      />,
    );
    expect(screen.getByTestId("agent-profile-usage").textContent).toContain(
      "agents:profileUsageReasonCredentialMissing",
    );
  });

  it("shows recorded account usage when the provider publishes none", () => {
    render(
      <AgentProfileUsageView
        usage={usage({
          state: "no_usage_api",
          recorded: {
            window_start: "2026-10-03T00:00:00Z",
            window_end: "2026-10-04T00:00:00Z",
            turns: 41,
            tokens_total: 3_200_000,
            profile_count: 5,
          },
        })}
      />,
    );
    expect(screen.getByTestId("agent-profile-usage").textContent).toContain(
      "agents:profileUsageRecordedToday",
    );
  });
});
