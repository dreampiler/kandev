import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { InboxItem } from "@/lib/state/slices/office/types";

const dismissInboxItem = vi.hoisted(() => vi.fn(() => Promise.resolve({ ok: true })));

vi.mock("@/lib/api/domains/office-extended-api", () => ({
  dismissInboxItem,
  retryProvider: vi.fn(),
}));

vi.mock("@/components/state-provider", () => ({
  useAppStore: (selector: (s: { office: { meta: unknown } }) => unknown) =>
    selector({ office: { meta: null } }),
}));

import { InboxItemRow } from "./inbox-item-row";

const agentErrorItem: InboxItem = {
  id: "ae-1",
  type: "agent_error",
  title: "Agent error",
  description: "boom",
  status: "active",
  entity_id: "agent-1",
  entity_type: "agent",
  createdAt: "2026-01-01T00:00:00Z",
  payload: { agent_profile_id: "agent-1" },
};

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("InboxItemRow agent_error", () => {
  it("renders a Mark fixed control", () => {
    render(<InboxItemRow item={agentErrorItem} />);

    expect(screen.getByTestId("inbox-mark-fixed-agent_error")).toBeTruthy();
  });

  it("dismisses with the agent_error kind and the item id", async () => {
    render(<InboxItemRow item={agentErrorItem} />);

    fireEvent.click(screen.getByTestId("inbox-mark-fixed-agent_error"));

    await waitFor(() => expect(dismissInboxItem).toHaveBeenCalledWith("agent_error", "ae-1"));
  });
});
