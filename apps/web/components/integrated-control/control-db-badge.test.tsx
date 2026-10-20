import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ControlDbBadge } from "./control-db-badge";

const BADGE = "control-db-badge";

const fixture = vi.hoisted(() => ({
  diagnostics: undefined as undefined | { driver: string; state: string; stores: [] },
  status: "ready" as string | undefined,
}));
vi.mock("./integrated-control-provider", () => ({
  useIntegratedControl: () => ({
    data: { persistence: fixture.diagnostics, persistenceStatus: fixture.status },
  }),
}));
afterEach(() => {
  cleanup();
  fixture.diagnostics = undefined;
  fixture.status = "ready";
});

describe("control DB badge", () => {
  it("draws nothing until the diagnostics read arrives", () => {
    render(<ControlDbBadge />);
    expect(screen.queryByTestId(BADGE)).toBeNull();
  });

  it("reads a healthy store quietly", () => {
    fixture.diagnostics = { driver: "sqlite", state: "healthy", stores: [] };
    render(<ControlDbBadge />);
    const badge = screen.getByTestId(BADGE);
    expect(badge.textContent).toBe("DB healthy");
    expect(badge.getAttribute("data-state")).toBe("healthy");
  });

  it("marks a recorded unhealthy store as abnormal", () => {
    fixture.diagnostics = { driver: "sqlite", state: "unhealthy", stores: [] };
    render(<ControlDbBadge />);
    expect(screen.getByTestId(BADGE).textContent).toBe("DB unhealthy");
  });

  it("does not claim a state while the store is initializing or the read failed", () => {
    fixture.diagnostics = { driver: "sqlite", state: "initializing", stores: [] };
    render(<ControlDbBadge />);
    expect(screen.queryByTestId(BADGE)).toBeNull();
    cleanup();
    fixture.diagnostics = { driver: "sqlite", state: "healthy", stores: [] };
    fixture.status = "error";
    render(<ControlDbBadge />);
    expect(screen.queryByTestId(BADGE)).toBeNull();
  });
});
