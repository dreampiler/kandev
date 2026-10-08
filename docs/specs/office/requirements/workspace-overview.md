---
status: draft
system: office
created: 2026-09-29
owners:
  - kandev
---

# Office Workspace Overview Requirements

## Overview

An operator with several workspaces needs one view of workspace health.
The view shows task counts, pending approvals, agent counts, and recent activity. It also shows running work, sessions, queued messages, problems, items that wait on a person, and model health. It does not add a write path or change workspace access.

## Requirements

### REQ-OFFICE-WORKSPACE-OVERVIEW-001: Read-only workspace overview

**Intent:** An operator can review the state of each Office workspace that the operator can access from one page.

**User story:** As an operator, I want to review several Office workspaces on one page, so that I can find work that needs attention.

#### Acceptance criteria

- **AC-OFFICE-WORKSPACE-OVERVIEW-001.1:** The Office navigation shall provide an Overview page on desktop and phone layouts.
- **AC-OFFICE-WORKSPACE-OVERVIEW-001.2:** The overview endpoint shall reject agent tokens in every authentication mode. When authentication is enabled, it shall require a real, non-synthetic user identity.
- **AC-OFFICE-WORKSPACE-OVERVIEW-001.3:** The endpoint shall return only workspaces from the caller's workspace list, narrowed by the caller's overview scope (REQ-OFFICE-WORKSPACE-OVERVIEW-002). It shall not change the existing workspace access rules.
- **AC-OFFICE-WORKSPACE-OVERVIEW-001.4:** Each workspace row shall show total, open, in-progress, blocked, and done task counts, pending approvals, total agents, and running agents.
- **AC-OFFICE-WORKSPACE-OVERVIEW-001.5:** The overview shall show the latest 20 activity entries across the returned workspaces, in newest-first order.
- **AC-OFFICE-WORKSPACE-OVERVIEW-001.6:** A workspace row shall open that workspace. An activity link shall keep the activity workspace in its destination.
- **AC-OFFICE-WORKSPACE-OVERVIEW-001.7:** The page shall show loading, error, and empty states. It shall refresh while mounted and keep loaded data when a later refresh fails.

### REQ-OFFICE-WORKSPACE-OVERVIEW-002: Overview scope preference

**Intent:** An operator chooses whether the overview covers only Office workspaces or every workspace the operator can open.

**User story:** As an operator who also runs Kanban workspaces, I want the overview to include them when I choose, so that one page covers all of my work.

#### Acceptance criteria

- **AC-OFFICE-WORKSPACE-OVERVIEW-002.1:** A per-user setting `office_overview_scope` shall accept `office` and `reachable`. The default and any unknown or invalid stored value shall be `office`.
- **AC-OFFICE-WORKSPACE-OVERVIEW-002.2:** With `office`, the overview shall show the caller's Office workspaces. With `reachable`, it shall show every workspace in the caller's workspace list. Neither value shall add a workspace the caller cannot already open.
- **AC-OFFICE-WORKSPACE-OVERVIEW-002.3:** The overview page shall let the user change the scope. When Organizations is enabled, the page shall name the caller's organization.

### REQ-OFFICE-WORKSPACE-OVERVIEW-003: Overview sections and lists

**Intent:** An operator sees what is running, stuck, or waiting across the workspaces in scope, and opens the exact task or session from any row.

**User story:** As an operator, I want running tasks, AI sessions, and queued messages separated and explained, so that I can act on the stuck ones first.

#### Acceptance criteria

