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

  it("shows the account's recorded turns and where limit hits happened", () => {
    render(
      <AgentProfileUsageView
        now={Date.parse("2026-10-04T12:00:00Z")}
        usage={usage({
          state: "unsupported",
          internal: {
            profile_count: 5,
            windows: [
              { label: "5h", turns: 12, tokens_total: 1, cost_subcents: 0 },
              { label: "day", turns: 41, tokens_total: 1, cost_subcents: 0 },
              { label: "week", turns: 300, tokens_total: 1, cost_subcents: 0 },
            ],
          },
          limit_hits: {
            count: 3,
            last_at: "2026-10-04T10:00:00Z",
            median_turns_5h: 30,
            median_turns_day: 120,
            median_tokens_day: 1,
            median_turns_week: 400,
            median_tokens_week: 1,
          },
        })}
      />,
    );
    expect(screen.queryByTestId("agent-profile-usage")).toBeNull();
    expect(screen.getByTestId("agent-profile-usage-internal").textContent).toContain('"day":"41"');
    expect(screen.getByTestId("agent-profile-usage-limit-hits").textContent).toContain(
      '"turns":"120"',
    );
  });
});

describe("AgentProfileUsageView - stale observation", () => {
  afterEach(() => cleanup());

  it("renders age line for a stale reading and is absent for a fresh one", () => {
    const now = Date.parse("2026-10-03T12:00:00Z");
    const fetchedAt = "2026-10-03T11:45:00Z";
    const { rerender } = render(
      <AgentProfileUsageView
        now={now}
        usage={usage({
          stale: true,
          fetched_at: fetchedAt,
          windows: [{ label: "5-hour", utilization_pct: 50 }],
        })}
      />,
    );
    const staleEl = screen.getByTestId("agent-profile-usage-stale");
    expect(staleEl).toBeDefined();
    expect(staleEl.textContent).toContain("agents:profileUsageStaleObserved");

    // Fresh reading has no stale element
    rerender(
      <AgentProfileUsageView
        now={now}
        usage={usage({
          stale: false,
          fetched_at: fetchedAt,
          windows: [{ label: "5-hour", utilization_pct: 50 }],
        })}
      />,
    );
    expect(screen.queryByTestId("agent-profile-usage-stale")).toBeNull();
  });
});
