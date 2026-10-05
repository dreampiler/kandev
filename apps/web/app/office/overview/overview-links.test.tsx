import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { WorkspaceAggregateEntry } from "@/lib/state/slices/office/types";

const boardWorkspaceID = "ws-1";
const oneWorkspaceNames = { [boardWorkspaceID]: "Board One" };
const failedSessionHref = "/t/task-f?sessionId=sess-f";
const fastProfileHref = "/settings/agents/claude-code/profiles/prof-1";
const eventRowTestId = "overview-event";

const getWorkspaceAggregateTasks = vi.hoisted(() => vi.fn());
const getWorkspaceAggregateRunning = vi.hoisted(() => vi.fn());
const getWorkspaceOverview = vi.hoisted(() => vi.fn());
const features = vi.hoisted(() => ({ needsYouInbox: true, multiTenancy: false }));

vi.mock("@/lib/api/domains/office-overview-api", () => ({
  getWorkspaceAggregateTasks,
  getWorkspaceAggregateRunning,
  getWorkspaceOverview,
}));
vi.mock("@/hooks/domains/office/use-workspace-overview", () => ({
  useWorkspaceOverview: (workspaceId: string) => {
    const entry = getWorkspaceOverview(workspaceId);
    return entry ? { status: "loaded", entry } : { status: "error" };
  },
}));
vi.mock("@/hooks/domains/settings/use-agent-profile-usage", () => ({
  useAgentProfileUsage: () => undefined,
  useAgentProfileUsageList: () => ({ byProfile: new Map(), loaded: false }),
}));
vi.mock("@kandev/ui/tooltip", () => ({
  TooltipProvider: ({ children }: { children: React.ReactNode }) => children,
  Tooltip: ({ children }: { children: React.ReactNode }) => children,
  TooltipTrigger: ({ children }: { children: React.ReactNode }) => children,
  TooltipContent: ({ children }: { children: React.ReactNode }) => children,
}));
vi.mock("@/components/state-provider", () => ({
  useAppStore: (selector: (state: unknown) => unknown) =>
    selector({ userSettings: { startupPage: "task_overview" } }),
}));
vi.mock("@/hooks/domains/features/use-feature", () => ({
  useFeature: (name: keyof typeof features) => features[name],
}));

import { OverviewWorkspaceCard } from "./overview-workspace-card";
import { OverviewSystemCards } from "./overview-system-cards";
import { OverviewLast24h, OverviewNeedsHuman } from "./overview-sections";
import { OverviewModels } from "./overview-models";

const createdAt = "2026-10-03T00:00:00Z";

const workspace: WorkspaceAggregateEntry = {
  workspace_id: boardWorkspaceID,
  name: "Board One",
  task_count: 3,
  open_tasks: 3,
  in_progress_tasks: 1,
  blocked_tasks: 0,
  done_tasks: 0,
  pending_approvals: 0,
  agent_count: 0,
  running_agents: 0,
  is_office: false,
  metrics: {
    status: "stalled",
    active_tasks: 2,
    running_sessions: 1,
    waiting_input_sessions: 0,
    last_output_at: createdAt,
    last_output_task_id: "task-out",
    queued_messages: 1,
    completed_24h: 4,
    open_tasks: 3,
    waiting_tasks: 0,
    blocked_tasks: 0,
    problems: { error: 0, stalled: 1, delayed: 0 },
    top_warning: {
      task_id: "task-warn",
      task_title: "Warn",
      status: "stalled",
      reason: { code: "no_output", values: { minutes: 20 } },
    },
  },
  parents: [
    { task_id: "task-parent", title: "Round", status: "running", children: 3, open_children: 2 },
  ],
};

const taskPage = {
  kind: "tasks",
  filter: "problems",
  total: 1,
  tasks: [
    {
      task_id: "task-row",
      title: "Row task",
      step_name: "Build",
      state: "IN_PROGRESS",
      workspace_id: boardWorkspaceID,
      workspace_name: "Board One",
      status: "stalled",
      reason: { code: "no_output", values: { minutes: 20 } },
      session_id: "sess-row",
      session_state: "RUNNING",
      failures_24h: 0,
      step_entered_at: createdAt,
      queued_messages: 0,
    },
  ],
};

const sessionsPage = {
  kind: "sessions",
  total: 1,
  sessions: [
    {
      session_id: "sess-9",
      task_id: "task-9",
      task_title: "Session task",
      workspace_id: boardWorkspaceID,
      workspace_name: "Board One",
      session_state: "RUNNING",
      status: "running",
      started_at: createdAt,
    },
  ],
};

