import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ControlRefreshBanner } from "./control-refresh-banner";

const BANNER = "control-refresh-banner";

type FailureKind = "unavailable" | "disconnected" | "denied" | "unsupported" | "error";

const fixture = vi.hoisted(() => ({
  failure: null as null | { kind: FailureKind; retryAt?: string },
  data: undefined as unknown,
}));
vi.mock("./integrated-control-provider", () => ({
  useIntegratedControl: () => ({ failure: fixture.failure, data: fixture.data }),
}));
afterEach(() => {
  cleanup();
  fixture.failure = null;
  fixture.data = undefined;
});

function snapshot(independent: { error: boolean; denied?: boolean }, tasks: object) {
  return {
    receivedAt: "2026-10-21T06:00:00Z",
    byWorkspace: {},
    conversationsByWorkspace: { w: { independent, tasks } },
  };
}

describe("control refresh banner", () => {
  it("stays quiet when every collection was read", () => {
    fixture.data = snapshot({ error: false }, { total: 0, error: false });
    render(<ControlRefreshBanner />);
    expect(screen.queryByTestId(BANNER)).toBeNull();
  });

  it("reports a denied independent conversation instead of showing an empty list", () => {
    fixture.data = snapshot(
      { error: true, denied: true },
      { total: null, error: true, denied: true },
    );
    render(<ControlRefreshBanner />);
    const banner = screen.getByTestId(BANNER);
    expect(banner.textContent).toContain("Some workspace data could not be loaded");
  });

  it("names an unsupported read without promising an automatic retry", () => {
    fixture.failure = { kind: "unsupported" };
    render(<ControlRefreshBanner />);
    const banner = screen.getByTestId(BANNER);
    expect(banner.getAttribute("data-kind")).toBe("unsupported");
    expect(banner.textContent).toBe("This server does not provide this reading.");
  });

  it("keeps the retry wording for a read that will be retried", () => {
    fixture.failure = { kind: "error" };
    render(<ControlRefreshBanner />);
    expect(screen.getByTestId(BANNER).textContent).toContain("retrying automatically");
  });
});
