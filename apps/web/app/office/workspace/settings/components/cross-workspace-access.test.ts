import { describe, expect, it } from "vitest";
import {
  emptyCrossWorkspaceAccess,
  isCrossWorkspaceDirty,
  setCoordinatorTask,
  toCrossWorkspaceDraft,
  toCrossWorkspacePayload,
  toggleAllowedWorkspace,
} from "./cross-workspace-access";

describe("cross-workspace access draft", () => {
  it("defaults to off when the server sends nothing", () => {
    expect(toCrossWorkspaceDraft(undefined)).toEqual(emptyCrossWorkspaceAccess());
    expect(emptyCrossWorkspaceAccess().enabled).toBe(false);
  });

  it("toggles allow-list entries without duplicates and in stable order", () => {
    let draft = emptyCrossWorkspaceAccess();
    draft = toggleAllowedWorkspace(draft, "ws-b", true);
    draft = toggleAllowedWorkspace(draft, "ws-a", true);
    draft = toggleAllowedWorkspace(draft, "ws-b", true);
    expect(draft.allowed_workspace_ids).toEqual(["ws-a", "ws-b"]);
    draft = toggleAllowedWorkspace(draft, "ws-a", false);
    expect(draft.allowed_workspace_ids).toEqual(["ws-b"]);
  });

  it("drops blank coordinator entries and trims ids in the payload", () => {
    let draft = { ...emptyCrossWorkspaceAccess(), enabled: true };
    draft = setCoordinatorTask(draft, "ws-a", "  task-1 ");
    draft = setCoordinatorTask(draft, "ws-b", "   ");
    expect(toCrossWorkspacePayload(draft)).toEqual({
      enabled: true,
      allowed_workspace_ids: [],
      coordinator_task_ids: { "ws-a": "task-1" },
    });
  });

  it("is dirty only when the normalized value changes", () => {
    const saved = toCrossWorkspaceDraft({
      enabled: true,
      allowed_workspace_ids: ["ws-b", "ws-a"],
      coordinator_task_ids: { "ws-a": "task-1" },
    });
    expect(isCrossWorkspaceDirty(saved, saved)).toBe(false);
    expect(isCrossWorkspaceDirty(saved, setCoordinatorTask(saved, "ws-b", " "))).toBe(false);
    expect(isCrossWorkspaceDirty(saved, { ...saved, enabled: false })).toBe(true);
    expect(isCrossWorkspaceDirty(saved, toggleAllowedWorkspace(saved, "ws-c", true))).toBe(true);
    expect(isCrossWorkspaceDirty(saved, setCoordinatorTask(saved, "ws-a", "task-2"))).toBe(true);
  });
});