const queuePage = {
  kind: "queue",
  total: 1,
  queue: [
    {
      session_id: "sess-q",
      task_id: "task-q",
      task_title: "Queue task",
      workspace_id: boardWorkspaceID,
      workspace_name: "Board One",
      status: "undeliverable",
      count: 2,
      oldest_at: createdAt,
      sender: "user",
      session_state: "CANCELLED",
      first_line: "retry the deploy",
    },
  ],
};

const system = {
  active_tasks: 1,
  running_sessions: 1,
  waiting_input_sessions: 0,
  session_limit: 8,
  queued_messages: 2,
  undeliverable_messages: 2,
  needs_human: 1,
  blocked_accounts: 0,
  blocked_accounts_total: 0,
  problems: 0,
};

// Every test drives the card through a synchronous stub of its own overview
// read, so the card renders the loaded entry instead of waiting on a promise.
function useLoadedWorkspace(entry: WorkspaceAggregateEntry = workspace) {
  getWorkspaceOverview.mockImplementation((id: string) =>
    id === entry.workspace_id ? entry : null,
  );
}

function hrefOf(element: HTMLElement): string | null {
  return element.closest("a")?.getAttribute("href") ?? null;
}

describe("overview project card destinations", () => {
  beforeEach(() => {
    getWorkspaceAggregateTasks.mockResolvedValue(taskPage);
    useLoadedWorkspace();
  });
  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it("links the project card to its board, tasks, last output, parent and warning", async () => {
    render(<OverviewWorkspaceCard workspace={workspace} refreshSeconds={60} />);
    expect(hrefOf(screen.getByTestId("overview-workspace-link"))).toBe(
      "/?home=overview&workspaceId=ws-1",
    );
    expect(hrefOf(screen.getByTestId("overview-parent-task"))).toBe("/t/task-parent");
    expect(hrefOf(screen.getByTestId("overview-warning"))).toBe("/t/task-warn");
    expect(hrefOf(screen.getByText("Last output"))).toBe("/t/task-out");
    expect(getWorkspaceAggregateTasks).not.toHaveBeenCalled();

    const expand = screen.getByTestId("overview-workspace-expand");
    expect(expand.getAttribute("aria-expanded")).toBe("false");
    expect(expand.textContent).toContain("Show details");
    await act(async () => {
      fireEvent.click(expand);
    });
    const collapse = screen.getByTestId("overview-workspace-expand");
    expect(collapse.getAttribute("aria-expanded")).toBe("true");
    expect(collapse.textContent).toContain("Hide details");
    // The problem list is read in full, and the task list is not read until a
    // filter chip asks for it.
    expect(getWorkspaceAggregateTasks).toHaveBeenCalledWith("ws-1", "problems", 500, {
      cache: "no-store",
    });
    const taskList = await screen.findByTestId("overview-workspace-task-list");
    const row = await within(taskList).findByTestId("overview-task-row");
    expect(hrefOf(within(row).getByText("Row task"))).toBe("/t/task-row");

    await act(async () => {
      fireEvent.click(screen.getByText("Completed (24 h)"));
    });
    expect(getWorkspaceAggregateTasks).toHaveBeenLastCalledWith("ws-1", "completed", 50, {
      cache: "no-store",
    });
  });

  it("links an Office workspace to its Office home", () => {
    render(
      <OverviewWorkspaceCard workspace={{ ...workspace, is_office: true }} refreshSeconds={60} />,
    );
    expect(hrefOf(screen.getByTestId("overview-workspace-link"))).toBe("/office?workspaceId=ws-1");
  });
});

// The repeated question carries its occurrence count; the approval does not.
const humanItems = [
  {
    kind: "question" as const,
    id: "p1",
    workspace_id: boardWorkspaceID,
    workspace_name: "Board One",
    task_id: "task-q1",
    task_title: "Q",
    session_id: "sess-q1",
    count: 3,
    created_at: createdAt,
  },
  {
    kind: "approval" as const,
    id: "a1",
    workspace_id: "ws-2",
    workspace_name: "Office",
    approval_type: "hire_agent",
    count: 1,
    created_at: createdAt,
  },
];

const eventItems = [
  {
    kind: "session_failed" as const,
    at: createdAt,
    workspace_id: boardWorkspaceID,
    task_id: "task-f",
    session_id: "sess-f",
    title: "Failed task",
    detail: "AI_APICallError: Rate limit exceeded",
  },
];

const modelItems = [
  {
    agent_profile_id: "prof-1",
    agent_id: "agent-1",
    agent_name: "claude-code",
    name: "Fast",
    kind: "concrete" as const,
    sessions_24h: 3,
    running: 1,
    failed_24h: 1,
    errors: [{ kind: "AI_APICallError: Rate limit exceeded", count: 1 }],
  },
];

