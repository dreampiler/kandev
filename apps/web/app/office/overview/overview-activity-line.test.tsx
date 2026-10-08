import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import type { OverviewWorkspaceActivity } from "@/lib/state/slices/office/overview-types";
import { OverviewActivityLine } from "./overview-activity-line";

const longKind = `${"x".repeat(200)} end of the agent's own error line`;

function activity(samples: { kind: string; count: number }[]): OverviewWorkspaceActivity {
  return {
    window_hours: 24,
    completed: 0,
    sessions_started: 4,
    sessions_failed: 2,
    agent_turns: 0,
    step_moves: 0,
    failure_buckets: [
      { code: "no_response", count: 1 },
      { code: "limit", count: 0 },
      { code: "start_failed", count: 1 },
      { code: "other", count: 0 },
    ],
    failure_samples: samples,
  };
}

describe("overview activity failure samples", () => {
  afterEach(cleanup);

  it("shows the agent's own first line verbatim instead of clipping it", () => {
    render(<OverviewActivityLine activity={activity([{ kind: longKind, count: 2 }])} />);
    fireEvent.click(screen.getByTestId("overview-failure-bucket-toggle"));
    const kind = within(screen.getByTestId("overview-failure-sample")).getByTitle(longKind);
    expect(kind.textContent).toBe(longKind);
    expect(kind.className).not.toContain("truncate");
  });
});
