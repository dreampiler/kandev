import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";

beforeAll(() => {
  // Radix Select needs these in jsdom; the repo does not otherwise polyfill them.
  if (!Element.prototype.hasPointerCapture) {
    Element.prototype.hasPointerCapture = () => false;
  }
  if (!Element.prototype.scrollIntoView) {
    Element.prototype.scrollIntoView = () => {};
  }
});

const syncSettings = vi.hoisted(() => vi.fn());
const setUserSettings = vi.hoisted(() => vi.fn());
const features = vi.hoisted(() => ({ needsYouInbox: false, multiTenancy: false }));

vi.mock("@/lib/user-settings-sync", () => ({
  createQueuedUserSettingsSyncWithResponse: () => syncSettings,
}));
vi.mock("@/hooks/domains/features/use-feature", () => ({
  useFeature: (name: keyof typeof features) => features[name],
}));
vi.mock("@/lib/api/domains/org-api", () => ({ getCurrentOrg: vi.fn() }));
vi.mock("@/components/state-provider", () => ({
  useAppStore: (selector: (state: unknown) => unknown) =>
    selector({
      userSettings: { officeOverviewScope: "office", officeOverviewRefreshSeconds: 60 },
      setUserSettings,
      getState: () => ({ userSettings: { officeOverviewScope: "office" } }),
    }),
  useAppStoreApi: () => ({ getState: () => ({ userSettings: { officeOverviewScope: "office" } }) }),
}));

import { OverviewHeader } from "./overview-header";
import { OverviewSystemCards } from "./overview-system-cards";

const system = {
  active_tasks: 3,
  running_sessions: 2,
  waiting_input_sessions: 1,
  session_limit: 8,
  queued_messages: 4,
  undeliverable_messages: 0,
  needs_human: 2,
  blocked_accounts: 1,
  problems: 1,
};

describe("overview reading state", () => {
  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it("shows every widget as analyzing instead of a number before the first read", () => {
    render(<OverviewSystemCards system={system} loading refreshSeconds={60} />);
    // Each widget says it is working; none shows its number, which would read
    // as a measurement of zero problems or nothing waiting.
    for (const id of [
      "overview-card-system",
      "overview-card-running-tasks",
      "overview-card-running-sessions",
      "overview-card-queue",
      "overview-card-needs-human",
      "overview-card-blocked-accounts",
    ]) {
      expect(screen.getByTestId(`${id}-analyzing`)).toBeTruthy();
      expect(within(screen.getByTestId(id)).queryByText("3")).toBeNull();
    }
  });

  it("centers the title, value, and sub line of every widget", () => {
    render(<OverviewSystemCards system={system} loading={false} refreshSeconds={60} />);
    const card = screen.getByTestId("overview-card-running-sessions");
    expect(card.querySelector(".flex-col.items-center.justify-center")).toBeTruthy();
  });

  it("reports an unknown circuit source instead of zero blocked models", () => {
    render(
      <OverviewSystemCards
        system={{ ...system, blocked_accounts_total: undefined }}
        loading={false}
        refreshSeconds={60}
      />,
    );
    const card = screen.getByTestId("overview-card-blocked-accounts");
    // The provider-health count is still shown, but the sub line says the
    // dynamic circuits did not answer.
    expect(within(card).getByText("1")).toBeTruthy();
    expect(within(card).getByText("Circuit state unknown")).toBeTruthy();
  });
});

describe("overview sync control", () => {
  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  function renderHeader(props: Partial<Parameters<typeof OverviewHeader>[0]> = {}) {
    const defaults = {
      onScopeChanged: vi.fn(),
      onRefresh: vi.fn(),
      refreshing: false,
      receivedAt: null as string | null,
      refreshSeconds: 60,
      onRefreshSecondsChanged: vi.fn(),
    };
    const merged = { ...defaults, ...props };
    render(<OverviewHeader {...merged} />);
    return merged;
  }

  it("says nothing has been synced before the first accepted read", () => {
    renderHeader();
    expect(screen.getByTestId("overview-last-sync").textContent).toBe("Not synced yet");
  });

  it("spins the sync icon only while a read is in flight", () => {
    renderHeader({ refreshing: true });
    const button = screen.getByTestId("overview-sync-button");
    expect(button.getAttribute("aria-busy")).toBe("true");
    expect(button.querySelector(".animate-spin")).toBeTruthy();
  });

  it("runs the refresh from the sync button", () => {
    const header = renderHeader({ receivedAt: "2026-10-04T00:00:00Z" });
    expect(screen.getByTestId("overview-last-sync").textContent).toContain("Last synced");
    fireEvent.click(screen.getByTestId("overview-sync-button"));
    expect(header.onRefresh).toHaveBeenCalled();
  });

  it("persists a chosen auto-refresh period", async () => {
    syncSettings.mockResolvedValue({ settings: {} });
    const header = renderHeader();
    const trigger = screen.getByTestId("overview-refresh-interval");
    expect(trigger.textContent).toContain("every 60 seconds");
    fireEvent.click(trigger);
    const option = await screen.findByText("every 30 seconds");
    await act(async () => {
      fireEvent.click(option);
    });
    expect(header.onRefreshSecondsChanged).toHaveBeenCalledWith(30);
    expect(syncSettings).toHaveBeenCalledWith(30);
  });
});