describe("overview list and section destinations", () => {
  beforeEach(() => {
    getWorkspaceAggregateRunning.mockImplementation(async (kind: string) =>
      kind === "sessions" ? sessionsPage : queuePage,
    );
  });
  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it("opens the running lists and links rows to the task and session", async () => {
    render(<OverviewSystemCards system={system} loading={false} refreshSeconds={60} />);
    const sessionsCard = within(screen.getByTestId("overview-card-running-sessions")).getByRole(
      "button",
    );
    await act(async () => {
      fireEvent.click(sessionsCard);
    });
    expect(sessionsCard.getAttribute("aria-expanded")).toBe("true");
    const sessionRow = await screen.findByTestId("overview-session-row");
    expect(hrefOf(within(sessionRow).getByText("Session task"))).toBe("/t/task-9?sessionId=sess-9");

    await act(async () => {
      fireEvent.click(within(screen.getByTestId("overview-card-queue")).getByRole("button"));
    });
    const queueRow = await screen.findByTestId("overview-queue-row");
    expect(hrefOf(within(queueRow).getByText("Queue task"))).toBe("/t/task-q?sessionId=sess-q");
    expect(within(queueRow).getByText(/retry the deploy/)).toBeTruthy();
    const needsYou = within(screen.getByTestId("overview-card-needs-human")).getByText("Needs you");
    expect(hrefOf(needsYou)).toBe("#overview-needs-human");
  });

  it("links people items, events and models to their screens", async () => {
    render(
      <>
        <OverviewNeedsHuman items={humanItems} />
        <OverviewLast24h events={eventItems} workspaceNames={oneWorkspaceNames} />
        <OverviewModels models={modelItems} blocked={[]} circuits={[]} circuitsKnown />
      </>,
    );
    const items = screen.getAllByTestId("overview-human-item");
    expect(items[0].getAttribute("href")).toBe("/t/task-q1?sessionId=sess-q1");
    expect(items[1].getAttribute("href")).toBe("/office/inbox?workspaceId=ws-2");
    // A repeated question is one row that says how many times it was asked.
    expect(within(items[0]).getByTestId("overview-human-repeat").textContent).toBe("3 times");
    expect(within(items[1]).queryByTestId("overview-human-repeat")).toBeNull();
    expect(hrefOf(screen.getByText("Open inbox"))).toBe("/needs-you-inbox");
    expect(screen.getByTestId(eventRowTestId).getAttribute("href")).toBe(failedSessionHref);
    // A model card sits inside its provider account, so the account opens first.
    await act(async () => {
      fireEvent.click(within(screen.getByTestId("overview-account")).getByRole("button"));
    });
    expect(hrefOf(screen.getByText("Fast"))).toBe(fastProfileHref);
  });
});

describe("overview automation destination", () => {
  afterEach(cleanup);

  it("opens an automation event's originating configuration", () => {
    render(
      <OverviewLast24h
        events={[
          {
            kind: "automation_run",
            at: createdAt,
            workspace_id: boardWorkspaceID,
            automation_id: "auto 1",
            task_id: "task-auto",
            title: "Scheduled digest",
          },
        ]}
        workspaceNames={oneWorkspaceNames}
      />,
    );
    expect(screen.getByTestId(eventRowTestId).getAttribute("href")).toBe(
      "/settings/workspaces/ws-1/automations/auto%201",
    );
  });
});

describe("overview last-24-hours kind filter", () => {
  afterEach(cleanup);

  const kinds = [
    {
      kind: "task_created" as const,
      at: createdAt,
      workspace_id: boardWorkspaceID,
      task_id: "t-new",
      title: "New task",
    },
    {
      kind: "task_completed" as const,
      at: createdAt,
      workspace_id: boardWorkspaceID,
      task_id: "t-done",
      title: "Done task",
    },
    { kind: "server_started" as const, at: createdAt, title: "Server started" },
  ];

  it("counts every kind, then narrows the list to the chosen one", () => {
    render(<OverviewLast24h events={kinds} workspaceNames={oneWorkspaceNames} />);
    const chips = within(screen.getByTestId("overview-event-kinds"));
    expect(chips.getByTestId("overview-event-kind-all").textContent).toContain("3");
    expect(chips.getByTestId("overview-event-kind-task_created").textContent).toContain("1");
    // A server start has no task to open, so only the other two are links.
    expect(screen.getAllByTestId(eventRowTestId)).toHaveLength(2);

    fireEvent.click(chips.getByTestId("overview-event-kind-task_created"));

    const remaining = screen.getAllByTestId(eventRowTestId);
    expect(remaining).toHaveLength(1);
    expect(remaining[0].textContent).toContain("New task");
  });
});
