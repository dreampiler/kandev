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
- **AC-OFFICE-WORKSPACE-OVERVIEW-003.1a:** The running-sessions card shall read the session limit from the configured session capacity, on every overview computation rather than once at start, so a limit changed in Settings is reflected without a restart. The general and control lanes shall each be shown against the limit that lane is admitted under, and a limit of zero (an unlimited general lane, or no control lane) shall be shown as that lane's count alone. A limit reading that did not arrive shall be shown as no limit at all rather than as zero.
- **AC-OFFICE-WORKSPACE-OVERVIEW-003.2:** Each workspace shall show tasks in progress, running AI sessions, last agent output, queued messages, tasks completed in 24 hours, and open tasks, plus problem counts, parent task groups, and the most severe warning.
- **AC-OFFICE-WORKSPACE-OVERVIEW-003.3:** Each open task shall have one status: error, possibly stalled, delayed, running, waiting, or on hold. Status shall come from task and session states and fixed time limits, not from workflow step names.
- **AC-OFFICE-WORKSPACE-OVERVIEW-003.4:** The server shall send each reason as a code with values. The page shall phrase it in the user's language. An agent's own error text shall be shown verbatim.
- **AC-OFFICE-WORKSPACE-OVERVIEW-003.5:** A workspace task list and the cross-workspace lists of running tasks, running sessions, and queued messages shall load only while expanded. Task rows shall open `/t/<task>`. Session and queued-message rows shall open `/t/<task>?sessionId=<session>`.
- **AC-OFFICE-WORKSPACE-OVERVIEW-003.6:** The server shall compute one overview per caller and scope at most every 20 seconds and share one computation among concurrent requests. It shall only read, and it shall never read message content or metadata. It shall read the head of a queued message only for the expanded queue list.
- **AC-OFFICE-WORKSPACE-OVERVIEW-003.7:** The page and expanded lists shall refresh every 30 seconds, and only while the page is visible.

## Out of scope

- Organization membership and organization-wide access rules, including an organization-wide overview scope for organization admins.
- Cross-workspace task or agent changes.
- Changes to the existing per-workspace dashboard.
