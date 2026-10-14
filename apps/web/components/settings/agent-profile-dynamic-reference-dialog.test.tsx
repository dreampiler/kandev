import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";

import {
  AgentProfileDynamicReferenceDialog,
  resolveEffectiveOperation,
} from "./agent-profile-dynamic-reference-dialog";
import type { DynamicProfileReference } from "@/lib/types/agent-profile-errors";

afterEach(cleanup);

const references: DynamicProfileReference[] = [
  {
    id: "dynamic-1",
    name: "Frontier",
    deleted: false,
    version: 7,
    routes: [{ position: 0, tier: 1, enabled: true }],
  },
  {
    id: "dynamic-2",
    name: "Cost Optimizer",
    deleted: false,
    version: 4,
    routes: [{ position: 2, tier: 2, enabled: true }],
  },
];

describe("AgentProfileDynamicReferenceDialog", () => {
  it("names each referencing profile and its tier, and offers the disable/remove choices", () => {
    const onChoose = vi.fn();
    render(
      <AgentProfileDynamicReferenceDialog
        profileName="gpt-balanced"
        mode="disable"
        references={references}
        pending={false}
        onCancel={() => {}}
        onChoose={onChoose}
      />,
    );

    expect(screen.getByText("Disable gpt-balanced?")).toBeTruthy();
    const rows = screen.getAllByTestId("agent-profile-dynamic-reference-row");
    expect(rows).toHaveLength(2);
    expect(rows[0].textContent).toContain("Frontier");
    expect(rows[0].textContent).toContain("tier 1, candidate 1");
    expect(rows[1].textContent).toContain("Cost Optimizer");
    expect(rows[1].textContent).toContain("tier 2, candidate 3");

    fireEvent.click(screen.getByTestId("agent-profile-dynamic-reference-disable"));
    expect(onChoose).toHaveBeenCalledWith("disable_candidate");
    fireEvent.click(screen.getByTestId("agent-profile-dynamic-reference-remove"));
    expect(onChoose).toHaveBeenCalledWith("remove_candidate");
  });

  it("states the profile is not deleted on the delete entry", () => {
    render(
      <AgentProfileDynamicReferenceDialog
        profileName="gpt-balanced"
        mode="delete"
        references={references}
        pending={false}
        onCancel={() => {}}
        onChoose={() => {}}
      />,
    );
    const dialog = screen.getByTestId("agent-profile-dynamic-reference-dialog");
    expect(within(dialog).getByText("Delete gpt-balanced?")).toBeTruthy();
    expect(
      within(dialog).getByText(
        "Disable the candidate and disable this profile instead of deleting it",
      ),
    ).toBeTruthy();
    expect(within(dialog).getByText("Remove the candidate and delete this profile")).toBeTruthy();
  });
});

describe("resolveEffectiveOperation", () => {
  it("disables the profile when the candidate is disabled, on either entry point", () => {
    expect(resolveEffectiveOperation("disable", "disable_candidate")).toBe("disable");
    expect(resolveEffectiveOperation("delete", "disable_candidate")).toBe("disable");
  });

  it("keeps the entry operation when the candidate is removed", () => {
    expect(resolveEffectiveOperation("disable", "remove_candidate")).toBe("disable");
    expect(resolveEffectiveOperation("delete", "remove_candidate")).toBe("delete");
  });
});
