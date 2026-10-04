import { cleanup, fireEvent, render, screen, within, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { WorkspacePauseViewProps } from "./workspace-pause-banner";
import { WorkspacePauseState, WorkspacePauseTopbarActions } from "./workspace-pause-banner";

afterEach(() => {
  cleanup();
});

function view(overrides: Partial<WorkspacePauseViewProps> = {}): WorkspacePauseViewProps {
  const action = vi.fn().mockResolvedValue({ ok: true });
  return {
    activeWorkspaceId: "ws-1",
    record: null,
    status: "known",
    refresh: action,
    pause: action,
    retryPause: action,
    resume: action,
    sweep: null,
    isMutating: false,
    ...overrides,
  };
}

describe("WorkspacePauseTopbarActions", () => {
  it("renders desktop actions and a mobile workspace-actions entry point", () => {
    render(<WorkspacePauseTopbarActions view={view()} />);

    expect(screen.getByTestId("office-workspace-topbar-actions")).toBeTruthy();
    expect(screen.getByTestId("office-workspace-actions-trigger")).toBeTruthy();
  });
});

const PAUSED_BANNER = "office-workspace-paused-banner";
const PAUSE_LOADING = "office-workspace-pause-loading";
const PAUSE_UNAVAILABLE = "office-workspace-pause-unavailable";

function pausedRecord(): NonNullable<WorkspacePauseViewProps["record"]> {
  return {
    id: "pause-1",
    reason: "maintenance",
    createdBy: "default-user",
    createdByKind: "user",
    createdAt: "2026-09-17T00:00:00Z",
  };
}

describe("WorkspacePauseState", () => {
  it("keeps the paused banner informational and leaves controls in the topbar", () => {
    render(<WorkspacePauseState view={view({ record: pausedRecord() })} />);

    expect(screen.getByTestId(PAUSED_BANNER)).toBeTruthy();
    expect(screen.queryByTestId("office-pause-workspace-button")).toBeNull();
    expect(screen.queryByTestId("office-resume-workspace-button")).toBeNull();
  });

  it("shows a reading indicator, not the failure warning, while loading with no record", () => {
    render(<WorkspacePauseState view={view({ record: null, status: "loading" })} />);

    expect(screen.getByTestId(PAUSE_LOADING)).toBeTruthy();
    expect(screen.queryByTestId(PAUSE_UNAVAILABLE)).toBeNull();
    expect(screen.queryByTestId(PAUSED_BANNER)).toBeNull();
  });

  it("shows a reading indicator before the first read with no record", () => {
    render(<WorkspacePauseState view={view({ record: null, status: "unknown" })} />);

    expect(screen.getByTestId(PAUSE_LOADING)).toBeTruthy();
    expect(screen.queryByTestId(PAUSE_UNAVAILABLE)).toBeNull();
  });

  it("shows the failure warning only on an actual read failure with no record", () => {
    render(<WorkspacePauseState view={view({ record: null, status: "error" })} />);

    expect(screen.getByTestId(PAUSE_UNAVAILABLE)).toBeTruthy();
    expect(screen.queryByTestId(PAUSE_LOADING)).toBeNull();
    expect(screen.queryByTestId(PAUSED_BANNER)).toBeNull();
  });

  it("renders nothing for a workspace confirmed running", () => {
    render(<WorkspacePauseState view={view({ record: null, status: "known" })} />);

    expect(screen.queryByTestId(PAUSED_BANNER)).toBeNull();
    expect(screen.queryByTestId(PAUSE_LOADING)).toBeNull();
    expect(screen.queryByTestId(PAUSE_UNAVAILABLE)).toBeNull();
  });

  it("keeps a read record on screen without a stale mark while re-reading", () => {
    render(<WorkspacePauseState view={view({ status: "loading", record: pausedRecord() })} />);

    expect(screen.getByTestId(PAUSED_BANNER)).toBeTruthy();
    expect(screen.queryByText("May be stale")).toBeNull();
  });

  it("marks a read record stale after a failed re-read", () => {
    render(<WorkspacePauseState view={view({ status: "error", record: pausedRecord() })} />);

    expect(screen.getByTestId(PAUSED_BANNER)).toBeTruthy();
    expect(screen.getByText("May be stale")).toBeTruthy();
  });
});

it.each([false, true])(
  "keeps mobile confirmation mounted after drawer close (paused=%s)",
  async (paused) => {
    const props = view(
      paused
        ? {
            record: {
              id: "p",
              reason: "maintenance",
              createdBy: "user",
              createdByKind: "user",
              createdAt: "2026-09-17T00:00:00Z",
            },
          }
        : {},
    );
    render(<WorkspacePauseTopbarActions view={props} />);
    fireEvent.click(screen.getByTestId("office-workspace-actions-trigger"));
    const drawer = screen.getByTestId("office-workspace-actions-drawer");
    const action = paused ? "resume" : "pause";
    fireEvent.click(within(drawer).getByTestId(`office-${action}-workspace-button`));
    fireEvent.animationEnd(drawer);
    expect(screen.queryByTestId(`office-${action}-workspace-dialog`)).not.toBeNull();
    if (!paused)
      fireEvent.change(screen.getByTestId("office-pause-reason-input"), {
        target: { value: "maintenance" },
      });
    fireEvent.click(screen.getByTestId(`office-${action}-confirm-button`));
    await waitFor(() => expect(paused ? props.resume : props.pause).toHaveBeenCalled());
  },
);
