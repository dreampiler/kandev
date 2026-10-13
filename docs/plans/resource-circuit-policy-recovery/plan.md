---
created: 2026-10-13
status: done
requirements:
  - REQ-AGENTS-CIRCUIT-RECOVERY-001
system_design:
  - ../../specs/agents/system-design/resource-circuit-policy-recovery.md
---

# Resource Circuit Policy Recovery Plan

The operator delegated implementation, targeted TDD, fork publication, merge and cleanup on 2026-10-13. No design handoff or production mutation is required.

| Work order | Status | Outcome |
|---|---|---|
| [Task 01](task-01-reconcile-and-release-circuits.md) | done | Correct ambiguous windows, cap legacy Go deadlines, release one circuit, resolve account-hold names |

## UI preview

```text
Desktop: Settings > Agents
  Resource suspensions                         [Refresh]
  profile/model identifier + reason            [Release this suspension]

Phone: Settings > Agents (page scroll)
  Resource suspensions       [Refresh]
  wrapping profile/model identifier
  reason
  [Release this suspension]  (44px minimum target)
```

Production builds/swaps and database edits are excluded. Browser evidence uses synthetic rows and the actual card through Vite.
