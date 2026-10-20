import { describe, expect, it } from "vitest";
import { conversationSessionStates, workspaceCoordinatorState } from "./control-coordinator-status";

const coordinator = (open_proposals: number, conversation_task_id: string | null) => ({
  open_proposals,
  conversation_task_id,
});

describe("workspace coordinator state", () => {
  it("reads no conversation as none", () => {
    expect(workspaceCoordinatorState([], new Map())).toBe("none");
    expect(workspaceCoordinatorState([coordinator(0, null)], new Map())).toBe("none");
  });

  it("lets a pending proposal outrank the conversation's own state", () => {
    expect(
      workspaceCoordinatorState(
        [coordinator(2, "task-1")],
        conversationSessionStates([{ id: "task-1", primary_session_state: "RUNNING" }]),
      ),
    ).toBe("responding");
  });

  it("reads a conversation with a live session as talking", () => {
    expect(
      workspaceCoordinatorState(
        [coordinator(0, "task-1")],
        conversationSessionStates([{ id: "task-1", primary_session_state: "RUNNING" }]),
      ),
    ).toBe("talking");
    expect(
      workspaceCoordinatorState(
        [coordinator(0, "task-1")],
        conversationSessionStates([{ id: "task-1", primary_session_state: "STARTING" }]),
      ),
    ).toBe("talking");
  });

  it("reads a conversation outside the bounded task sample as waiting, not talking", () => {
    expect(
      workspaceCoordinatorState(
        [coordinator(0, "task-1")],
        conversationSessionStates([{ id: "other", primary_session_state: "RUNNING" }]),
      ),
    ).toBe("waiting");
    expect(
      workspaceCoordinatorState(
        [coordinator(0, "task-1")],
        conversationSessionStates([{ id: "task-1", primary_session_state: "IDLE" }]),
      ),
    ).toBe("waiting");
  });

  it("prefers a live conversation over a waiting sibling", () => {
    expect(
      workspaceCoordinatorState(
        [coordinator(0, "task-1"), coordinator(0, "task-2")],
        conversationSessionStates([
          { id: "task-1", primary_session_state: "IDLE" },
          { id: "task-2", primary_session_state: "RUNNING" },
        ]),
      ),
    ).toBe("talking");
  });
});