- **AC-OFFICE-WORKSPACE-OVERVIEW-003.1:** The overview shall show system cards for uptime, running tasks, running AI sessions against the session limit, queued messages with undeliverable messages, items waiting on a person, and blocked model accounts.
- **AC-OFFICE-WORKSPACE-OVERVIEW-003.1a:** The running-sessions card shall read the session limit from the configured session capacity, on every overview computation rather than once at start, so a limit changed in Settings is reflected without a restart. The general and control lanes shall each be shown against the limit that lane is admitted under, and a limit of zero (an unlimited general lane, or no control lane) shall be shown as that lane's count alone. A limit reading that did not arrive shall be shown as no limit at all rather than as zero. Each lane shall also show that lane's own sessions waiting on a person, split from the scope's waiting-input count by the same control profile set the lanes were classified with, so the two lane counts add up to the scope's waiting count. A lane whose waiting reading did not arrive shall show no waiting line rather than the scope total.
- **AC-OFFICE-WORKSPACE-OVERVIEW-003.2:** Each workspace shall show tasks in progress, running AI sessions, last agent output, queued messages, tasks completed in 24 hours, and open tasks, plus problem counts. A workspace with work shall also name the tasks that have a session executing right now, most severe first and bounded, each with its own running and waiting-on-a-person session counts, so the card leads with the work in front of the operator instead of repeating parent task groups and the most severe warning that the card already counts elsewhere. The last-output figure shall open the task whose most recent live session produced that output, and shall say so on hover. The expanded task list shall report each row's own count of sessions waiting on a person.
- **AC-OFFICE-WORKSPACE-OVERVIEW-003.2a:** The 24-hour completed figure shall count the tasks that entered a step configured to complete their task inside the window, not the tasks whose last write falls inside it, so archiving or editing a task that finished earlier cannot pull it back into the figure. Pressing the figure shall open that same set of tasks as a list, and the figure and the list shall report the same number because they are read from one result. Each row shall report when the task finished and shall mark a task that has left the board, since a completed list is mostly archived rows and without that mark the reader cannot tell why a task they just saw finish is on no board.
- **AC-OFFICE-WORKSPACE-OVERVIEW-003.3:** Each open task shall have one status: error, possibly stalled, delayed, running, waiting, or on hold. Status shall come from task and session states and fixed time limits, not from workflow step names. The hold count is the one exception: it also counts a task parked on a step whose stored nature marks it as a hold step. That nature is a stored value on the step, not its name, so renaming a hold step keeps the reading and naming a step "hold" does not create one. Every other status, and every problem and delay reading, still comes from task and session states, one step automation property, and fixed time limits.
- **AC-OFFICE-WORKSPACE-OVERVIEW-003.3f:** The open-task figure shall break down into the tasks on hold, the tasks waiting on an unfinished predecessor, and the remaining tasks, each open task counted once and the three adding up to the open-task figure. The remaining figure shall not be worded as if none of those tasks were moving, because the tasks in progress are part of it. A task that is both on hold and waiting on a predecessor shall be counted as on hold only.
- **AC-OFFICE-WORKSPACE-OVERVIEW-003.3a:** A problem count shall count only what needs a hand now, and shall fall again on the next read once the situation clears. A task deliberately waiting, deliberately stopped, or carrying an error it has already moved past shall not be counted as a problem.
- **AC-OFFICE-WORKSPACE-OVERVIEW-003.3b:** A task's condition shall be read from the session that is the task's own: its primary session when it has one, otherwise a live session, otherwise its newest. A failure of a session the task ran alongside shall not speak for the task. A task with a turn running shall not be reported as an error on the strength of an earlier session.
- **AC-OFFICE-WORKSPACE-OVERVIEW-003.3c:** A session that was stopped on purpose, including one carrying the stop as its error text, shall not be an error. A session failure from before the time the task entered its current step shall not be an error either.
- **AC-OFFICE-WORKSPACE-OVERVIEW-003.3d:** Time in the current step shall make a task delayed only when the task is somewhere it is meant to leave on its own. A task on hold, a task with an open prerequisite, and a task waiting on a person shall take no dwell limit and shall be reported as on hold or waiting. A prerequisite that finished without the task moving on shall still be reported as not advancing.
- **AC-OFFICE-WORKSPACE-OVERVIEW-003.3e:** A task waiting on a decision from the owner shall be waiting, not delayed, and shall be listed among the items that wait on a person. The same read of answerable questions shall serve both.
- **AC-OFFICE-WORKSPACE-OVERVIEW-003.4:** The server shall send each reason as a code with values. The page shall phrase it in the user's language. An agent's own error text shall be shown verbatim.
- **AC-OFFICE-WORKSPACE-OVERVIEW-003.5:** A workspace task list and the cross-workspace lists of running tasks, running sessions, and queued messages shall load only while expanded. Task rows shall open `/t/<task>`. Session and queued-message rows shall open `/t/<task>?sessionId=<session>`.
- **AC-OFFICE-WORKSPACE-OVERVIEW-003.6:** The server shall compute one overview per caller and scope at most every 20 seconds and share one computation among concurrent requests. It shall only read, and it shall never read message content or metadata. It shall read the head of a queued message only for the expanded queue list.
- **AC-OFFICE-WORKSPACE-OVERVIEW-003.7:** The page and expanded lists shall refresh every 30 seconds, and only while the page is visible.
- **AC-OFFICE-WORKSPACE-OVERVIEW-003.8:** Each workspace card shall show a period statistics line with five totals read from existing tables: tasks completed, sessions started, failed sessions, agent turns, and step moves. The line shall state the period it covers, and the page shall let the operator select that period from 24 hours, 7 days, and 30 days, defaulting to 24 hours. The selected period shall apply to this line only; every other overview section shall keep its own fixed window. The totals shall be exact aggregates over the whole period, not a bounded sample, and automation-run and ephemeral tasks shall be excluded as everywhere else on the overview. The line shall show for a workspace with no work in the period, because it answers how much happened rather than what is running now.
- **AC-OFFICE-WORKSPACE-OVERVIEW-003.9:** The failed-session total shall be broken into four reason buckets: no response, limit, start failed, and other. The buckets shall be shown behind one disclosure on the same statistics line, each naming its count, and shall be derived from the session's own state, completion, and error text without new storage or a schema change. The agent's own first-line error text shall be shown verbatim under the buckets, following the same verbatim rule as elsewhere (AC-OFFICE-WORKSPACE-OVERVIEW-003.4). The buckets shall be read for the same period as the totals, and no session, task, or workspace identity shall be embedded in the buckets.
- **AC-OFFICE-WORKSPACE-OVERVIEW-003.10:** The statistics period shall travel as a validated query value (`window_hours`) that accepts only the supported periods; any other value shall be rejected with HTTP 400. The response shall report the period it actually applied, and the overview cache key shall carry that period so two selections cannot share a snapshot computed for a different one.
- **AC-OFFICE-WORKSPACE-OVERVIEW-003.11:** The merged-change-request figure in the last-24-hours list shall state its actual coverage. Its label shall name the code host it counts, and a scope note shown alongside the figure shall say that only pull requests tracked for a Kandev task are counted and that merges on other code hosts are not. It shall not be worded as if it counted every merge in scope.
- **AC-OFFICE-WORKSPACE-OVERVIEW-003.12:** The merged-change-request figure shall also count merges an installed code-host plugin reports for a Kandev task through the Host-owned task change-request ledger. The label and scope note shall say that GitHub pull requests and plugin-reported merges (for example Forgejo or Gitea) are counted, while merges on GitLab or Azure DevOps and merges of pull requests Kandev does not track are not. Only rows whose state is merged and whose merged instant is set shall be counted.

## Out of scope

- Organization membership and organization-wide access rules, including an organization-wide overview scope for organization admins.
- Cross-workspace task or agent changes.
- Changes to the existing per-workspace dashboard.
