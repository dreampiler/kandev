import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ControlDbBadge } from "./control-db-badge";

const fixture = vi.hoisted(() => ({
  diagnostics: undefined as undefined | { driver: string; state: string; stores: [] },
}));
vi.mock("./integrated-control-provider", () => ({
  useIntegratedControl: () => ({ data: { persistence: fixture.diagnostics } }),
}));
afterEach(() => {
  cleanup();
  fixture.diagnostics = undefined;
});

describe("control DB badge", () => {
  it("draws nothing until the diagnostics read arrives", () => {
    render(<ControlDbBadge />);
    expect(screen.queryByTestId("control-db-badge")).toBeNull();
  });

  it("reads a healthy store quietly", () => {
    fixture.diagnostics = { driver: "sqlite", state: "healthy", stores: [] };
    render(<ControlDbBadge />);
    const badge = screen.getByTestId("control-db-badge");
    expect(badge.textContent).toBe("DB healthy");
    expect(badge.getAttribute("data-state")).toBe("healthy");
  });

  it("marks any other recorded state as abnormal", () => {
    fixture.diagnostics = { driver: "sqlite", state: "unhealthy", stores: [] };
    render(<ControlDbBadge />);
    expect(screen.getByTestId("control-db-badge").textContent).toBe("DB unhealthy");
  });
});
