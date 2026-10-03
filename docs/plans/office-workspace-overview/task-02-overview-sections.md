---
id: "02-overview-sections"
title: "Extend the overview with scope, sections, and lazy lists"
status: done
wave: 2
depends_on:
  - "01-cross-workspace-overview"
plan: "plan.md"
requirements:
  - REQ-OFFICE-WORKSPACE-OVERVIEW-002
  - REQ-OFFICE-WORKSPACE-OVERVIEW-003
acceptance_criteria:
  - AC-OFFICE-WORKSPACE-OVERVIEW-002.1
  - AC-OFFICE-WORKSPACE-OVERVIEW-002.2
  - AC-OFFICE-WORKSPACE-OVERVIEW-002.3
  - AC-OFFICE-WORKSPACE-OVERVIEW-003.1
  - AC-OFFICE-WORKSPACE-OVERVIEW-003.2
  - AC-OFFICE-WORKSPACE-OVERVIEW-003.3
  - AC-OFFICE-WORKSPACE-OVERVIEW-003.4
  - AC-OFFICE-WORKSPACE-OVERVIEW-003.5
  - AC-OFFICE-WORKSPACE-OVERVIEW-003.6
  - AC-OFFICE-WORKSPACE-OVERVIEW-003.7
system_design:
  - ../../specs/office/system-design/workspace-overview.md
---

# Task 02: Extend the overview sections

## Acceptance

- Preserve the existing aggregate fields, route guards, and activity links.
- Store the scope preference through user settings; default to Office workspaces.
- Separate active task counts from running AI sessions and queued messages.
- Show per-workspace problems, parent groups, last output, and lazy task lists.
- Show items waiting on a person, recent events, and model health with direct links.
- Phrase reason codes through existing localization keys where possible.
- Cache the caller-scoped snapshot for 20 seconds and refresh visible views only.

## UI preview

See [UI-02 in the plan](plan.md#expanded-overview-preview). The six system cards sit above workspace cards. A selected card opens its list inline; workspace metrics select a task filter. Human-action items, recent events, and model health follow the workspace cards.

## Verification

- Backend aggregate, overview classification, route-scope, clarification, and user-setting unit tests.
- Frontend overview copy and link tests, lazy-list and visibility hook tests, and user-setting mapping tests.
- Frontend typecheck, lint, and i18n catalog checks.
- Isolated-instance Korean and English screenshots and actual link/filter clicks with synthetic data.
- Backend changed-package lint, SQL guard, specification lint, and document catalog validation.

## Implementation result

The aggregate snapshot, caller scope setting, lazy list endpoints, localized sections, and direct task/session links are implemented. Unit tests cover caller-separated caching, shared computation, status classification, route guards, locale copy, and visibility-aware refresh. Screenshots and click results accompany the pull request. Organization-wide scope, operational status files, and configurable thresholds remain separate work.
