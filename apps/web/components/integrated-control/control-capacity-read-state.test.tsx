import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { SessionCapacityObservation } from "@/lib/types/system";
import { ControlCapacitySnapshot } from "./control-capacity-snapshot";
const fixture = vi.hoisted(() => ({
  now: Date.parse("2026-10-19T10:00:00Z"),
  control: { loading: false, error: false, data: { receivedAt: "2026-10-19T10:00:04.999Z" } },
}));
vi.mock("@/hooks/use-now", () => ({ useNow: () => fixture.now }));
vi.mock("@/components/state-provider", () => ({ useOptionalAppStore: () => true }));
vi.mock("./integrated-control-provider", () => ({ useIntegratedControl: () => fixture.control }));
afterEach(cleanup);
const observation: SessionCapacityObservation = {
  waiting: { known: false },
  enabled: true,
  policy: "all",
  known: true,
  in_use: 0,
  limit: 14,
  observed_at: "2026-10-19T10:00:05.001Z",
};
describe("capacity response and source clocks", () => {
  it("does not reject a just-received response near the five-second display clock boundary", () => {
    render(<ControlCapacitySnapshot observation={observation} />);
    expect(screen.getByTestId("control-capacity-total").textContent).toBe("0 / 14");
    expect(screen.getByText(/Source collection time is not provided/)).toBeTruthy();
  });
  it("keeps a historical zero separate from the latest backend unknown", () => {
    render(
      <ControlCapacitySnapshot
        observation={{ ...observation, known: false, in_use: null }}
        lastConfirmed={observation}
      />,
    );
    expect(screen.getByTestId("control-capacity-total").textContent).toBe("Unknown / 14");
    expect(screen.getByTestId("control-capacity-last-confirmed").textContent).toContain("0 / 14");
    expect(
      screen.getByTestId("control-capacity-last-confirmed").querySelector("time")?.dateTime,
    ).toBe(observation.observed_at);
  });
  it.each(["2026-02-30T00:00:00Z", "0", "2026-10-19T10:01:00Z"])(
    "rejects an invalid or future response time %s",
    (at) => {
      render(<ControlCapacitySnapshot observation={{ ...observation, observed_at: at }} />);
      expect(screen.getByTestId("control-capacity-total").textContent).toBe("Unknown / 14");
    },
  );
});
