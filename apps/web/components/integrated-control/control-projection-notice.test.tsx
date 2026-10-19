import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { ControlProjectionNotice } from "./control-projection-notice";

afterEach(cleanup);
describe("partial source disclosure", () => {
  it("retains the source reason beside partial measurements", () => {
    render(
      <ControlProjectionNotice
        status={{ state: "partial", reasons: ["server_start_history_not_supported"] }}
      />,
    );
    expect(screen.getByRole("status").textContent).toContain("server_start_history_not_supported");
  });
  it("does not label a known projection as partial", () => {
    render(<ControlProjectionNotice status={{ state: "known", reasons: [] }} />);
    expect(screen.queryByRole("status")).toBeNull();
  });
});
